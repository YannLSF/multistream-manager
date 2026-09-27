let statusData = null;
let destination = null;
let destinationId = "";

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
  const options = {...opts, cache: "no-store"};
  const headers = {...(opts.headers || {})};

  if(options.body && !headers["Content-Type"]){
    headers["Content-Type"] = "application/json";
  }

  options.headers = headers;

  const response = await fetch(path, options);

  if(response.redirected && response.url.includes("/login")){
    window.location.href = "/login";
    throw new Error("Authentification requise");
  }

  const type = response.headers.get("content-type") || "";
  let data;

  if(type.includes("application/json")){
    data = await response.json();
  }else{
    const text = await response.text();
    data = {error: text};
  }

  if(!response.ok){
    throw new Error(data?.error || `HTTP ${response.status}`);
  }

  return data;
}

function idFromURL(){
  const m = window.location.pathname.match(/^\/destinations\/([^/]+)\/edit\/?$/);
  if(!m) return "";

  try{
    return decodeURIComponent(m[1]);
  }catch{
    return "";
  }
}

function presetById(id){
  return (statusData?.presets || []).find(p => p.id === id) || {
    id: "custom",
    name: "Custom RTMP / RTMPS",
    category: "Custom",
    server_hint: "URL de publication RTMP ou RTMPS de la plateforme.",
    key_mode: "unknown"
  };
}

function videoTracks(){
  return (statusData?.tracks || []).filter(t => t.codec_type === "video");
}

function audioTracks(){
  return (statusData?.tracks || []).filter(t => t.codec_type === "audio");
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
          `<option value="${esc(p.id)}" ${p.id === selected ? "selected" : ""}>${esc(p.name)}</option>`
        ).join("")
      }</optgroup>`
    ).join("");
}

function populateTracks(videoOrder, audioOrder){
  const videos = videoTracks();
  const audios = audioTracks();

  const vf = videos.length ? videos : [{
    video_order: Number(videoOrder),
    label: `Vidéo #${Number(videoOrder) + 1} — source hors ligne`
  }];

  const af = audios.length ? audios : [{
    audio_order: Number(audioOrder),
    label: `Audio #${Number(audioOrder) + 1} — source hors ligne`
  }];

  $("#videoTrack").innerHTML = vf.map(t =>
    `<option value="${Number(t.video_order)}" ${
      Number(t.video_order) === Number(videoOrder) ? "selected" : ""
    }>${esc(t.label)}</option>`
  ).join("");

  $("#audioTrack").innerHTML = af.map(t =>
    `<option value="${Number(t.audio_order)}" ${
      Number(t.audio_order) === Number(audioOrder) ? "selected" : ""
    }>${esc(t.label)}</option>`
  ).join("");
}

function updateHints(){
  const preset = presetById($("#provider").value);

  $("#serverHint").textContent =
    preset.server_hint ||
    "URL de publication RTMP ou RTMPS de la plateforme.";

  const labels = {
    session: "Clé généralement temporaire ou liée à la session.",
    persistent: "Clé généralement persistante : elle peut rester enregistrée.",
    account: "Clé liée au compte ou au canal.",
    unknown: "La durée de validité de la clé dépend de la plateforme."
  };

  const configured = destination?.key_configured
    ? " Une clé est actuellement configurée. Laisser ce champ vide pour la conserver."
    : " Aucune clé n’est actuellement configurée.";

  $("#keyHint").textContent =
    (labels[preset.key_mode] || labels.unknown) + configured;
}

function fillForm(){
  document.title = `Éditer ${destination.name} — Multistream Manager`;
  $("#pageTitle").textContent = `Éditer ${destination.name}`;

  $("#name").value = destination.name || "";
  populatePresets(destination.provider || "custom");
  $("#server").value = destination.server || "";
  $("#streamKey").value = "";

  $("#enabled").checked = destination.enabled === true;
  $("#autoStart").checked = destination.auto_start === true;
  $("#autoAdaptAudio").checked = destination.auto_adapt_audio === true;

  populateTracks(destination.video_track, destination.audio_track);
  updateHints();

  $("#editPageForm").classList.remove("hidden");
}

async function load(){
  destinationId = idFromURL();

  if(!destinationId){
    $("#loadError").textContent = "URL de destination invalide.";
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

$("#provider").addEventListener("change", updateHints);

$("#editPageForm").addEventListener("submit", async e => {
  e.preventDefault();

  $("#formError").classList.add("hidden");
  $("#saveSuccess").classList.add("hidden");

  const payload = {
    id: destinationId,
    name: $("#name").value.trim(),
    provider: $("#provider").value,
    enabled: $("#enabled").checked,
    auto_start: $("#autoStart").checked,
    auto_adapt_audio: $("#autoAdaptAudio").checked,
    server: $("#server").value.trim(),
    stream_key: $("#streamKey").value,
    video_track: Number($("#videoTrack").value || 0),
    audio_track: Number($("#audioTrack").value || 0)
  };

  try{
    destination = await api(
      `/api/destinations/${encodeURIComponent(destinationId)}`,
      {
        method: "PUT",
        body: JSON.stringify(payload)
      }
    );

    $("#streamKey").value = "";
    updateHints();

    $("#saveSuccess").textContent =
      "Destination enregistrée. Si le champ clé était vide, la clé existante a été conservée.";
    $("#saveSuccess").classList.remove("hidden");

    $("#pageTitle").textContent = `Éditer ${destination.name}`;
    document.title = `Éditer ${destination.name} — Multistream Manager`;
  }catch(err){
    $("#formError").textContent = err.message;
    $("#formError").classList.remove("hidden");
  }
});

load();
