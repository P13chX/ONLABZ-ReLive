(() => {
  const $ = (id) => document.getElementById(id);
  const state = {
    user:null,channels:[],channel:null,destinations:[],samples:[],incidents:[],
    timer:null,historyMinutes:15,lastLiveBytes:null,lastLiveAt:null
  };

  function esc(v){return String(v ?? '').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));}
  function n(v,d=1){return Number.isFinite(Number(v)) ? Number(v).toFixed(d) : '—';}
  function integer(v){return Number.isFinite(Number(v)) ? Number(v).toLocaleString() : '—';}
  function statusClass(v){v=(v||'unknown').toLowerCase();return ['live','ready','degraded','failed','reconnecting','testing','connecting','idle','offline','disabled'].includes(v)?v:'unknown';}
  function roleRank(r){return r==='super_administrator'?3:r==='administrator'?2:1;}
  function ago(ts){if(!ts)return '—';const s=(Date.now()-new Date(ts).getTime())/1000;if(s<5)return 'now';if(s<60)return Math.floor(s)+'s ago';if(s<3600)return Math.floor(s/60)+'m ago';return Math.floor(s/3600)+'h ago';}

  async function api(path,opts={}){
    const init={credentials:'same-origin',...opts,headers:{...(opts.headers||{})}};
    if(init.body && !init.headers['content-type'])init.headers['content-type']='application/json';
    const res=await fetch(path,init);
    if(!res.ok){
      const text=await res.text();
      const err=new Error(text||res.statusText);
      err.status=res.status;
      throw err;
    }
    return res.status===204?null:res.json();
  }

  async function boot(){
    tickClock();setInterval(tickClock,1000);
    try{
      state.user=await api('/api/v1/auth/me');
      await enterApp();
    }catch(e){
      showLogin();
    }
  }

  function showLogin(){
    $('loginScreen').classList.remove('hidden');
    $('shell').classList.add('hidden');
  }

  async function enterApp(){
    $('loginScreen').classList.add('hidden');
    $('shell').classList.remove('hidden');
    applyRole();
    await loadChannels();
  }

  function applyRole(){
    const role=state.user?.role||'user';
    document.body.dataset.role=role;
    $('roleBadge').textContent=role.replaceAll('_',' ').toUpperCase();
    $('userName').textContent=state.user?.display_name||state.user?.email||'—';
  }

  async function loadChannels(){
    state.channels=await api('/api/v1/channels');
    $('channelSelect').innerHTML=state.channels.map(c=>'<option value="'+c.id+'">'+esc(c.name)+(state.user?.role==='super_administrator'?' · '+esc(c.owner_id):'')+'</option>').join('');
    if(state.channels.length) await selectChannel(state.channels[0].id);
    else {
      state.channel=null;
      $('apiState').textContent='API: online · no channels assigned';
    }
  }

  function tickClock(){$('clock').textContent=new Date().toLocaleTimeString([], {hour12:false});}

  async function selectChannel(id){
    state.channel=state.channels.find(c=>String(c.id)===String(id))||null;
    state.samples=[];state.destinations=[];state.incidents=[];
    state.lastLiveBytes=null;state.lastLiveAt=null;
    if(!state.channel)return;
    renderChannel(state.channel);
    if(roleRank(state.user.role)>=2) await loadHistory();
    await poll();
    if(state.timer)clearInterval(state.timer);
    state.timer=setInterval(poll,2000);
  }

  function rtmpURL(c){
    const protocol=(c.ingest_protocol||'rtmp').toLowerCase();
    const app=(c.ingest_app||'/live').startsWith('/')?(c.ingest_app||'/live'):'/'+(c.ingest_app||'live');
    return protocol+'://'+c.ingest_host+':'+c.ingest_port+app;
  }

  function renderChannel(c){
    $('resolution').textContent=c.resolution||'—';
    $('fps').textContent=c.fps||'—';
    $('videoBitrate').textContent=n((c.video_bitrate_kbps||0)/1000,1)+' Mbps';
    $('audioBitrate').textContent=integer(c.audio_bitrate_kbps)+' kbps';
    $('protocol').textContent=(c.ingest_protocol||'rtmp').toUpperCase();
    $('streamId').textContent=c.stream_id||'—';
    $('rtmpUrl').textContent=rtmpURL(c);
    $('streamKey').textContent=c.stream_key||'—';
    $('keyVersion').textContent='v'+(c.stream_key_version||1);
    setChip($('channelStatus'),c.status);
  }

  async function loadHistory(){
    if(!state.channel)return;
    try{
      const rows=await api('/api/v1/channels/'+state.channel.id+'/telemetry/history?minutes='+state.historyMinutes+'&limit=3000');
      state.samples=rows.map(x=>({at:new Date(x.observed_at).getTime(),rtt:Number(x.rtt_ms)||0,receive:Number(x.receive_bitrate_mbit)||0,bandwidth:Number(x.bandwidth_mbit)||0}));
      renderCharts();
    }catch(e){state.samples=[];renderCharts();}
  }

  async function poll(){
    if(!state.channel)return;
    const id=state.channel.id;
    const calls=[
      api('/api/v1/channels/'+id),
      api('/api/v1/channels/'+id+'/telemetry/live'),
      api('/api/v1/destinations/runtime?channel_id='+id)
    ];
    if(roleRank(state.user.role)>=2){
      calls.push(api('/api/v1/incidents?channel_id='+id+'&limit=80'));
      calls.push(api('/api/v1/channels/'+id+'/recommendation'));
    }
    const results=await Promise.allSettled(calls);

    if(results[0]?.status==='fulfilled'){state.channel=results[0].value;renderChannel(state.channel);}
    if(results[1]?.status==='fulfilled')updateTelemetry(results[1].value);
    if(results[2]?.status==='fulfilled'){state.destinations=results[2].value;updateDestinations(state.destinations);}
    if(roleRank(state.user.role)>=2){
      if(results[3]?.status==='fulfilled'){state.incidents=results[3].value;renderIncidents();}
      if(results[4]?.status==='fulfilled')updateRecommendation(results[4].value);else clearRecommendation();
    }
    $('apiState').textContent='API: online';
  }

  function updateTelemetry(t){
    const live=t.status==='live';
    $('inputStatus').textContent=(t.status||'unknown').toUpperCase();
    $('inputMetaStatus').textContent=(t.status||'unknown').toUpperCase();
    $('inputDot').className='dot '+statusClass(t.status);
    $('inputMeta').textContent=live ? state.channel.resolution+' · '+state.channel.fps+' fps · '+(t.protocol||state.channel.ingest_protocol).toUpperCase() : 'No active publisher';

    const isSRT=(t.protocol||'').toLowerCase()==='srt';
    $('rttValue').textContent=live&&isSRT?n(t.rtt_ms):'—';
    $('bwValue').textContent=live&&isSRT?n(t.bandwidth_mbit):'—';
    $('latencyValue').textContent=live&&isSRT?integer(t.srt_latency_ms):'—';
    $('bufferHint').textContent=isSRT?'Receive buffer '+(live?integer(t.recv_buffer_ms):'—')+' ms':'RTMP primary input';
    $('pktUnique').textContent=live&&isSRT?integer(t.recv_unique_packets):'—';
    $('pktLoss').textContent=live&&isSRT?integer(t.recv_loss_packets):'—';
    $('pktRetrans').textContent=live&&isSRT?integer(t.recv_retrans_packets):'—';
    $('pktDrop').textContent=live&&isSRT?integer(t.recv_drop_packets):'—';

    if(!isSRT){
      $('headroomHint').textContent=live?'RTMP publisher connected':'Waiting for RTMP publisher';
      return;
    }

    const last=state.samples[state.samples.length-1];
    $('headroomHint').textContent='Receive '+(last?n(last.receive):'—')+' Mbps';
    if(live){
      const now=Date.now();
      let receive=last?last.receive:0;
      const bytes=Number(t.recv_unique_bytes);
      if(Number.isFinite(bytes)&&state.lastLiveBytes!==null&&state.lastLiveAt!==null&&bytes>=state.lastLiveBytes){
        const seconds=(now-state.lastLiveAt)/1000;if(seconds>0)receive=((bytes-state.lastLiveBytes)*8/seconds/1000000);
      }
      if(Number.isFinite(bytes)){state.lastLiveBytes=bytes;state.lastLiveAt=now;}
      const duplicate=last&&Math.abs(last.at-now)<1200;
      if(!duplicate)state.samples.push({at:now,rtt:Number(t.rtt_ms)||0,receive,bandwidth:Number(t.bandwidth_mbit)||0});
      const cutoff=now-state.historyMinutes*60*1000;
      state.samples=state.samples.filter(x=>x.at>=cutoff);
      renderCharts();
    }else{state.lastLiveBytes=null;state.lastLiveAt=null;}
  }

  function renderCharts(){
    $('sampleCount').textContent=state.samples.length+' samples';
    drawChart('rttChart',state.samples.map(x=>x.rtt),'rttRange','ms');
    drawChart('bwChart',state.samples.map(x=>x.receive),'bwRange','Mbps');
  }

  function drawChart(id,vals,labelId,suffix){
    const svg=$(id);if(!svg)return;
    if(!vals.length){svg.innerHTML='';$(labelId).textContent='—';return;}
    const min=Math.min(...vals),max=Math.max(...vals),span=Math.max(max-min,1),w=700,h=150,p=8;
    const pts=vals.map((v,i)=>[p+(w-p*2)*(i/Math.max(vals.length-1,1)),h-p-(h-p*2)*((v-min)/span)]);
    const line=pts.map(x=>x[0].toFixed(1)+','+x[1].toFixed(1)).join(' ');
    const area='M '+pts[0][0]+' '+(h-p)+' L '+pts.map(x=>x[0]+' '+x[1]).join(' L ')+' L '+pts[pts.length-1][0]+' '+(h-p)+' Z';
    svg.innerHTML='<line class="gridline" x1="0" y1="50" x2="700" y2="50"/><line class="gridline" x1="0" y1="100" x2="700" y2="100"/><path class="area" d="'+area+'"/><polyline class="line" points="'+line+'"/>';
    $(labelId).textContent=n(min)+'–'+n(max)+' '+suffix;
  }

  function audioBadge(d){
    const s=(d.audio_status||'unknown').toLowerCase();
    const label=s==='healthy'?n(d.audio_bitrate_kbps,0)+' kbps · '+n(d.audio_pps,1)+' pps':s.toUpperCase();
    return '<div class="audio-cell"><i class="audio-dot '+esc(s)+'"></i><span>'+esc(label)+'</span></div>';
  }

  function integrityBadge(d){
    const s=(d.quality_status||'unknown').toLowerCase();
    let label='UNKNOWN',cls='unknown';
    if(s==='preserved'){label='PRESERVED';cls='preserved';}
    if(s==='changed'){label='QUALITY CHANGED';cls='changed';}
    const reasons=Array.isArray(d.quality_reasons)?d.quality_reasons:[];
    const detail=(d.source_resolution||'—')+' '+n(d.source_fps,1)+'fps → '+(d.output_resolution||'—')+' '+n(d.output_fps,1)+'fps';
    return '<div class="integrity-cell" title="'+esc(reasons.join(', '))+'"><span class="integrity-badge '+cls+'">'+esc(label)+'</span><div class="subline">'+esc(detail)+'</div></div>';
  }

  function controlButtons(d){
    const running=d.desired_state==='running';
    if(running)return '<div class="row-actions"><button class="btn mini advanced-only-inline" data-cmd="restart" data-id="'+d.id+'">Restart</button><button class="btn mini danger" data-cmd="stop" data-id="'+d.id+'">Stop</button></div>';
    return '<button class="btn mini primary" data-cmd="start" data-id="'+d.id+'">Start</button>';
  }

  function updateDestinations(list){
    if(!list.length){
      $('destinationRows').innerHTML='<tr><td colspan="9" class="empty">No destinations configured for this channel</td></tr>';
      updateIncidentPanel();return;
    }
    $('destinationRows').innerHTML=list.map(d=>{
      const s=statusClass(d.status),err=d.last_error||'';
      return '<tr>'+
        '<td><strong>'+esc(d.name)+'</strong><div class="subline">'+esc((d.platform||'').toUpperCase())+'</div></td>'+
        '<td><div class="status-cell"><i class="dot '+s+'"></i>'+esc((d.status||'unknown').toUpperCase())+'</div></td>'+
        '<td class="advanced-only-cell">'+integrityBadge(d)+'</td>'+
        '<td class="advanced-only-cell">'+n(d.video_bitrate_kbps,0)+' kbps</td>'+
        '<td class="advanced-only-cell">'+audioBadge(d)+'</td>'+
        '<td class="advanced-only-cell">'+n(d.fps,1)+'</td>'+
        '<td class="advanced-only-cell">'+integer(d.reconnect_count)+'</td>'+
        '<td class="problem">'+esc(err||'—')+'</td>'+
        '<td>'+controlButtons(d)+'</td>'+
      '</tr>';
    }).join('');
    updateIncidentPanel();
  }

  async function commandDestination(id,command){
    const btn=document.querySelector('[data-id="'+id+'"][data-cmd="'+command+'"]');
    if(btn){btn.disabled=true;btn.textContent=command==='start'?'Starting…':command==='stop'?'Stopping…':'Restarting…';}
    try{
      await api('/api/v1/destinations/'+id+'/command',{method:'POST',body:JSON.stringify({command})});
      setTimeout(poll,350);
    }catch(e){showTransientProblem('Destination command failed');}
  }

  function updateRecommendation(r){
    $('recommendationTitle').textContent=(r.result||'UNKNOWN').replaceAll('_',' ');
    $('recommendationReasons').textContent=(r.reasons||[]).join(' · ');
  }
  function clearRecommendation(){
    $('recommendationTitle').textContent='No recent SRT test';
    $('recommendationReasons').textContent='RTMP is the primary ingest. Use SRT test only for advanced SRT contribution.';
  }

  function renderIncidents(){
    $('incidentTimeline').innerHTML=state.incidents.length?state.incidents.map(i=>{
      const bad=i.severity==='critical';
      return '<div class="timeline-item '+(bad?'bad':'')+'"><div class="timeline-time">'+new Date(i.created_at).toLocaleTimeString([], {hour12:false})+'</div><div><strong>'+esc(i.severity.toUpperCase())+'</strong> · '+esc(i.message)+'</div></div>';
    }).join(''):'<div class="empty">No incidents recorded</div>';
    updateIncidentPanel();
  }

  function updateIncidentPanel(){
    if(roleRank(state.user?.role||'user')<2)return;
    const badDests=state.destinations.filter(d=>['failed','degraded','reconnecting'].includes(statusClass(d.status))||d.audio_status==='missing'||d.quality_status==='changed');
    const panel=$('incidentPanel');
    if(!badDests.length){panel.classList.add('hidden');return;}
    panel.classList.remove('hidden');
    $('incidentCount').textContent=badDests.length;
    $('incidentText').textContent=badDests.map(d=>d.name+' '+(d.quality_status==='changed'?'QUALITY CHANGED':d.audio_status==='missing'?'AUDIO MISSING':String(d.status||'unknown').toUpperCase())).join(' · ');
  }

  function showTransientProblem(message){
    if(roleRank(state.user?.role||'user')<2)return;
    const panel=$('incidentPanel');panel.classList.remove('hidden');$('incidentText').textContent=message;$('incidentCount').textContent='!';
  }

  function setChip(el,status){if(!el)return;el.textContent=(status||'unknown').toUpperCase();el.className='status-chip '+statusClass(status);}

  $('loginForm').addEventListener('submit',async e=>{
    e.preventDefault();$('loginError').textContent='';
    try{
      state.user=await api('/api/v1/auth/login',{method:'POST',body:JSON.stringify({email:$('loginEmail').value,password:$('loginPassword').value})});
      $('loginPassword').value='';
      await enterApp();
    }catch(err){$('loginError').textContent='Sign in failed';}
  });

  $('logoutBtn').addEventListener('click',async()=>{
    try{await api('/api/v1/auth/logout',{method:'POST'});}catch(_){}
    if(state.timer)clearInterval(state.timer);
    state.user=null;showLogin();
  });

  $('channelSelect').addEventListener('change',e=>selectChannel(e.target.value));
  $('historyRange').addEventListener('change',async e=>{state.historyMinutes=Number(e.target.value)||15;await loadHistory();});
  $('destinationRows').addEventListener('click',e=>{const b=e.target.closest('button[data-id][data-cmd]');if(b)commandDestination(b.dataset.id,b.dataset.cmd);});

  $('resetKeyBtn').addEventListener('click',async()=>{
    if(!state.channel)return;
    if(!confirm('Reset this channel stream key? The old key will no longer be used by ReLive.'))return;
    const btn=$('resetKeyBtn');btn.disabled=true;btn.textContent='Resetting…';
    try{
      const out=await api('/api/v1/channels/'+state.channel.id+'/stream-key/reset',{method:'POST'});
      state.channel.stream_key=out.stream_key;
      state.channel.stream_key_version=out.stream_key_version;
      state.channel.stream_key_updated_at=out.stream_key_updated_at;
      const i=state.channels.findIndex(x=>x.id===state.channel.id);if(i>=0)state.channels[i]=state.channel;
      renderChannel(state.channel);
    }catch(e){showTransientProblem('Unable to reset stream key');}
    finally{btn.disabled=false;btn.textContent='Reset key';}
  });

  document.querySelectorAll('.copy-btn').forEach(btn=>btn.addEventListener('click',async()=>{
    const el=$(btn.dataset.copy);if(!el)return;
    try{await navigator.clipboard.writeText(el.textContent);const old=btn.textContent;btn.textContent='Copied';setTimeout(()=>btn.textContent=old,900);}catch(_){}
  }));

  document.querySelectorAll('.nav-item').forEach(btn=>btn.addEventListener('click',()=>{
    document.querySelectorAll('.nav-item').forEach(x=>x.classList.remove('active'));btn.classList.add('active');
    $('pageTitle').textContent=btn.textContent.trim();
  }));

  $('testBtn').addEventListener('click',async()=>{
    const btn=$('testBtn');if(!state.channel)return;
    btn.disabled=true;btn.textContent='Testing…';
    try{await api('/api/v1/channels/'+state.channel.id+'/test/start',{method:'POST'});setChip($('channelStatus'),'testing');}
    catch(e){showTransientProblem('Unable to start connection test');}
    finally{setTimeout(()=>{btn.disabled=false;btn.textContent='Test connection';},3000);}
  });

  boot();
})();
