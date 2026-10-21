(() => {
  const $=id=>document.getElementById(id);
  const state={user:null,channels:[],channel:null,destinations:[],samples:[],incidents:[],owners:[],users:[],timer:null,view:'overview',historyMinutes:15,hls:null,audio:null,lastPreviewManifest:''};
  const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const n=(v,d=1)=>Number.isFinite(Number(v))?Number(v).toFixed(d):'—';
  const integer=v=>Number.isFinite(Number(v))?Number(v).toLocaleString():'—';
  const rank=r=>r==='super_administrator'?3:r==='administrator'?2:1;
  const statusClass=v=>{v=(v||'unknown').toLowerCase();return ['live','ready','degraded','failed','reconnecting','testing','connecting','idle','offline','disabled','running'].includes(v)?v:'unknown';};

  async function api(path,opts={}){
    const init={credentials:'same-origin',...opts,headers:{...(opts.headers||{})}};
    if(init.body&&!init.headers['content-type'])init.headers['content-type']='application/json';
    const res=await fetch(path,init);
    if(!res.ok){const e=new Error(await res.text()||res.statusText);e.status=res.status;throw e;}
    return res.status===204?null:res.json();
  }

  async function boot(){
    setInterval(()=>{$('clock').textContent=new Date().toLocaleTimeString([], {hour12:false});},1000);
    try{state.user=await api('/api/v1/auth/me');await enterApp();}catch(_){showLogin();}
  }
  function showLogin(){$('loginScreen').classList.remove('hidden');$('shell').classList.add('hidden');}
  async function enterApp(){
    $('loginScreen').classList.add('hidden');$('shell').classList.remove('hidden');
    document.body.dataset.role=state.user.role;
    $('roleBadge').textContent=state.user.role.replaceAll('_',' ').toUpperCase();
    $('userName').textContent=state.user.display_name||state.user.email;
    await loadChannels();
    if(state.user.role==='super_administrator')await Promise.all([loadOwners(),loadUsers()]);
  }

  async function loadChannels(){
    state.channels=await api('/api/v1/channels');
    $('channelSelect').innerHTML=state.channels.map(c=>'<option value="'+c.id+'">'+esc(c.name)+(state.user.role==='super_administrator'?' · '+esc(c.owner_id):'')+'</option>').join('');
    if(state.channels.length)await selectChannel(state.channels[0].id);
  }
  async function selectChannel(id){
    state.channel=state.channels.find(c=>String(c.id)===String(id))||null;
    state.samples=[];state.incidents=[];state.destinations=[];
    stopPreviewClient();
    if(!state.channel)return;
    renderChannel(state.channel);
    if(rank(state.user.role)>=2)await loadHistory();
    await pollOverview();
    if(state.timer)clearInterval(state.timer);
    state.timer=setInterval(()=>{if(state.view==='overview')pollOverview();},2000);
  }
  function renderChannel(c){
    $('rtmpUrl').textContent=(c.ingest_protocol||'rtmp')+'://'+c.ingest_host+':'+c.ingest_port+(c.ingest_app||'/live');
    $('streamKey').textContent=c.stream_key||'—';$('keyVersion').textContent='v'+(c.stream_key_version||1);
    $('resolution').textContent=c.resolution||'—';$('fps').textContent=c.fps||'—';
    $('videoBitrate').textContent=n((c.video_bitrate_kbps||0)/1000,1)+' Mbps';$('audioBitrate').textContent=integer(c.audio_bitrate_kbps)+' kbps';
    $('protocol').textContent=(c.ingest_protocol||'').toUpperCase();$('streamId').textContent=c.stream_id||'—';
    setChip($('channelStatus'),c.status);
  }

  async function pollOverview(){
    if(!state.channel)return;
    const id=state.channel.id;
    const calls=[api('/api/v1/channels/'+id),api('/api/v1/channels/'+id+'/telemetry/live'),api('/api/v1/destinations/runtime?channel_id='+id),api('/api/v1/channels/'+id+'/preview')];
    if(rank(state.user.role)>=2){calls.push(api('/api/v1/incidents?channel_id='+id+'&limit=80'));calls.push(api('/api/v1/channels/'+id+'/recommendation'));}
    const r=await Promise.allSettled(calls);
    if(r[0]?.status==='fulfilled'){state.channel=r[0].value;renderChannel(state.channel);}
    if(r[1]?.status==='fulfilled')renderInput(r[1].value);
    if(r[2]?.status==='fulfilled'){state.destinations=r[2].value;renderDestinations();}
    if(r[3]?.status==='fulfilled')renderPreview(r[3].value);
    if(rank(state.user.role)>=2){
      if(r[4]?.status==='fulfilled'){state.incidents=r[4].value;renderIncidents();}
      if(r[5]?.status==='fulfilled')renderRecommendation(r[5].value);
    }
    $('apiState').textContent='API: online';
  }

  function renderInput(t){
    const live=t.status==='live',s=statusClass(t.status);
    $('inputStatus').textContent=(t.status||'unknown').toUpperCase();$('inputDot').className='dot '+s;$('inputMetaStatus').textContent=(t.status||'unknown').toUpperCase();
    $('inputMeta').textContent=live?state.channel.resolution+' · '+state.channel.fps+' fps · '+(t.protocol||state.channel.ingest_protocol).toUpperCase():'No active publisher';
    const srt=(t.protocol||'').toLowerCase()==='srt';
    $('rttValue').textContent=live&&srt?n(t.rtt_ms):'—';$('bwValue').textContent=live&&srt?n(t.bandwidth_mbit):'—';$('latencyValue').textContent=live&&srt?integer(t.srt_latency_ms):'—';
    $('pktUnique').textContent=live&&srt?integer(t.recv_unique_packets):'—';$('pktLoss').textContent=live&&srt?integer(t.recv_loss_packets):'—';$('pktRetrans').textContent=live&&srt?integer(t.recv_retrans_packets):'—';$('pktDrop').textContent=live&&srt?integer(t.recv_drop_packets):'—';
    $('headroomHint').textContent=srt?'SRT telemetry':'RTMP publisher state';$('bufferHint').textContent=srt?'Receive buffer '+integer(t.recv_buffer_ms)+' ms':'RTMP primary input';
  }

  function renderPreview(p){
    setChip($('previewStatus'),p.status==='live'?'live':p.status);
    const live=p.status==='live';
    $('previewPlaceholder').classList.toggle('hidden',live);
    $('sourceCodec').textContent=(p.video_codec||'—').toUpperCase();
    $('sourceResolution').textContent=p.width&&p.height?p.width+'×'+p.height:'—';
    $('sourceFPS').textContent=n(p.fps,2);$('sourceVideoBitrate').textContent=p.video_bitrate_kbps?n(p.video_bitrate_kbps/1000,2)+' Mbps':'—';
    $('sourceAudioCodec').textContent=(p.audio_codec||'—').toUpperCase();$('sourceAudioBitrate').textContent=p.audio_bitrate_kbps?n(p.audio_bitrate_kbps,0)+' kbps':'—';
    $('sourceAudioHz').textContent=p.audio_sample_hz?integer(p.audio_sample_hz)+' Hz':'—';$('sourceAudioChannels').textContent=p.audio_channels||'—';
    if(live&&p.manifest_url&&p.manifest_url!==state.lastPreviewManifest){state.lastPreviewManifest=p.manifest_url;attachPreview(p.manifest_url);}
  }
  function attachPreview(url){
    const v=$('previewVideo');
    if(state.hls){state.hls.destroy();state.hls=null;}
    if(v.canPlayType('application/vnd.apple.mpegurl')){v.src=url;v.play().catch(()=>{});return;}
    if(window.Hls&&Hls.isSupported()){
      state.hls=new Hls({liveSyncDurationCount:2,maxLiveSyncPlaybackRate:1.1,enableWorker:true});
      state.hls.loadSource(url);state.hls.attachMedia(v);state.hls.on(Hls.Events.MANIFEST_PARSED,()=>v.play().catch(()=>{}));
    }
  }
  function stopPreviewClient(){
    state.lastPreviewManifest='';if(state.hls){state.hls.destroy();state.hls=null;}
    const v=$('previewVideo');if(v){v.pause();v.removeAttribute('src');v.load();}
  }

  async function enableAudio(){
    const v=$('previewVideo');
    if(!state.audio){
      const C=window.AudioContext||window.webkitAudioContext;if(!C)return;
      const ctx=new C(),source=ctx.createMediaElementSource(v),analyser=ctx.createAnalyser();analyser.fftSize=1024;
      source.connect(analyser);analyser.connect(ctx.destination);
      state.audio={ctx,analyser,data:new Float32Array(analyser.fftSize),raf:0};
    }
    if(state.audio.ctx.state==='suspended')await state.audio.ctx.resume();
    v.muted=!v.muted;$('audioToggle').textContent=v.muted?'Enable audio':'Mute audio';
    meterLoop();
  }
  function meterLoop(){
    if(!state.audio)return;
    cancelAnimationFrame(state.audio.raf);
    const tick=()=>{
      state.audio.analyser.getFloatTimeDomainData(state.audio.data);
      let sum=0,peak=0;for(const x of state.audio.data){sum+=x*x;peak=Math.max(peak,Math.abs(x));}
      const rms=Math.sqrt(sum/state.audio.data.length),db=rms>0?20*Math.log10(rms):-Infinity;
      const pct=Math.max(0,Math.min(100,(db+60)/60*100));
      $('audioMeterBar').style.width=pct+'%';$('audioDb').textContent=Number.isFinite(db)?db.toFixed(1)+' dBFS':'−∞ dBFS';
      state.audio.raf=requestAnimationFrame(tick);
    };tick();
  }

  async function loadHistory(){
    try{const rows=await api('/api/v1/channels/'+state.channel.id+'/telemetry/history?minutes='+state.historyMinutes+'&limit=3000');state.samples=rows.map(x=>({rtt:Number(x.rtt_ms)||0,receive:Number(x.receive_bitrate_mbit)||0}));renderCharts();}catch(_){state.samples=[];renderCharts();}
  }
  function renderCharts(){$('sampleCount').textContent=state.samples.length+' samples';draw('rttChart',state.samples.map(x=>x.rtt),'rttRange','ms');draw('bwChart',state.samples.map(x=>x.receive),'bwRange','Mbps');}
  function draw(id,vals,labelId,suffix){
    const svg=$(id);if(!vals.length){svg.innerHTML='';$(labelId).textContent='—';return;}
    const min=Math.min(...vals),max=Math.max(...vals),span=Math.max(max-min,1),w=700,h=150,p=8;
    const pts=vals.map((v,i)=>[p+(w-p*2)*(i/Math.max(vals.length-1,1)),h-p-(h-p*2)*((v-min)/span)]);
    const line=pts.map(x=>x[0].toFixed(1)+','+x[1].toFixed(1)).join(' ');
    svg.innerHTML='<polyline class="line" points="'+line+'"/>';$(''+labelId).textContent=n(min)+'–'+n(max)+' '+suffix;
  }

  function renderDestinations(){
    const advanced=rank(state.user.role)>=2;
    $('destinationRows').innerHTML=state.destinations.length?state.destinations.map(d=>{
      const integrity=(d.quality_status||'unknown').toUpperCase(),controls=d.desired_state==='running'?'<button class="btn mini" data-cmd="restart" data-id="'+d.id+'">Restart</button> <button class="btn mini danger" data-cmd="stop" data-id="'+d.id+'">Stop</button>':'<button class="btn mini primary" data-cmd="start" data-id="'+d.id+'">Start</button>';
      return '<tr><td><strong>'+esc(d.name)+'</strong><div class="subline">'+esc((d.platform||'').toUpperCase())+'</div></td><td><span class="status-chip '+statusClass(d.status)+'">'+esc((d.status||'unknown').toUpperCase())+'</span></td>'+
        '<td class="advanced-only-cell">'+esc(integrity)+'</td><td class="advanced-only-cell">'+n(d.video_bitrate_kbps,0)+' kbps</td><td class="advanced-only-cell">'+n(d.audio_bitrate_kbps,0)+' kbps</td><td class="advanced-only-cell">'+n(d.fps,1)+'</td><td class="problem">'+esc(d.last_error||'—')+'</td><td>'+controls+'</td></tr>';
    }).join(''):'<tr><td colspan="8" class="empty">No destinations configured</td></tr>';
    if(!advanced)document.querySelectorAll('.advanced-only-cell').forEach(x=>x.style.display='none');
    updateIncidentPanel();
  }
  function renderIncidents(){
    $('incidentTimeline').innerHTML=state.incidents.length?state.incidents.map(i=>'<div class="timeline-item '+(i.severity==='critical'?'bad':'')+'"><div class="timeline-time">'+new Date(i.created_at).toLocaleTimeString([], {hour12:false})+'</div><div><strong>'+esc(i.severity.toUpperCase())+'</strong> · '+esc(i.message)+'</div></div>').join(''):'<div class="empty">No incidents recorded</div>';updateIncidentPanel();
  }
  function updateIncidentPanel(){
    if(rank(state.user.role)<2)return;const bad=state.destinations.filter(d=>['failed','degraded','reconnecting'].includes(statusClass(d.status))||d.audio_status==='missing'||d.quality_status==='changed');
    $('incidentPanel').classList.toggle('hidden',!bad.length);$('incidentCount').textContent=bad.length;$('incidentText').textContent=bad.map(d=>d.name+' '+(d.quality_status==='changed'?'QUALITY CHANGED':String(d.status).toUpperCase())).join(' · ');
  }
  function renderRecommendation(r){$('recommendationTitle').textContent=(r.result||'UNKNOWN').replaceAll('_',' ');$('recommendationReasons').textContent=(r.reasons||[]).join(' · ');}
  function setChip(el,status){el.textContent=(status||'unknown').toUpperCase();el.className='status-chip '+statusClass(status);}

  async function loadOwners(){state.owners=await api('/api/v1/admin/owners');renderOwners();fillOwnerSelects();}
  function renderOwners(){$('ownersRows').innerHTML=state.owners.map(o=>'<tr><td><strong>'+esc(o.name)+'</strong></td><td class="mono">'+esc(o.owner_key)+'</td><td>'+o.channel_count+'</td><td>'+o.user_count+'</td><td>'+(o.active?'ACTIVE':'DISABLED')+'</td><td><button class="btn mini" data-owner-toggle="'+o.id+'" data-active="'+o.active+'">'+(o.active?'Disable':'Enable')+'</button></td></tr>').join('');}
  function fillOwnerSelects(){const opts=state.owners.filter(o=>o.active).map(o=>'<option value="'+esc(o.owner_key)+'">'+esc(o.name)+'</option>').join('');$('newUserOwner').innerHTML=opts;$('newChannelOwner').innerHTML=opts;}

  async function loadUsers(){state.users=await api('/api/v1/users');renderUsers();}
  function renderUsers(){$('usersRows').innerHTML=state.users.map(u=>'<tr><td>'+esc(u.display_name||'—')+'</td><td>'+esc(u.email)+'</td><td>'+esc(u.owner_id)+'</td><td><select class="inline-select" data-user-role="'+u.id+'"><option value="user" '+(u.role==='user'?'selected':'')+'>user</option><option value="administrator" '+(u.role==='administrator'?'selected':'')+'>administrator</option><option value="super_administrator" '+(u.role==='super_administrator'?'selected':'')+'>super admin</option></select></td><td>'+(u.active?'ACTIVE':'DISABLED')+'</td><td><button class="btn mini" data-user-toggle="'+u.id+'" data-active="'+u.active+'">'+(u.active?'Disable':'Enable')+'</button></td></tr>').join('');}

  function renderAdminChannels(){
    $('channelsRows').innerHTML=state.channels.map(c=>'<tr><td><strong>'+esc(c.name)+'</strong></td><td>'+esc(c.owner_id)+'</td><td>'+esc(c.ingest_protocol.toUpperCase())+'</td><td>'+esc((c.status||'offline').toUpperCase())+'</td><td class="mono">'+esc((c.stream_key||'').slice(0,12))+'…</td><td><button class="btn mini" data-channel-reset="'+c.id+'">Reset key</button> <button class="btn mini danger" data-channel-delete="'+c.id+'">Delete</button></td></tr>').join('');
  }

  async function loadSystem(){
    const h=await api('/api/v1/admin/system-health');
    const cards=[['Overall',h.status],['PostgreSQL',h.database],['Core',h.core],['Channels',h.channels],['Owners',h.owners],['Users',h.users],['Destinations',h.destinations],['Live outputs',h.live_destinations],['Failed outputs',h.failed_destinations],['RTMP publishers',h.rtmp_publishers],['SRT publishers',h.srt_publishers],['Preview processes',h.preview_processes]];
    $('systemCards').innerHTML=cards.map(x=>'<article class="stat-card"><div class="stat-label">'+esc(x[0])+'</div><div class="system-value">'+esc(String(x[1]).toUpperCase())+'</div></article>').join('');
    $('systemSummary').textContent='Critical incidents in last hour: '+h.recent_critical_incidents+' · Updated '+new Date(h.generated_at).toLocaleTimeString([], {hour12:false});
  }

  async function setView(view){
    state.view=view;document.querySelectorAll('.view').forEach(v=>v.classList.add('hidden'));$('view-'+view).classList.remove('hidden');
    document.querySelectorAll('.nav-item').forEach(b=>b.classList.toggle('active',b.dataset.view===view));$('pageTitle').textContent=document.querySelector('.nav-item[data-view="'+view+'"]').textContent;
    $('overviewActions').classList.toggle('hidden',view!=='overview');
    if(view==='owners')await loadOwners();
    if(view==='users')await Promise.all([loadOwners(),loadUsers()]);
    if(view==='channels'){await Promise.all([loadOwners(),loadChannels()]);renderAdminChannels();}
    if(view==='system')await loadSystem();
  }

  $('loginForm').addEventListener('submit',async e=>{e.preventDefault();try{state.user=await api('/api/v1/auth/login',{method:'POST',body:JSON.stringify({email:$('loginEmail').value,password:$('loginPassword').value})});$('loginPassword').value='';await enterApp();}catch(_){$('loginError').textContent='Sign in failed';}});
  $('logoutBtn').addEventListener('click',async()=>{try{await api('/api/v1/auth/logout',{method:'POST'});}catch(_){}if(state.timer)clearInterval(state.timer);stopPreviewClient();state.user=null;showLogin();});
  $('channelSelect').addEventListener('change',e=>selectChannel(e.target.value));
  $('historyRange').addEventListener('change',async e=>{state.historyMinutes=Number(e.target.value)||15;await loadHistory();});
  $('audioToggle').addEventListener('click',enableAudio);
  $('destinationRows').addEventListener('click',async e=>{const b=e.target.closest('[data-cmd]');if(!b)return;await api('/api/v1/destinations/'+b.dataset.id+'/command',{method:'POST',body:JSON.stringify({command:b.dataset.cmd})});setTimeout(pollOverview,300);});
  $('resetKeyBtn').addEventListener('click',async()=>{if(!state.channel||!confirm('Reset stream key?'))return;const o=await api('/api/v1/channels/'+state.channel.id+'/stream-key/reset',{method:'POST'});state.channel.stream_key=o.stream_key;state.channel.stream_key_version=o.stream_key_version;renderChannel(state.channel);stopPreviewClient();});
  document.addEventListener('click',async e=>{const b=e.target.closest('.copy-btn');if(b){const el=$(b.dataset.copy);if(el)await navigator.clipboard.writeText(el.textContent);}});
  document.querySelectorAll('.nav-item').forEach(b=>b.addEventListener('click',()=>setView(b.dataset.view)));

  $('ownerForm').addEventListener('submit',async e=>{e.preventDefault();await api('/api/v1/admin/owners',{method:'POST',body:JSON.stringify({owner_key:$('ownerKey').value,name:$('ownerName').value})});e.target.reset();await loadOwners();});
  $('ownersRows').addEventListener('click',async e=>{const b=e.target.closest('[data-owner-toggle]');if(!b)return;await api('/api/v1/admin/owners/'+b.dataset.ownerToggle,{method:'PATCH',body:JSON.stringify({active:b.dataset.active!=='true'})});await loadOwners();});

  $('userForm').addEventListener('submit',async e=>{e.preventDefault();await api('/api/v1/users',{method:'POST',body:JSON.stringify({email:$('newUserEmail').value,display_name:$('newUserName').value,owner_id:$('newUserOwner').value,role:$('newUserRole').value,password:$('newUserPassword').value})});e.target.reset();await loadUsers();});
  $('usersRows').addEventListener('change',async e=>{if(!e.target.matches('[data-user-role]'))return;await api('/api/v1/users/'+e.target.dataset.userRole,{method:'PATCH',body:JSON.stringify({role:e.target.value})});await loadUsers();});
  $('usersRows').addEventListener('click',async e=>{const b=e.target.closest('[data-user-toggle]');if(!b)return;await api('/api/v1/users/'+b.dataset.userToggle,{method:'PATCH',body:JSON.stringify({active:b.dataset.active!=='true'})});await loadUsers();});

  $('channelForm').addEventListener('submit',async e=>{e.preventDefault();await api('/api/v1/channels',{method:'POST',body:JSON.stringify({owner_id:$('newChannelOwner').value,name:$('newChannelName').value,ingest_host:$('newChannelHost').value,ingest_protocol:$('newChannelProtocol').value})});e.target.reset();await loadChannels();renderAdminChannels();});
  $('channelsRows').addEventListener('click',async e=>{
    const reset=e.target.closest('[data-channel-reset]'),del=e.target.closest('[data-channel-delete]');
    if(reset){await api('/api/v1/channels/'+reset.dataset.channelReset+'/stream-key/reset',{method:'POST'});await loadChannels();renderAdminChannels();}
    if(del&&confirm('Delete this channel and its destinations?')){await api('/api/v1/channels/'+del.dataset.channelDelete,{method:'DELETE'});await loadChannels();renderAdminChannels();}
  });
  $('refreshSystemBtn').addEventListener('click',loadSystem);
  $('testBtn').addEventListener('click',async()=>{if(!state.channel)return;await api('/api/v1/channels/'+state.channel.id+'/test/start',{method:'POST'});});
  boot();
})();