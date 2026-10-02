(() => {
  const $ = (id) => document.getElementById(id);
  const state = {
    channels: [], channel: null, destinations: [], samples: [],
    incidents: [], lastDestState: new Map(), timer: null
  };

  function esc(v){return String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));}
  function n(v,d=1){return Number.isFinite(Number(v)) ? Number(v).toFixed(d) : '—';}
  function integer(v){return Number.isFinite(Number(v)) ? Number(v).toLocaleString() : '—';}
  function statusClass(v){v=(v||'unknown').toLowerCase();return ['live','ready','degraded','failed','reconnecting','testing','connecting','idle','offline'].includes(v)?v:'unknown';}
  function ago(ts){if(!ts)return '—';const d=(Date.now()-new Date(ts).getTime())/1000;if(d<5)return 'now';if(d<60)return Math.floor(d)+'s ago';return Math.floor(d/60)+'m ago';}

  async function api(path, opts){
    const res = await fetch(path, opts);
    if(!res.ok){const text=await res.text();throw new Error(text || res.statusText);}
    return res.status===204?null:res.json();
  }

  async function boot(){
    tickClock();
    setInterval(tickClock,1000);
    try{
      state.channels = await api('/api/v1/channels');
      $('channelSelect').innerHTML = state.channels.map(c => '<option value="'+c.id+'">'+esc(c.name)+'</option>').join('');
      if(state.channels.length){
        selectChannel(state.channels[0].id);
      } else {
        $('apiState').textContent='API: online · no channels';
      }
    }catch(e){
      $('apiState').textContent='API: unavailable';
      addIncident('API unavailable: '+e.message,true);
    }
  }

  function tickClock(){
    $('clock').textContent=new Date().toLocaleTimeString([], {hour12:false});
  }

  function selectChannel(id){
    state.channel = state.channels.find(c => String(c.id)===String(id)) || null;
    state.samples = [];
    state.incidents = [];
    state.lastDestState.clear();
    renderChannel();
    poll();
    if(state.timer)clearInterval(state.timer);
    state.timer=setInterval(poll,2000);
  }

  function renderChannel(){
    const c=state.channel;if(!c)return;
    $('resolution').textContent=c.resolution;
    $('fps').textContent=c.fps;
    $('videoBitrate').textContent=(c.video_bitrate_kbps/1000).toFixed(1)+' Mbps';
    $('audioBitrate').textContent=c.audio_bitrate_kbps+' kbps';
    $('protocol').textContent=(c.ingest_protocol||'').toUpperCase();
    $('streamId').textContent=c.stream_id;
    setChip($('channelStatus'), c.status);
  }

  async function poll(){
    if(!state.channel)return;
    try{
      const owner=encodeURIComponent(state.channel.owner_id||'');
      const [telemetry,dests] = await Promise.all([
        api('/api/v1/channels/'+state.channel.id+'/telemetry/live'),
        api('/api/v1/destinations/runtime?owner_id='+owner)
      ]);
      state.destinations=dests;
      updateTelemetry(telemetry);
      updateDestinations(dests);
      await updateRecommendation();
      $('apiState').textContent='API: online';
    }catch(e){
      $('apiState').textContent='API: degraded';
      addIncident('Polling failed: '+e.message,true);
    }
  }

  function updateTelemetry(t){
    const live=t.status==='live';
    $('inputStatus').textContent=t.status.toUpperCase();
    $('inputDot').className='dot '+statusClass(t.status);
    $('inputMeta').textContent=live ? (state.channel.resolution+' · '+state.channel.fps+' fps · '+state.channel.ingest_protocol.toUpperCase()) : 'No active SRT publisher';
    $('rttValue').textContent=live?n(t.rtt_ms):'—';
    $('bwValue').textContent=live?n(t.bandwidth_mbit):'—';
    $('latencyValue').textContent=live?integer(t.srt_latency_ms):'—';
    $('bufferHint').textContent='Receive buffer '+(live?integer(t.recv_buffer_ms):'—')+' ms';
    $('pktUnique').textContent=live?integer(t.recv_unique_packets):'—';
    $('pktLoss').textContent=live?integer(t.recv_loss_packets):'—';
    $('pktRetrans').textContent=live?integer(t.recv_retrans_packets):'—';
    $('pktDrop').textContent=live?integer(t.recv_drop_packets):'—';

    if(live){
      state.samples.push({at:Date.now(),rtt:Number(t.rtt_ms)||0,bw:Number(t.bandwidth_mbit)||0});
      if(state.samples.length>60)state.samples.shift();
    }
    $('sampleCount').textContent=state.samples.length+' samples';
    drawChart('rttChart', state.samples.map(x=>x.rtt), 'rttRange','ms');
    drawChart('bwChart', state.samples.map(x=>x.bw), 'bwRange','Mbps');

    if(live && Number(t.rtt_ms)>300) addIncident('High SRT RTT: '+n(t.rtt_ms)+' ms', false, 'rtt-high');
    if(live && Number(t.recv_drop_packets)>0) addIncident('SRT receiver has dropped packets: '+integer(t.recv_drop_packets), true, 'drop-'+t.recv_drop_packets);
  }

  function drawChart(id, vals, labelId, suffix){
    const svg=$(id); if(!vals.length){svg.innerHTML='';$(labelId).textContent='—';return;}
    const min=Math.min(...vals), max=Math.max(...vals), span=Math.max(max-min,1);
    const w=700,h=150,p=8;
    const pts=vals.map((v,i)=>[(p+(w-p*2)*(i/Math.max(vals.length-1,1))), (h-p-(h-p*2)*((v-min)/span))]);
    const line=pts.map(p=>p[0].toFixed(1)+','+p[1].toFixed(1)).join(' ');
    const area='M '+pts[0][0]+' '+(h-p)+' L '+pts.map(p=>p[0]+' '+p[1]).join(' L ')+' L '+pts[pts.length-1][0]+' '+(h-p)+' Z';
    svg.innerHTML='<line class="gridline" x1="0" y1="50" x2="700" y2="50"/><line class="gridline" x1="0" y1="100" x2="700" y2="100"/><path class="area" d="'+area+'"/><polyline class="line" points="'+line+'"/>';
    $(labelId).textContent=n(min)+'–'+n(max)+' '+suffix;
  }

  function updateDestinations(list){
    if(!list.length){$('destinationRows').innerHTML='<tr><td colspan="7" class="empty">No destinations configured</td></tr>';updateIncidentPanel();return;}
    $('destinationRows').innerHTML=list.map(d=>{
      const s=statusClass(d.status);
      const err=d.last_error||'';
      const prior=state.lastDestState.get(d.id);
      if(prior && prior!==d.status && ['degraded','reconnecting','failed'].includes(s)){
        addIncident(d.name+' changed '+prior.toUpperCase()+' → '+d.status.toUpperCase()+(err?': '+err:''), s==='failed');
      }
      state.lastDestState.set(d.id,d.status);
      if(err && ['degraded','failed','reconnecting'].includes(s)) addIncident(d.name+': '+err,s==='failed','dest-'+d.id+'-'+err);
      return '<tr>'+
        '<td><strong>'+esc(d.name)+'</strong></td>'+
        '<td>'+esc((d.platform||'').toUpperCase())+'</td>'+
        '<td><div class="status-cell"><i class="dot '+s+'"></i>'+esc((d.status||'unknown').toUpperCase())+'</div></td>'+
        '<td>'+n(d.output_bitrate_mbps)+' Mbps</td>'+
        '<td>'+integer(d.reconnect_count)+'</td>'+
        '<td>'+ago(d.last_status_at)+'</td>'+
        '<td class="problem">'+esc(err||'—')+'</td>'+
      '</tr>';
    }).join('');
    updateIncidentPanel();
  }

  async function updateRecommendation(){
    try{
      const r=await api('/api/v1/channels/'+state.channel.id+'/recommendation');
      $('recommendationTitle').textContent=r.result.replaceAll('_',' ');
      $('recommendationReasons').textContent=(r.reasons||[]).join(' · ');
    }catch(_){
      $('recommendationTitle').textContent='No test result';
      $('recommendationReasons').textContent='Run Test connection before going live.';
    }
  }

  function setChip(el,status){
    el.textContent=(status||'unknown').toUpperCase();
    el.className='status-chip '+statusClass(status);
  }

  function addIncident(message,bad=false,key=''){
    if(key && state.incidents.some(x=>x.key===key))return;
    state.incidents.unshift({message,bad,key,time:new Date()});
    if(state.incidents.length>40)state.incidents.pop();
    renderTimeline();updateIncidentPanel();
  }

  function renderTimeline(){
    $('incidentTimeline').innerHTML=state.incidents.length?state.incidents.map(i=>
      '<div class="timeline-item '+(i.bad?'bad':'')+'"><div class="timeline-time">'+i.time.toLocaleTimeString([], {hour12:false})+'</div><div>'+esc(i.message)+'</div></div>'
    ).join(''):'<div class="empty">No incidents in this browser session</div>';
  }

  function updateIncidentPanel(){
    const badDests=state.destinations.filter(d=>['failed','degraded','reconnecting'].includes(statusClass(d.status)));
    const panel=$('incidentPanel');
    if(!badDests.length){panel.classList.add('hidden');return;}
    panel.classList.remove('hidden');
    $('incidentCount').textContent=badDests.length;
    $('incidentText').textContent=badDests.map(d=>d.name+' '+(d.status||'unknown').toUpperCase()).join(' · ');
  }

  $('channelSelect').addEventListener('change', e=>selectChannel(e.target.value));
  $('testBtn').addEventListener('click', async ()=>{
    const btn=$('testBtn'); if(!state.channel)return;
    btn.disabled=true;btn.textContent='Testing…';
    try{
      await api('/api/v1/channels/'+state.channel.id+'/test/start',{method:'POST'});
      setChip($('channelStatus'),'testing');
      addIncident('Connection test started',false,'test-start-'+Date.now());
    }catch(e){addIncident('Unable to start test: '+e.message,true);}
    finally{setTimeout(()=>{btn.disabled=false;btn.textContent='Test connection';},3000);}
  });

  boot();
})();
