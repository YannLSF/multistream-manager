let state = null;
let compatRequestSeq = 0;
let logEntries = [];
let logFilter = 'all';
let previewCtx = null;
let previewHls = null;
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));

async function api(url, options={}) {
  const res = await fetch(url, {headers:{'Content-Type':'application/json',...(options.headers||{})}, cache:'no-store', ...options});
  if(res.status===401){ location.replace('/login'); throw new Error('Authentification requise'); }
  const data = await res.json().catch(()=>({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

function fmtCPU(v){ return `${Number(v||0).toFixed(Number(v||0)>=10?0:1)} %`; }
function fmtBytes(v){
  const n=Number(v||0);
  if(n<1024*1024) return `${(n/1024).toFixed(0)} Ko`;
  return `${(n/1024/1024).toFixed(n>=100*1024*1024?0:1)} Mo`;
}

function fmtUptime(iso){
  if(!iso) return '—';
  const s=Math.max(0,Math.floor((Date.now()-new Date(iso).getTime())/1000));
  if(s<60)return `${s}s`;
  const m=Math.floor(s/60);
  if(m<60)return `${m}m ${s%60}s`;
  return `${Math.floor(m/60)}h ${m%60}m`;
}

function videoTracks(){ return (state?.tracks||[]).filter(t=>t.codec_type==='video'); }
function audioTracks(){ return (state?.tracks||[]).filter(t=>t.codec_type==='audio'); }
function getTrack(kind, order){
  const arr=kind==='video'?videoTracks():audioTracks();
  const key=kind==='video'?'video_order':'audio_order';
  return arr.find(t => Number(t[key])===Number(order));
}
function fallbackTrack(kind, order){
  return {name:`${kind==='video'?'Vidéo':'Audio'} #${Number(order)+1}`,details:'Piste non détectée actuellement',label:`${kind==='video'?'Vidéo':'Audio'} #${Number(order)+1}`};
}
function trackInfo(kind, order){ return getTrack(kind,order) || fallbackTrack(kind,order); }
function presetById(id){ return (state?.presets||[]).find(p=>p.id===id) || (state?.presets||[]).find(p=>p.id==='custom') || {id:'custom',name:'RTMP / RTMPS personnalisé',constraints:{}}; }

function trackTile(t){
  return `<div class="track">
    <div class="track-main"><strong>${esc(t.name || t.label)}</strong><span>${esc(t.details || '')}</span></div>
    <div class="track-tech">Stream #${t.index}${t.codec_type==='video'?` · 0:v:${t.video_order}`:` · 0:a:${t.audio_order}`}</div>
  </div>`;
}

function choiceHTML(kind, order){
  const t=trackInfo(kind,order);
  return `<div class="choice"><strong>${esc(t.name)}</strong><span>${esc(t.details || '')}</span></div>`;
}

function statusBadge(r){
  if(r.running) return '<span class="badge live">● LIVE</span>';
  if(r.retry_in_seconds>0) return `<span class="badge retry">↻ RETRY ${r.retry_in_seconds}s</span>`;
  if(r.last_error) return '<span class="badge error">■ ERREUR</span>';
  return '<span class="badge off">OFFLINE</span>';
}

function compatBadge(c){
  const icons={direct:'●',warning:'▲',adapt_audio:'↻',transcode_video:'■',unknown:'?'};
  const status=c?.status||'unknown';
  const automatic=status==='adapt_audio' && c?.audio_plan?.automatic;
  const klass={direct:'direct',warning:'warning',adapt_audio:automatic?'adapt':'warning',transcode_video:'transcode',unknown:'unknown'};
  return `<span class="compat ${klass[status]||'unknown'}">${icons[status]||'?'} ${esc(c?.label||'Compatibilité inconnue')}</span>`;
}

function compatIssuesHTML(c, limit=3){
  const issues=(c?.issues||[]);
  if(!issues.length) return '';
  const shown=issues.slice(0,limit);
  return `<div class="compat-issues">${shown.map(i=>`<div class="compat-issue ${esc(i.severity)}">${esc(i.message)}</div>`).join('')}${issues.length>limit?`<div class="compat-more">+ ${issues.length-limit} autre(s)</div>`:''}</div>`;
}

function audioPlanHTML(c){
  const p=c?.audio_plan;
  if(!p || p.mode==='unknown') return '';
  if(p.mode==='copy' && (p.reasons||[]).length){
    return `<div class="audio-plan unsupported"><span>▲ Adaptation désactivée</span><strong>Audio source conservé · ${esc(p.label||'copie directe')}</strong></div>`;
  }
  if(p.mode==='copy') return '';
  if(p.mode==='transcode' && p.automatic){
    return `<div class="audio-plan auto"><span>↻ Sortie automatique</span><strong>${esc(p.label||'Audio adapté')}</strong></div>`;
  }
  return `<div class="audio-plan unsupported"><span>▲ Adaptation requise</span><strong>${esc(p.label||'Non automatisable')}</strong></div>`;
}

function render(){
  if(!state) return;
  const s=state.source;
  $('#sourceDot').className='dot '+(s.online?'on':'off');
  $('#sourceText').textContent=s.online?'En ligne':'Hors ligne';
  $('#videoCount').textContent=s.video_count||0;
  $('#audioCount').textContent=s.audio_count||0;
  $('#trackCount').textContent=s.track_count||0;

  const rs=state.resources||{};
  const setResource=(prefix,x={})=>{
    $(`#${prefix}CPU`).textContent=fmtCPU(x.cpu_percent);
    $(`#${prefix}RAM`).textContent=fmtBytes(x.rss_bytes);
  };
  setResource('manager',rs.manager);
  setResource('forwards',rs.forwards);
  setResource('previews',rs.previews);
  setResource('total',rs.total);
  $('#resourceUpdated').textContent=rs.updated_at?`Mis à jour ${new Date(rs.updated_at).toLocaleTimeString()}`:'—';
  $('#logoutBtn').classList.toggle('hidden',!state.auth_enabled);

  const pe=$('#probeError');
  if(s.probe_error){ pe.textContent=s.probe_error; pe.classList.remove('hidden'); }
  else pe.classList.add('hidden');

  const tr=$('#tracks');
  if(!state.tracks?.length){
    tr.className='tracks empty';
    tr.textContent='Aucune piste détectée.';
  } else {
    tr.className='tracks';
    tr.innerHTML=state.tracks.map(trackTile).join('');
  }

  const box=$('#destinations');
  if(!state.destinations?.length){
    box.innerHTML=`<div class="dest"><div class="dest-title">Aucune destination</div><p class="muted">Ajoute un preset ou un endpoint RTMP/RTMPS personnalisé.</p></div>`;
    return;
  }

  box.innerHTML=state.destinations.map(x=>{
    const d=x.config,r=x.runtime,c=x.compatibility||{}, logs=x.logs||{};
    const preset=presetById(d.provider);
    const canStart=s.online && d.enabled;
    const retry=(r.retry_in_seconds>0 && d.enabled && d.auto_start)
      ? `<div class="retry-note">Nouvelle tentative automatique dans ${r.retry_in_seconds}s${r.retry_count>1?` · tentative ${r.retry_count}`:''}</div>`:'';
    const logBadge=(logs.warnings||logs.errors||logs.fatals)
      ? `<span class="log-count ${logs.errors||logs.fatals?'has-errors':'has-warnings'}">${logs.errors||logs.fatals?'■':'▲'} ${(logs.errors||0)+(logs.fatals||0)+(logs.warnings||0)}</span>`:'';
    return `<article class="dest">
      <div class="dest-head">
        <div>
          <div class="dest-title">${esc(d.name)}</div>
          <div class="provider">${esc(preset.name)} · ${d.enabled?'activé':'désactivé'} · ${d.auto_start?'auto':'manuel'}</div>
        </div>
        ${statusBadge(r)}
      </div>
      <div class="compat-row">${compatBadge(c)}</div>
      ${compatIssuesHTML(c)}
      <div class="dest-meta">
        <div class="meta"><label>Vidéo</label>${choiceHTML('video',d.video_track)}</div>
        <div class="meta"><label>Audio</label>${choiceHTML('audio',d.audio_track)}${audioPlanHTML(c)}</div>
        <div class="meta"><label>Clé</label><span>${d.key_configured?'Configurée':'Manquante'}</span></div>
        <div class="meta"><label>Durée</label><span>${r.running?fmtUptime(r.started_at):'—'}</span></div>
        <div class="meta"><label>CPU / RAM</label><span>${r.running?`${fmtCPU(x.resources?.cpu_percent)} · ${fmtBytes(x.resources?.rss_bytes)}`:'—'}</span></div>
      </div>
      ${r.last_error?`<div class="error"><strong>FFmpeg :</strong> ${esc(r.last_error)}</div>`:''}
      ${retry}
      <div class="actions">
        <button class="small preview-btn" ${!s.online?'disabled':''} onclick="openPreview('${esc(d.id)}')">Aperçu${x.preview?.running?' ●':''}</button>
        ${r.running
          ? `<button class="small danger" onclick="stopDest('${esc(d.id)}')">Arrêter</button>`
          : `<button class="small success" ${!canStart?'disabled':''} onclick="startDest('${esc(d.id)}')">Démarrer</button>`}
        <button class="small ghost" onclick="editDest('${esc(d.id)}')">Modifier</button>
        <button class="small ghost logs-btn" onclick="showLogs('${esc(d.id)}')">Logs ${logBadge}</button>
        <button class="small danger" onclick="deleteDest('${esc(d.id)}')">Supprimer</button>
      </div>
    </article>`;
  }).join('');
}

async function refresh(){
  try {
    state=await api('/api/status');
    render();
    syncPreviewDialog();
    if($('#editDialog').open && !$('#provider').options.length) populatePresetSelect();
  } catch(e){ console.error(e); }
}
async function startDest(id){ try{ await api(`/api/destinations/${id}/start`,{method:'POST'}); setTimeout(refresh,250); }catch(e){ alert(e.message); } }
async function stopDest(id){ try{ await api(`/api/destinations/${id}/stop`,{method:'POST'}); setTimeout(refresh,350); }catch(e){ alert(e.message); } }
async function deleteDest(id){
  const name=state?.destinations?.find(x=>x.config.id===id)?.config?.name||id;
  if(!confirm(`Supprimer ${name} ?`))return;
  try{ await api(`/api/destinations/${id}`,{method:'DELETE'}); refresh(); }catch(e){ alert(e.message); }
}

function renderLogs(){
  const box=$('#logsBox');
  const entries=logEntries.filter(e=>logFilter==='all' || (logFilter==='error'?(e.level==='error'||e.level==='fatal'):e.level===logFilter));
  box.innerHTML=entries.length?entries.map(e=>`<div class="log-line ${esc(e.level)}"><span class="log-level">${e.level==='warning'?'▲ WARNING':e.level==='error'?'■ ERROR':e.level==='fatal'?'■ FATAL':'● INFO'}</span><span class="log-message">${esc(e.message)}</span></div>`).join(''):'<div class="log-empty">Aucune ligne dans ce filtre.</div>';
  box.scrollTop=box.scrollHeight;
  $$('.log-filters button').forEach(b=>b.classList.toggle('active',b.dataset.logFilter===logFilter));
}

async function showLogs(id){
  try{
    const r=await api(`/api/destinations/${id}/logs`);
    const name=state?.destinations?.find(x=>x.config.id===id)?.config?.name||id;
    logEntries=r.entries||[];
    logFilter='all';
    $('#logsName').textContent=name;
    $('#warningCount').textContent=r.summary?.warnings||0;
    $('#errorCount').textContent=(r.summary?.errors||0)+(r.summary?.fatals||0);
    renderLogs();
    $('#logsDialog').showModal();
  }catch(e){ alert(e.message); }
}

function populatePresetSelect(selected='kick'){
  const select=$('#provider');
  const groups={};
  for(const p of state?.presets||[]){
    const cat=p.category||'Autre';
    (groups[cat] ||= []).push(p);
  }
  select.innerHTML=Object.entries(groups).map(([cat,items])=>`<optgroup label="${esc(cat)}">${items.map(p=>`<option value="${esc(p.id)}" ${p.id===selected?'selected':''}>${esc(p.name)}</option>`).join('')}</optgroup>`).join('');
  if(!select.value && select.options.length) select.value=select.options[0].value;
  select.dataset.previous=select.value;
}

function populateTrackSelects(videoOrder=0,audioOrder=0){
  const v=videoTracks(),a=audioTracks();
  const vf=v.length?v:[{video_order:Number(videoOrder),label:`Vidéo #${Number(videoOrder)+1} — source hors ligne`}];
  const af=a.length?a:[{audio_order:Number(audioOrder),label:`Audio #${Number(audioOrder)+1} — source hors ligne`}];
  $('#videoTrack').innerHTML=vf.map(t=>`<option value="${t.video_order}" ${Number(t.video_order)===Number(videoOrder)?'selected':''}>${esc(t.label)}</option>`).join('');
  $('#audioTrack').innerHTML=af.map(t=>`<option value="${t.audio_order}" ${Number(t.audio_order)===Number(audioOrder)?'selected':''}>${esc(t.label)}</option>`).join('');
}

function updateProviderHints(fillDefault=false){
  const select=$('#provider');
  const p=presetById(select.value);
  const previous=presetById(select.dataset.previous||select.value);
  const server=$('#server');
  if(fillDefault && (!server.value.trim() || (previous.default_server && server.value.trim()===previous.default_server))){
    server.value=p.default_server||'';
  }
  $('#serverHint').textContent=p.server_hint||'URL de publication RTMP ou RTMPS de la plateforme.';
  const keyLabels={session:'Clé généralement temporaire ou liée à la session.',persistent:'Clé généralement persistante : elle peut rester enregistrée.',account:'Clé liée au compte ou au canal.',unknown:'La durée de validité de la clé dépend de la plateforme.'};
  $('#keyHint').textContent=keyLabels[p.key_mode]||keyLabels.unknown;
  select.dataset.previous=p.id;
  updateFormCompatibility();
}

function onProviderChange(){
  const select=$('#provider');
  const previous=presetById(select.dataset.previous||select.value);
  const current=presetById(select.value);
  if(!$('#destId').value && $('#name').value.trim()===previous.name) $('#name').value=current.name;
  updateProviderHints(true);
}

async function updateFormCompatibility(){
  if(!state?.tracks?.length){
    $('#compatPreview').innerHTML='<span class="compat unknown">? Source hors ligne : compatibilité non vérifiable</span>';
    return;
  }
  const seq=++compatRequestSeq;
  try{
    const c=await api('/api/compatibility',{method:'POST',body:JSON.stringify({
      provider:$('#provider').value,
      video_track:Number($('#videoTrack').value||0),
      audio_track:Number($('#audioTrack').value||0),
      auto_adapt_audio:$('#autoAdaptAudio').checked
    })});
    if(seq!==compatRequestSeq)return;
    $('#compatPreview').innerHTML=`${compatBadge(c)}${compatIssuesHTML(c,5)}${audioPlanHTML(c)}`;
  }catch(e){
    if(seq===compatRequestSeq) $('#compatPreview').innerHTML=`<span class="compat unknown">? ${esc(e.message)}</span>`;
  }
}

function openNew(){
  $('#dialogTitle').textContent='Ajouter une destination';
  $('#destId').value='';
  $('#name').value='Kick';
  populatePresetSelect('kick');
  const p=presetById('kick');
  $('#server').value=p.default_server||'';
  $('#streamKey').value='';
  $('#enabled').checked=true;
  $('#autoStart').checked=true;
  $('#autoAdaptAudio').checked=false;
  populateTrackSelects();
  updateProviderHints(false);
  $('#formError').classList.add('hidden');
  $('#editDialog').showModal();
}

function editDest(id){
  const d=state.destinations.find(x=>x.config.id===id)?.config;
  if(!d)return;
  $('#dialogTitle').textContent=`Modifier ${d.name}`;
  $('#destId').value=d.id;
  $('#name').value=d.name;
  populatePresetSelect(d.provider||'custom');
  $('#server').value=d.server;
  $('#streamKey').value='';
  $('#enabled').checked=d.enabled;
  $('#autoStart').checked=d.auto_start;
  $('#autoAdaptAudio').checked=d.auto_adapt_audio===true;
  populateTrackSelects(d.video_track,d.audio_track);
  updateProviderHints(false);
  $('#formError').classList.add('hidden');
  $('#editDialog').showModal();
}

$('#editForm').addEventListener('submit',async e=>{
  e.preventDefault();
  const id=$('#destId').value;
  const payload={
    id,
    name:$('#name').value.trim(),
    provider:$('#provider').value,
    enabled:$('#enabled').checked,
    auto_start:$('#autoStart').checked,
    auto_adapt_audio:$('#autoAdaptAudio').checked,
    server:$('#server').value.trim(),
    stream_key:$('#streamKey').value,
    video_track:Number($('#videoTrack').value||0),
    audio_track:Number($('#audioTrack').value||0)
  };
  try{
    if(id) await api(`/api/destinations/${id}`,{method:'PUT',body:JSON.stringify(payload)});
    else await api('/api/destinations',{method:'POST',body:JSON.stringify(payload)});
    $('#editDialog').close();
    await refresh();
  } catch(err){
    const el=$('#formError');
    el.textContent=err.message;
    el.classList.remove('hidden');
  }
});

function pickPreviewVideo(preferred){
  const selected=getTrack('video',preferred);
  if(selected?.codec_name?.toLowerCase()==='h264') return Number(preferred);
  const sameOrientation=selected ? videoTracks().find(t=>t.codec_name?.toLowerCase()==='h264' && ((t.width>=t.height)===(selected.width>=selected.height))) : null;
  const fallback=sameOrientation||videoTracks().find(t=>t.codec_name?.toLowerCase()==='h264')||selected||videoTracks()[0];
  return Number(fallback?.video_order ?? preferred ?? 0);
}
function pickPreviewAudio(preferred){
  const selected=getTrack('audio',preferred);
  if(selected?.codec_name?.toLowerCase()==='aac') return Number(preferred);
  const fallback=audioTracks().find(t=>t.codec_name?.toLowerCase()==='aac')||selected||audioTracks()[0];
  return Number(fallback?.audio_order ?? preferred ?? 0);
}

function populatePreviewSelects(videoOrder,audioOrder){
  $('#previewVideoTrack').innerHTML=videoTracks().map(t=>`<option value="${t.video_order}" ${Number(t.video_order)===Number(videoOrder)?'selected':''}>${esc(t.label)}</option>`).join('');
  $('#previewAudioTrack').innerHTML=audioTracks().map(t=>`<option value="${t.audio_order}" ${Number(t.audio_order)===Number(audioOrder)?'selected':''}>${esc(t.label)}</option>`).join('');
  updatePreviewWarning();
}
function updatePreviewWarning(){
  const v=getTrack('video',Number($('#previewVideoTrack').value||0));
  const a=getTrack('audio',Number($('#previewAudioTrack').value||0));
  const notes=[];
  if(v && v.codec_name?.toLowerCase()!=='h264') notes.push(`${v.name} utilise ${v.codec_name?.toUpperCase()} : la lecture dépend du navigateur/OS. Une rendition H.264 est préférable.`);
  if(a && a.codec_name?.toLowerCase()!=='aac') notes.push(`${a.name} utilise ${a.codec_name?.toUpperCase()} : l’aperçu HLS en copie directe requiert actuellement AAC.`);
  const el=$('#previewWarning');
  if(notes.length){el.textContent=notes.join(' ');el.classList.remove('hidden');}else el.classList.add('hidden');
}

function destroyPreviewPlayer(){
  if(previewHls){ previewHls.destroy(); previewHls=null; }
  const video=$('#previewVideo');
  video.pause();
  video.removeAttribute('src');
  video.load();
}

function setPreviewError(message=''){
  const el=$('#previewError');
  if(message){el.textContent=message;el.classList.remove('hidden');}
  else {el.textContent='';el.classList.add('hidden');}
}

function attachPreviewPlayer(url){
  destroyPreviewPlayer();
  setPreviewError();
  const video=$('#previewVideo');
  const source=url + (url.includes('?')?'&':'?') + 't=' + Date.now();
  if(window.Hls && Hls.isSupported()){
    previewHls=new Hls({
      liveSyncDurationCount:2,
      liveMaxLatencyDurationCount:5,
      maxLiveSyncPlaybackRate:1.2,
      manifestLoadingTimeOut:10000,
      manifestLoadingMaxRetry:12,
      manifestLoadingRetryDelay:500,
      fragLoadingMaxRetry:8,
      fragLoadingRetryDelay:500
    });
    previewHls.loadSource(source);
    previewHls.attachMedia(video);
    previewHls.on(Hls.Events.MANIFEST_PARSED,()=>{
      video.play().catch(()=>setPreviewError('Le flux est prêt. Clique sur Lecture si le navigateur a bloqué la lecture automatique.'));
    });
    previewHls.on(Hls.Events.FRAG_LOADED,()=>setPreviewError());
    previewHls.on(Hls.Events.ERROR,(_,data)=>{
      if(!data.fatal)return;
      const detail=data.details?` (${data.details})`:'';
      const status=data.response?.code?` — HTTP ${data.response.code}`:'';
      if(data.type===Hls.ErrorTypes.NETWORK_ERROR){
        setPreviewError(`Erreur réseau HLS${detail}${status}. Nouvelle tentative…`);
        previewHls.startLoad();
      } else if(data.type===Hls.ErrorTypes.MEDIA_ERROR){
        setPreviewError(`Erreur de décodage HLS${detail}. Tentative de récupération…`);
        previewHls.recoverMediaError();
      } else {
        setPreviewError(`Erreur HLS fatale${detail}${status}.`);
      }
    });
  } else if(video.canPlayType('application/vnd.apple.mpegurl')){
    video.src=source;
    video.play().catch(()=>setPreviewError('Le flux est prêt. Clique sur Lecture si le navigateur a bloqué la lecture automatique.'));
  } else {
    setPreviewError('hls.js n’est pas disponible et ce navigateur ne lit pas HLS nativement. Vérifie aussi que le navigateur peut joindre cdn.jsdelivr.net.');
  }
}


function syncPreviewDialog(){
  if(!previewCtx || !$('#previewDialog').open || !state)return;
  const x=state.destinations.find(x=>x.config.id===previewCtx.id);
  if(!x)return;
  const p=x.preview||{};
  if(p.running && p.ready){
    $('#previewState').textContent='● APERÇU ACTIF';
    $('#previewState').classList.add('active');
    const video=$('#previewVideo');
    if(p.playlist && !previewHls && !video.getAttribute('src') && !video.currentSrc) attachPreviewPlayer(p.playlist);
    return;
  }
  $('#previewState').classList.remove('active');
  if(p.running){
    $('#previewState').textContent='DÉMARRAGE DE L’APERÇU…';
    return;
  }
  if(p.last_error){
    destroyPreviewPlayer();
    $('#previewState').textContent='APERÇU EN ERREUR';
    setPreviewError(p.last_error);
  } else {
    $('#previewState').textContent='APERÇU ARRÊTÉ';
  }
}

async function openPreview(id){
  const x=state.destinations.find(x=>x.config.id===id);
  if(!x)return;
  previewCtx={id,name:x.config.name};
  $('#previewName').textContent=x.config.name;
  setPreviewError(x.preview?.last_error||'');
  const running=x.preview?.running;
  const ready=running && x.preview?.ready;
  const videoOrder=running?x.preview.video_track:pickPreviewVideo(x.config.video_track);
  const audioOrder=running?x.preview.audio_track:pickPreviewAudio(x.config.audio_track);
  populatePreviewSelects(videoOrder,audioOrder);
  $('#previewState').textContent=ready?'● APERÇU ACTIF':(running?'DÉMARRAGE DE L’APERÇU…':'APERÇU ARRÊTÉ');
  $('#previewState').classList.toggle('active',!!ready);
  $('#previewDialog').showModal();
  if(ready && x.preview.playlist) attachPreviewPlayer(x.preview.playlist);
}

async function startPreview(){
  if(!previewCtx)return;
  setPreviewError();
  destroyPreviewPlayer();
  $('#previewState').textContent='DÉMARRAGE DE L’APERÇU…';
  $('#previewState').classList.remove('active');
  try{
    const r=await api(`/api/destinations/${previewCtx.id}/preview`,{method:'POST',body:JSON.stringify({
      video_track:Number($('#previewVideoTrack').value||0),
      audio_track:Number($('#previewAudioTrack').value||0)
    })});
    if(!r.ready) throw new Error(r.last_error||'Le premier segment HLS n’est pas encore disponible.');
    $('#previewState').textContent='● APERÇU ACTIF';
    $('#previewState').classList.add('active');
    attachPreviewPlayer(r.playlist);
    setTimeout(refresh,200);
  }catch(e){
    $('#previewState').textContent='APERÇU EN ERREUR';
    $('#previewState').classList.remove('active');
    setPreviewError(e.message);
    setTimeout(refresh,200);
  }
}

async function stopPreview(close=false){
  if(previewCtx){
    try{ await api(`/api/destinations/${previewCtx.id}/preview`,{method:'DELETE'}); }catch(e){ if(!close) alert(e.message); }
  }
  destroyPreviewPlayer();
  $('#previewState').textContent='APERÇU ARRÊTÉ';
  $('#previewState').classList.remove('active');
  if(close){ $('#previewDialog').close(); previewCtx=null; }
  setTimeout(refresh,200);
}


async function downloadProtected(url, filename){
  const res=await fetch(url,{cache:'no-store'});
  if(res.status===401){ location.replace('/login'); return; }
  if(!res.ok){ const d=await res.json().catch(()=>({})); throw new Error(d.error||`HTTP ${res.status}`); }
  const blob=await res.blob();
  const a=document.createElement('a');
  a.href=URL.createObjectURL(blob); a.download=filename; document.body.appendChild(a); a.click(); a.remove();
  setTimeout(()=>URL.revokeObjectURL(a.href),1000);
}

async function uploadJSON(url,file){
  if(!file) return;
  const text=await file.text();
  return api(url,{method:'POST',body:text});
}

async function openHistory(){
  if(!$('#historyDialog').open) $('#historyDialog').showModal();
  const box=$('#historyBox');
  box.innerHTML='<div class="log-empty">Chargement…</div>';
  try{
    const r=await api('/api/errors?limit=250');
    const events=r.events||[];
    $('#historyCount').textContent=`${events.length} événement(s) affiché(s)`;
    box.innerHTML=events.length?events.map(e=>`<div class="history-event">
      <div class="history-time">${esc(new Date(e.time).toLocaleString())}</div>
      <div><strong>${esc(e.destination||e.destination_id||'Système')}</strong><span class="history-kind">${esc(e.kind||'error')}</span></div>
      <div class="history-message">${esc(e.message)}</div>
      ${e.retry_count?`<div class="history-retry">Tentative automatique ${e.retry_count}</div>`:''}
    </div>`).join(''):'<div class="log-empty">Aucune erreur enregistrée.</div>';
  }catch(e){ box.innerHTML=`<div class="error">${esc(e.message)}</div>`; }
}

async function clearHistory(){
  if(!confirm('Vider tout l’historique persistant des erreurs ?')) return;
  try{ await api('/api/errors',{method:'DELETE'}); await openHistory(); }catch(e){ alert(e.message); }
}

async function openManage(){
  $('#manageError').classList.add('hidden');
  $('#presetManageStatus').textContent=`${state?.presets?.length||0} presets chargés depuis /data/presets.json`;
  $('#authManageStatus').textContent=state?.auth_enabled?`Authentification activée · utilisateur : ${state.auth_username||'—'}`:'Authentification désactivée.';
  $('#manageDialog').showModal();
}
function manageError(e){ const el=$('#manageError'); el.textContent=e.message||String(e); el.classList.remove('hidden'); }

async function importConfigFile(file){
  if(!file) return;
  if(!confirm('Importer cette configuration remplacera toutes les destinations actuelles et arrêtera les forwards en cours. Continuer ?')){ $('#configFile').value=''; return; }
  try{
    const r=await uploadJSON('/api/config/import',file);
    $('#manageDialog').close();
    await refresh();
    alert(`${r.destinations||0} destination(s) importée(s).`);
  }catch(e){ manageError(e); }
  finally{ $('#configFile').value=''; }
}

async function importPresetFile(file){
  if(!file) return;
  try{
    const r=await uploadJSON('/api/presets/import',file);
    $('#presetManageStatus').textContent=`Catalogue importé : ${r.presets} presets actifs.`;
    await refresh();
  }catch(e){ manageError(e); }
  finally{ $('#presetsFile').value=''; }
}

async function reloadPresets(){
  try{
    const r=await api('/api/presets/reload',{method:'POST'});
    $('#presetManageStatus').textContent=`Catalogue rechargé : ${r.presets} presets actifs.`;
    await refresh();
  }catch(e){ manageError(e); }
}

async function logout(){
  try{ await api('/api/auth/logout',{method:'POST'}); }catch(_){ }
  location.replace('/login');
}

$('#provider').addEventListener('change',onProviderChange);
$('#videoTrack').addEventListener('change',updateFormCompatibility);
$('#audioTrack').addEventListener('change',updateFormCompatibility);
$('#autoAdaptAudio').addEventListener('change',updateFormCompatibility);
$('#previewVideoTrack').addEventListener('change',updatePreviewWarning);
$('#previewAudioTrack').addEventListener('change',updatePreviewWarning);
$('#addBtn').addEventListener('click',openNew);
$('#refreshBtn').addEventListener('click',refresh);
$('#historyBtn').addEventListener('click',openHistory);
$('#manageBtn').addEventListener('click',openManage);
$('#logoutBtn').addEventListener('click',logout);
$('#closeHistory').onclick=()=>$('#historyDialog').close();
$('#closeManage').onclick=()=>$('#manageDialog').close();
$('#clearHistory').addEventListener('click',clearHistory);
$('#exportConfig').addEventListener('click',()=>downloadProtected('/api/config/export','multistream-config.json').catch(manageError));
$('#importConfig').addEventListener('click',()=>$('#configFile').click());
$('#configFile').addEventListener('change',e=>importConfigFile(e.target.files?.[0]));
$('#exportPresets').addEventListener('click',()=>downloadProtected('/api/presets/export','multistream-presets.json').catch(manageError));
$('#importPresets').addEventListener('click',()=>$('#presetsFile').click());
$('#presetsFile').addEventListener('change',e=>importPresetFile(e.target.files?.[0]));
$('#reloadPresets').addEventListener('click',reloadPresets);
$('#closeDialog').onclick=()=>$('#editDialog').close();
$('#cancelBtn').onclick=()=>$('#editDialog').close();
$('#closeLogs').onclick=()=>$('#logsDialog').close();
$('#startPreview').onclick=startPreview;
$('#stopPreview').onclick=()=>stopPreview(false);
$('#closePreview').onclick=()=>stopPreview(true);
$('#previewDialog').addEventListener('cancel',e=>{e.preventDefault();stopPreview(true);});
$$('.log-filters button').forEach(b=>b.addEventListener('click',()=>{logFilter=b.dataset.logFilter;renderLogs();}));

window.startDest=startDest;
window.stopDest=stopDest;
window.editDest=editDest;
window.deleteDest=deleteDest;
window.showLogs=showLogs;
window.openPreview=openPreview;

refresh();
setInterval(refresh,2000);
setInterval(()=>{if(state)render()},1000);
