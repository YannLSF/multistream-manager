package main

// Preset describes a streaming service and the constraints that Multistream
// Manager can evaluate without transcoding. Empty/zero values mean unknown or
// unrestricted rather than "not supported".
type Preset struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Category      string            `json:"category"`
	Description   string            `json:"description,omitempty"`
	DefaultServer string            `json:"default_server,omitempty"`
	ServerHint    string            `json:"server_hint,omitempty"`
	KeyMode       string            `json:"key_mode,omitempty"` // persistent, session, account, unknown
	Constraints   PresetConstraints `json:"constraints"`
}

type PresetConstraints struct {
	VideoCodecs             []string `json:"video_codecs,omitempty"`
	AudioCodecs             []string `json:"audio_codecs,omitempty"`
	MaxVideoBitrateKbps     int      `json:"max_video_bitrate_kbps,omitempty"`
	VideoBitrateIsHardLimit bool     `json:"video_bitrate_is_hard_limit,omitempty"`
	MaxAudioBitrateKbps     int      `json:"max_audio_bitrate_kbps,omitempty"`
	MaxFPS                  float64  `json:"max_fps,omitempty"`
	MaxWidth                int      `json:"max_width,omitempty"`
	MaxHeight               int      `json:"max_height,omitempty"`
	RecommendedProfile      string   `json:"recommended_profile,omitempty"`
	MaxBFrames              *int     `json:"max_b_frames,omitempty"`
	SampleRates             []int    `json:"sample_rates,omitempty"`
	MaxChannels             int      `json:"max_channels,omitempty"`
	Orientation             string   `json:"orientation,omitempty"` // landscape, portrait, any
	KeyframeSeconds         float64  `json:"keyframe_seconds,omitempty"`
	Notes                   []string `json:"notes,omitempty"`
}

func intPtr(v int) *int { return &v }

