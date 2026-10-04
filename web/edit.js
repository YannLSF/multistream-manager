let statusData = null;
let destination = null;
let destinationId = "";
let compatibilitySeq = 0;

const $ = q => document.querySelector(q);

function esc(v){
  return String(v ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

async function api(path, opts = {}){
  const options = {...opts, cache:"no-store"};
  const headers = {...(opts.headers || {})};

  if(options.body && !headers["Content-Type"]){
    headers["Content-Type"] = "application/json";
  }

  options.headers = headers;

  const response = await fetch(path, options);

  if(response.status === 401){
    const next = location.pathname + location.search;
    location.replace("/login?next=" + encodeURIComponent(next));
    throw new Error("Authentification requise");
  }

  const type = response.headers.get("content-type") || "";

  const data = type.includes("application/json")
    ? await response.json()
    : {error: await response.text()};

  if(!response.ok){
    throw new Error(data?.error || `HTTP ${response.status}`);
  }

  return data;
}

function idFromURL(){
  const m = location.pathname.match(
    /^\/destinations\/([^/]+)\/edit\/?$/
  );

  if(!m)return "";

  try{
    return decodeURIComponent(m[1]);
  }catch{
    return "";
  }
}

function presetById(id){
  return (statusData?.presets || []).find(p => p.id === id) || {
    id:"custom",
    name:"Custom RTMP / RTMPS",
    category:"Custom",
    server_hint:"URL de publication RTMP ou RTMPS de la plateforme.",
    key_mode:"unknown"
  };
}

function sourceById(id = "primary"){
  const sid = id || "primary";

  const found = (statusData?.sources || []).find(
    src => src.id === sid
  );

  if(found)return found;

  if(sid === "primary"){
    return {
      id:"primary",
      label:"Twitch / Enhanced RTMP",
      primary:true,
      state:statusData?.source || {},
      tracks:statusData?.tracks || []
    };
  }

  return null;
}

function sourceTracks(id){
  return sourceById(id)?.tracks || [];
}

function videoTracks(id){
  return sourceTracks(id).filter(
    t => t.codec_type === "video"
  );
}

function audioTracks(id){
  return sourceTracks(id).filter(
    t => t.codec_type === "audio"
  );
}

function populatePresets(selected){
  const groups = {};

  for(const p of statusData?.presets || []){
    const cat = p.category || "Autre";
    (groups[cat] ||= []).push(p);
  }

  $("#provider").innerHTML = Object.entries(groups)
    .map(([cat, items]) =>
      `<optgroup label="${esc(cat)}">${
        items.map(p =>
          `<option value="${esc(p.id)}" ${
            p.id === selected ? "selected" : ""
          }>${esc(p.name)}</option>`
        ).join("")
      }</optgroup>`
    ).join("");
}

function populateSources(selected = "primary"){
  const sources = statusData?.sources || [];

  $("#sourceId").innerHTML = sources.map(src =>
    `<option value="${esc(src.id)}" ${
      src.id === selected ? "selected" : ""
    }>${esc(src.label || src.id)}${
      src.state?.online === true ? "" : " — hors ligne"
    }</option>`
  ).join("");

  if(!$("#sourceId").value && $("#sourceId").options.length){
    $("#sourceId").value = $("#sourceId").options[0].value;
  }

  updateSourceHint();
}

function updateSourceHint(){
  const src = sourceById($("#sourceId").value || "primary");

  if(!src){
    $("#sourceHint").textContent = "Source inconnue";
    return;
  }

  $("#sourceHint").textContent =
    `${src.state?.online === true ? "En ligne" : "Hors ligne"} · ` +
    `${src.state?.video_count || 0} vidéo · ` +
    `${src.state?.audio_count || 0} audio`;
}

function populateTracks(videoOrder = 0, audioOrder = 0){
  const sourceID = $("#sourceId").value || "primary";

  const videos = videoTracks(sourceID);
  const audios = audioTracks(sourceID);

  const vf = videos.length ? videos : [{
    video_order:Number(videoOrder),
    label:`Vidéo #${Number(videoOrder)+1} — source hors ligne`
  }];

  const af = audios.length ? audios : [{
    audio_order:Number(audioOrder),
    label:`Audio #${Number(audioOrder)+1} — source hors ligne`
  }];

  $("#videoTrack").innerHTML = vf.map(t =>
    `<option value="${Number(t.video_order)}" ${
      Number(t.video_order) === Number(videoOrder)
        ? "selected"
        : ""
    }>${esc(t.label)}</option>`
  ).join("");

  $("#audioTrack").innerHTML = af.map(t =>
    `<option value="${Number(t.audio_order)}" ${
      Number(t.audio_order) === Number(audioOrder)
        ? "selected"
        : ""
    }>${esc(t.label)}</option>`
  ).join("");

  if(videos.length &&
     !videos.some(t =>
       Number(t.video_order) === Number($("#videoTrack").value)
     )){
    $("#videoTrack").value = String(videos[0].video_order);
  }

  if(audios.length &&
     !audios.some(t =>
       Number(t.audio_order) === Number($("#audioTrack").value)
     )){
    $("#audioTrack").value = String(audios[0].audio_order);
  }
}

function updateHints(){
  const preset = presetById($("#provider").value);

  $("#serverHint").textContent =
    preset.server_hint ||
    "URL de publication RTMP ou RTMPS de la plateforme.";

  const labels = {
    session:"Clé généralement temporaire ou liée à la session.",
    persistent:"Clé généralement persistante : elle peut rester enregistrée.",
    account:"Clé liée au compte ou au canal.",
    unknown:"La durée de validité de la clé dépend de la plateforme."
  };

  const configured = destination?.key_configured
    ? " Une clé est actuellement configurée. Laisser ce champ vide pour la conserver."
    : " Aucune clé n’est actuellement configurée.";

  $("#keyHint").textContent =
    (labels[preset.key_mode] || labels.unknown) + configured;
}

function compatibilityHTML(c){
  const icons = {
    direct:"●",
    warning:"▲",
    adapt_audio:"↻",
    transcode_video:"■",
    unknown:"?"
  };

  const automatic =
    c?.status === "adapt_audio" &&
    c?.audio_plan?.automatic;

  const classes = {
    direct:"direct",
    warning:"warning",
    adapt_audio:automatic ? "adapt" : "warning",
    transcode_video:"transcode",
    unknown:"unknown"
  };

  const status = c?.status || "unknown";

  let html =
    `<span class="compat ${classes[status] || "unknown"}">` +
    `${icons[status] || "?"} ` +
    `${esc(c?.label || "Compatibilité inconnue")}` +
    `</span>`;

  const issues = c?.issues || [];

  if(issues.length){
    html +=
      `<div class="compat-issues">` +
      issues.slice(0,5).map(i =>
        `<div class="compat-issue ${esc(i.severity)}">${esc(i.message)}</div>`
      ).join("") +
      `</div>`;
  }

  return html;
}

async function updateCompatibility(){
  const sourceID = $("#sourceId").value || "primary";

  if(!sourceTracks(sourceID).length){
    $("#compatPreview").innerHTML =
      '<span class="compat unknown">? Source hors ligne : compatibilité non vérifiable</span>';
    return;
  }

  const seq = ++compatibilitySeq;

  try{
    const c = await api("/api/compatibility", {
      method:"POST",
      body:JSON.stringify({
        source_id:sourceID,
        provider:$("#provider").value,
        video_track:Number($("#videoTrack").value || 0),
        audio_track:Number($("#audioTrack").value || 0),
        auto_adapt_audio:$("#autoAdaptAudio").checked
      })
    });

    if(seq === compatibilitySeq){
      $("#compatPreview").innerHTML = compatibilityHTML(c);
    }
  }catch(err){
    if(seq === compatibilitySeq){
      $("#compatPreview").innerHTML =
        `<span class="compat unknown">? ${esc(err.message)}</span>`;
    }
  }
}

function fillForm(){
  const sourceID = destination.source_id || "primary";

  document.title =
    `Éditer ${destination.name} — Ylyxium Multistream Manager`;

  $("#pageTitle").textContent =
    `Éditer ${destination.name}`;

  $("#name").value = destination.name || "";

  populatePresets(destination.provider || "custom");
  populateSources(sourceID);

  $("#server").value = destination.server || "";
  $("#streamKey").value = "";

  $("#enabled").checked = destination.enabled === true;
  $("#autoStart").checked = destination.auto_start === true;
  $("#autoAdaptAudio").checked =
    destination.auto_adapt_audio === true;

  populateTracks(
    destination.video_track,
    destination.audio_track
  );

  updateHints();
  updateCompatibility();

  $("#editPageForm").classList.remove("hidden");
}

async function load(){
  destinationId = idFromURL();

  if(!destinationId){
    $("#loadError").textContent =
      "URL de destination invalide.";

    $("#loadError").classList.remove("hidden");
    return;
  }

  try{
    [statusData, destination] = await Promise.all([
      api("/api/status"),
      api(`/api/destinations/${encodeURIComponent(destinationId)}`)
    ]);

    fillForm();
  }catch(err){
    $("#loadError").textContent = err.message;
    $("#loadError").classList.remove("hidden");
  }
}

$("#sourceId").addEventListener("change", () => {
  updateSourceHint();
  populateTracks(0,0);
  updateCompatibility();
});

$("#provider").addEventListener("change", () => {
  updateHints();
  updateCompatibility();
});

$("#videoTrack").addEventListener(
  "change",
  updateCompatibility
);

$("#audioTrack").addEventListener(
  "change",
  updateCompatibility
);

$("#autoAdaptAudio").addEventListener(
  "change",
  updateCompatibility
);

$("#editPageForm").addEventListener("submit", async e => {
  e.preventDefault();

  $("#formError").classList.add("hidden");
  $("#saveSuccess").classList.add("hidden");

  const previous =
    statusData?.destinations?.find(
      x => x.config.id === destinationId
    );

  const payload = {
    id:destinationId,
    name:$("#name").value.trim(),
    provider:$("#provider").value,
    source_id:$("#sourceId").value || "primary",
    enabled:$("#enabled").checked,
    auto_start:$("#autoStart").checked,
    auto_adapt_audio:$("#autoAdaptAudio").checked,
    server:$("#server").value.trim(),
    stream_key:$("#streamKey").value,
    video_track:Number($("#videoTrack").value || 0),
    audio_track:Number($("#audioTrack").value || 0)
  };

  try{
    destination = await api(
      `/api/destinations/${encodeURIComponent(destinationId)}`,
      {
        method:"PUT",
        body:JSON.stringify(payload)
      }
    );

    const selectionChanged =
      previous &&
      (
        (previous.config.source_id || "primary") !== payload.source_id ||
        Number(previous.config.video_track) !== payload.video_track ||
        Number(previous.config.audio_track) !== payload.audio_track
      );

    if(selectionChanged && previous?.preview?.running){
      await api(
        `/api/destinations/${encodeURIComponent(destinationId)}/preview`,
        {method:"DELETE"}
      ).catch(()=>{});
    }

    $("#streamKey").value = "";

    statusData = await api("/api/status");

    populateSources(destination.source_id || "primary");
    populateTracks(
      destination.video_track,
      destination.audio_track
    );

    updateHints();
    updateCompatibility();

    $("#saveSuccess").textContent =
      "Destination enregistrée. Si le champ clé était vide, la clé existante a été conservée.";

    $("#saveSuccess").classList.remove("hidden");

    $("#pageTitle").textContent =
      `Éditer ${destination.name}`;

    document.title =
      `Éditer ${destination.name} — Ylyxium Multistream Manager`;
  }catch(err){
    $("#formError").textContent = err.message;
    $("#formError").classList.remove("hidden");
  }
});

load();