var builtInPresetCatalog = []Preset{
	{ID: "kick", Name: "Kick", Category: "Streaming", DefaultServer: "rtmps://fa723fc1b171.global-contribute.live-video.net/app", ServerHint: "Endpoint Kick RTMPS. Le chemin /app est ajouté automatiquement si nécessaire.", KeyMode: "persistent", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "trovo", Name: "Trovo", Category: "Streaming", ServerHint: "Utilise l’URL RTMP/RTMPS fournie par Trovo.", KeyMode: "persistent", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 9000, MaxAudioBitrateKbps: 160, Orientation: "landscape"}},
	{ID: "youtube", Name: "YouTube Live", Category: "Streaming", ServerHint: "Colle l’URL de publication fournie par YouTube Studio.", KeyMode: "persistent", Constraints: PresetConstraints{VideoCodecs: []string{"h264", "hevc", "av1"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "facebook", Name: "Facebook Live", Category: "Social", ServerHint: "Facebook utilise RTMPS pour les encodeurs externes.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 9000, MaxAudioBitrateKbps: 128, RecommendedProfile: "main", Orientation: "landscape"}},
	{ID: "instagram", Name: "Instagram Live", Category: "Social", ServerHint: "Live Producer fournit une URL et une clé temporaires pour la session.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "portrait"}},
	{ID: "tiktok", Name: "TikTok LIVE", Category: "Social", ServerHint: "Disponible lorsque le compte possède l’accès à une clé de stream/encodeur externe.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "portrait"}},
	{ID: "telegram", Name: "Telegram Live", Category: "Social", ServerHint: "Utilise le serveur et la clé RTMP fournis par Telegram.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "linkedin", Name: "LinkedIn Live", Category: "Social", ServerHint: "Utilise l’URL RTMP/RTMPS de l’événement LinkedIn Live.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 6000, VideoBitrateIsHardLimit: true, MaxAudioBitrateKbps: 128, MaxFPS: 30, RecommendedProfile: "baseline", Orientation: "landscape"}},
	{ID: "x", Name: "X / Media Studio", Category: "Social", ServerHint: "Utilise l’URL RTMP/RTMPS de Live Producer / Media Studio.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxAudioBitrateKbps: 128, Orientation: "landscape"}},
	{ID: "niconico", Name: "niconico Live", Category: "Streaming", ServerHint: "Colle le serveur de diffusion fourni par niconico.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 5808, RecommendedProfile: "high", Orientation: "landscape"}},
	{ID: "nimo", Name: "Nimo TV", Category: "Streaming", ServerHint: "Utilise le serveur RTMP fourni dans le centre créateur Nimo.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 6000, MaxAudioBitrateKbps: 160, Orientation: "landscape"}},
	{ID: "fc2", Name: "FC2 Live", Category: "Streaming", ServerHint: "FC2 fournit l’URL serveur et la clé pour les logiciels de diffusion.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 6000, KeyframeSeconds: 2, Orientation: "landscape"}},
	{ID: "goodgame", Name: "GoodGame", Category: "Streaming", ServerHint: "Utilise le serveur RTMP et la clé de ton compte GoodGame.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "odysee", Name: "Odysee", Category: "Streaming", ServerHint: "Odysee fournit un serveur RTMP et une clé pour les encodeurs externes.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "okru", Name: "OK.ru", Category: "Streaming", ServerHint: "Utilise les informations RTMP de la diffusion OK.ru.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "onlyfans", Name: "OnlyFans", Category: "Creator", ServerHint: "Utilise l’endpoint RTMP fourni à ton compte/création de live.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 2500, VideoBitrateIsHardLimit: true, RecommendedProfile: "main", MaxBFrames: intPtr(0), Orientation: "landscape"}},
	{ID: "rumble", Name: "Rumble", Category: "Streaming", ServerHint: "Rumble Direct RTMP fournit l’URL et la clé de publication.", KeyMode: "persistent", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 10000, Orientation: "landscape"}},
	{ID: "scooplive", Name: "Scoop Live", Category: "Streaming", ServerHint: "Utilise l’URL RTMP et la clé fournies par Scoop Live.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 5000, Orientation: "landscape"}},
	{ID: "vaughn", Name: "Vaughn Live", Category: "Streaming", ServerHint: "Choisis un point d’ingest Vaughn Live et colle la clé associée.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "vkvideo", Name: "VK Video Live", Category: "Streaming", ServerHint: "VK Video Live fournit le serveur et la clé dans son studio.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "houla", Name: "Hou.la", Category: "Creator", ServerHint: "Hou.la fournit un endpoint RTMPS et accepte portrait comme paysage.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 6000, Orientation: "any"}},
	{ID: "vimeo", Name: "Vimeo Live", Category: "Streaming", ServerHint: "Utilise l’URL RTMP/RTMPS fournie dans les paramètres de l’événement Vimeo.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "steam", Name: "Steam Broadcasting", Category: "Streaming", ServerHint: "Utilise le serveur et le token RTMP Steam Broadcasting.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 7000, MaxAudioBitrateKbps: 128, RecommendedProfile: "high", Orientation: "landscape"}},
	{ID: "rutube", Name: "RUTUBE", Category: "Streaming", ServerHint: "Utilise le serveur et la clé du studio RUTUBE.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxAudioBitrateKbps: 128, SampleRates: []int{44100}, KeyframeSeconds: 1, Orientation: "landscape"}},
	{ID: "soop", Name: "SOOP / AfreecaTV", Category: "Streaming", ServerHint: "Utilise l’endpoint RTMP du centre créateur SOOP.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 8000, RecommendedProfile: "main", Orientation: "landscape"}},
	{ID: "picarto", Name: "Picarto", Category: "Streaming", ServerHint: "Utilise le serveur RTMP et la clé Picarto.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 3500, RecommendedProfile: "main", Orientation: "landscape"}},
	{ID: "piczel", Name: "Piczel.tv", Category: "Streaming", ServerHint: "Utilise l’endpoint RTMP indiqué par Piczel.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 2500, Orientation: "landscape"}},
	{ID: "mixcloud", Name: "Mixcloud Live", Category: "Streaming", ServerHint: "Utilise l’URL RTMP et la clé Mixcloud Live.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "aparat", Name: "Aparat", Category: "Streaming", ServerHint: "Utilise le serveur RTMP et la clé Aparat.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxVideoBitrateKbps: 6000, Orientation: "landscape"}},
	{ID: "kakaotv", Name: "KakaoTV", Category: "Streaming", ServerHint: "Utilise le serveur RTMP et la clé KakaoTV.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "boosty", Name: "Boosty", Category: "Creator", ServerHint: "Preset sans endpoint figé : colle le serveur et la clé fournis par Boosty si ton compte les expose.", KeyMode: "unknown", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "restream", Name: "Restream.io", Category: "Relay", ServerHint: "Restream peut lui-même être utilisé comme destination RTMP.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "castr", Name: "Castr.io", Category: "Relay", ServerHint: "Utilise l’URL RTMP/RTMPS fournie par Castr.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "livepush", Name: "Livepush", Category: "Relay", ServerHint: "Utilise l’endpoint RTMP/RTMPS fourni par Livepush.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "twitch", Name: "Twitch", Category: "Streaming", ServerHint: "Utilise le serveur d’ingest et la clé de stream Twitch de ton compte.", KeyMode: "persistent", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "dlive", Name: "DLive", Category: "Streaming", ServerHint: "Utilise le serveur RTMP et la clé fournis par DLive.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "uscreen", Name: "Uscreen", Category: "Pro / événementiel", ServerHint: "Utilise les informations RTMP fournies par ton événement Uscreen.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "eplay", Name: "ePlay", Category: "Streaming", ServerHint: "Utilise l’endpoint RTMP fourni par ePlay.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "joystick", Name: "Joystick.TV", Category: "Streaming", ServerHint: "Utilise le serveur RTMP et la clé Joystick.TV. Les contraintes peuvent être plus strictes selon le compte.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "livepeer", Name: "Livepeer Studio", Category: "Pro / événementiel", ServerHint: "Utilise l’URL RTMP ou RTMPS de ton stream Livepeer Studio.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "vrcdn", Name: "VRCDN", Category: "Pro / événementiel", ServerHint: "Utilise l’endpoint RTMP fourni par VRCDN.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "nfhs", Name: "NFHS Network", Category: "Sport", ServerHint: "Utilise les informations RTMP de la diffusion NFHS Network.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "boomstream", Name: "Boomstream", Category: "Pro / événementiel", ServerHint: "Utilise l’URL RTMP et la clé fournies par Boomstream.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "meridix", Name: "Meridix Live Sports", Category: "Sport", ServerHint: "Utilise l’endpoint RTMP de ton événement Meridix.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "landscape"}},
	{ID: "wpstream", Name: "WPStream", Category: "Pro / événementiel", ServerHint: "Utilise le point d’ingest RTMP de la région choisie dans WPStream.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "switchboard", Name: "Switchboard Live", Category: "Relay", ServerHint: "Utilise l’endpoint RTMPS fourni par Switchboard Live.", KeyMode: "account", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "eventlive", Name: "EventLive.pro", Category: "Pro / événementiel", ServerHint: "Utilise le serveur RTMP et la clé de l’événement EventLive.pro.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "lightcast", Name: "Lightcast", Category: "Pro / événementiel", ServerHint: "Utilise l’endpoint RTMP fourni par Lightcast.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "bitmovin", Name: "Bitmovin Live", Category: "Pro / événementiel", ServerHint: "Utilise l’URL RTMP de l’input Bitmovin Live.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "enchant", Name: "Enchant.events", Category: "Pro / événementiel", ServerHint: "Utilise l’endpoint RTMPS fourni par Enchant.events.", KeyMode: "session", Constraints: PresetConstraints{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Orientation: "any"}},
	{ID: "custom", Name: "RTMP / RTMPS personnalisé", Category: "Autre", Description: "Destination universelle sans contrainte prédéfinie.", ServerHint: "URL de publication RTMP ou RTMPS de la plateforme.", KeyMode: "unknown", Constraints: PresetConstraints{Orientation: "any"}},
}
