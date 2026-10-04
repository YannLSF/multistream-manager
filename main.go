package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	appVersion     = "0.5.0"
	enhancedCodecs = "ac-3,av01,avc1,ec-3,fLaC,hvc1,.mp3,mp4a,Opus,vp09"
)

//go:embed web/*
var webFS embed.FS

type Settings struct {
	Bind              string
	DataDir           string
	MTXAPI            string
	MTXRTMPBase       string
	MTXPathPrefix     string
	MTXSourcesPrefix  string
	MediaMTXBin       string
	MediaMTXManaged   bool
	MediaMTXConfig    string
	PollInterval      time.Duration
	FFmpegBin         string
	FFprobeBin        string
	LogRingLines      int
	LogMaxBytes       int64
	LogBackups        int
	ErrorHistoryLimit int
	AuthUsername      string
	AuthPasswordHash  string
	AuthSessionTTL    time.Duration
	AuthCookieSecure  bool
}

type Destination struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Enabled        bool   `json:"enabled"`
	AutoStart      bool   `json:"auto_start"`
	Server         string `json:"server"`
	StreamKey      string `json:"stream_key"`
	SourceID       string `json:"source_id,omitempty"`
	VideoTrack     int    `json:"video_track"`
	AudioTrack     int    `json:"audio_track"`
	AutoAdaptAudio bool   `json:"auto_adapt_audio"`
}

type StoredConfig struct {
	Destinations []Destination `json:"destinations"`
}

type PublicDestination struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Provider       string `json:"provider"`
	Enabled        bool   `json:"enabled"`
	AutoStart      bool   `json:"auto_start"`
	Server         string `json:"server"`
	KeyConfigured  bool   `json:"key_configured"`
	SourceID       string `json:"source_id"`
	VideoTrack     int    `json:"video_track"`
	AudioTrack     int    `json:"audio_track"`
	AutoAdaptAudio bool   `json:"auto_adapt_audio"`
}

type RuntimeState struct {
	Running       bool      `json:"running"`
	PID           int       `json:"pid,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	RetryCount    int       `json:"retry_count,omitempty"`
	RetryIn       int       `json:"retry_in_seconds,omitempty"`
	ManualStopped bool      `json:"-"`
	Stopping      bool      `json:"-"`
	LastExit      time.Time `json:"-"`
	NextRetry     time.Time `json:"-"`
	LastLogs      string    `json:"-"`
}

type Track struct {
	Index      int    `json:"index"`
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	Profile    string `json:"profile,omitempty"`
	Level      int    `json:"level,omitempty"`
	HasBFrames int    `json:"has_b_frames,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	FrameRate  string `json:"frame_rate,omitempty"`
	BitRate    int64  `json:"bit_rate,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Channels   int    `json:"channels,omitempty"`
	Name       string `json:"name"`
	Details    string `json:"details"`
	Label      string `json:"label"`
	VideoOrder int    `json:"video_order"`
	AudioOrder int    `json:"audio_order"`
}

type SourceState struct {
	Online      bool      `json:"online"`
	Since       time.Time `json:"since,omitempty"`
	TrackCount  int       `json:"track_count"`
	VideoCount  int       `json:"video_count"`
	AudioCount  int       `json:"audio_count"`
	ProbeError  string    `json:"probe_error,omitempty"`
	LastUpdated time.Time `json:"last_updated,omitempty"`
}

type DestinationStatus struct {
	Config        PublicDestination   `json:"config"`
	Runtime       RuntimeState        `json:"runtime"`
	Compatibility CompatibilityResult `json:"compatibility"`
	Logs          LogSummary          `json:"logs"`
	Preview       PreviewState        `json:"preview"`
	Resources     ProcessResources    `json:"resources"`
}

type AppStatus struct {
	Source       SourceState         `json:"source"`
	Tracks       []Track             `json:"tracks"`
	Sources      []SourceStatus      `json:"sources"`
	Presets      []Preset            `json:"presets"`
	Destinations []DestinationStatus `json:"destinations"`
	Resources    ResourceSummary     `json:"resources"`
	AuthEnabled  bool                `json:"auth_enabled"`
	AuthUsername string              `json:"auth_username,omitempty"`
}

type ringLog struct {
	mu         sync.Mutex
	lines      []string
	max        int
	buf        string
	redactions []string
	filePath   string
	maxBytes   int64
	backups    int
}

func newRingLog(max int, redactions ...string) *ringLog {
	clean := make([]string, 0, len(redactions))
	for _, v := range redactions {
		if strings.TrimSpace(v) != "" {
			clean = append(clean, v)
		}
	}
	return &ringLog{max: max, redactions: clean}
}

func newPersistentRingLog(max int, filePath string, maxBytes int64, backups int, redactions ...string) *ringLog {
	r := newRingLog(max, redactions...)
	r.filePath = filePath
	r.maxBytes = maxBytes
	r.backups = backups
	return r
}

func (r *ringLog) persistLineLocked(line string) {
	if r.filePath == "" || r.maxBytes <= 0 || r.backups < 0 {
		return
	}
	_ = os.MkdirAll(filepath.Dir(r.filePath), 0o700)
	need := int64(len(line) + 1)
	if st, err := os.Stat(r.filePath); err == nil && st.Size()+need > r.maxBytes {
		if r.backups == 0 {
			_ = os.Remove(r.filePath)
		} else {
			_ = os.Remove(fmt.Sprintf("%s.%d", r.filePath, r.backups))
			for i := r.backups - 1; i >= 1; i-- {
				_ = os.Rename(fmt.Sprintf("%s.%d", r.filePath, i), fmt.Sprintf("%s.%d", r.filePath, i+1))
			}
			_ = os.Rename(r.filePath, r.filePath+".1")
		}
	}
	f, err := os.OpenFile(r.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = io.WriteString(f, line+"\n")
	_ = f.Close()
}

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	chunk := string(p)
	for _, secret := range r.redactions {
		chunk = strings.ReplaceAll(chunk, secret, "***")
	}
	r.buf += chunk
	for {
		idx := strings.IndexByte(r.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(r.buf[:idx], "\r")
		r.buf = r.buf[idx+1:]
		if line != "" {
			r.lines = append(r.lines, line)
			if len(r.lines) > r.max {
				r.lines = append([]string(nil), r.lines[len(r.lines)-r.max:]...)
			}
			r.persistLineLocked(line)
		}
	}
	return len(p), nil
}

func (r *ringLog) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.lines...)
	if strings.TrimSpace(r.buf) != "" {
		out = append(out, r.buf)
	}
	return strings.Join(out, "\n")
}

func (r *ringLog) Tail(n int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.lines...)
	if strings.TrimSpace(r.buf) != "" {
		out = append(out, r.buf)
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return strings.Join(out, "\n")
}

type procState struct {
	cmd      *exec.Cmd
	log      *ringLog
	sourceID string
}

type App struct {
	settings Settings
	auth     *AuthManager

	mu                sync.Mutex
	config            StoredConfig
	source            SourceState
	sourcePath        string
	tracks            []Track
	sources           map[string]*sourceEntry
	runtime           map[string]*RuntimeState
	processes         map[string]*procState
	previews          map[string]*previewProc
	previewErrors     map[string]string
	shuttingDown      bool
	lastProbeAt       time.Time
	lastProbedPath    string
	resources         ResourceSummary
	resourcePrev      map[int]procCPUSample
	resourceByDest    map[string]ProcessResources
	resourceByPreview map[string]ProcessResources
	clockTicks        float64

	historyMu    sync.Mutex
	errorHistory []ErrorEvent
}

func main() {
	if handleHashPasswordCLI() {
		return
	}
	settings := loadSettings()
	if err := prepareRuntimeDataDir(settings); err != nil {
		log.Fatalf("cannot prepare data directory: %v", err)
	}
	logRuntimeDiagnostics(settings)

	if err := loadPresetCatalog(settings.DataDir); err != nil {
		log.Fatalf("cannot load preset catalog: %v", err)
	}
	auth, err := newAuthManager(settings)
	if err != nil {
		log.Fatalf("cannot configure authentication: %v", err)
	}
	app := &App{
		settings:          settings,
		auth:              auth,
		sources:           make(map[string]*sourceEntry),
		runtime:           make(map[string]*RuntimeState),
		processes:         make(map[string]*procState),
		previews:          make(map[string]*previewProc),
		previewErrors:     make(map[string]string),
		resourcePrev:      make(map[int]procCPUSample),
		resourceByDest:    make(map[string]ProcessResources),
		resourceByPreview: make(map[string]ProcessResources),
		clockTicks:        detectClockTicks(),
	}
	if err := app.loadConfig(); err != nil {
		log.Fatalf("cannot load config: %v", err)
	}
	if err := app.loadErrorHistory(); err != nil {
		log.Fatalf("cannot load error history: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mediaMTX := newMediaMTXSupervisor(settings)
	if err := mediaMTX.Start(ctx); err != nil {
		log.Fatalf("cannot start managed MediaMTX: %v", err)
	}
	defer func() {
		_ = mediaMTX.Stop()
	}()

	go app.monitor(ctx)

	mux := http.NewServeMux()
	app.registerAuthAPI(mux)
	app.registerAPI(mux)
	app.registerManagementAPI(mux)

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		b, err := fs.ReadFile(sub, "login.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/destinations/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}

		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/destinations/"), "/")
		parts := strings.Split(rest, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "edit" {
			http.NotFound(w, r)
			return
		}

		b, err := fs.ReadFile(sub, "edit.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})

	mux.Handle("/", http.FileServer(http.FS(sub)))

	srv := &http.Server{Addr: settings.Bind, Handler: loggingMiddleware(app.authMiddleware(mux)), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		app.stopAllPreviews()
		app.stopAll(false)
		_ = mediaMTX.Stop()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Printf("Multistream Manager v%s listening on %s", appVersion, settings.Bind)
	log.Printf("MediaMTX API: %s ; RTMP: %s ; path prefix: %q", settings.MTXAPI, settings.MTXRTMPBase, settings.MTXPathPrefix)
	if app.auth.Enabled() {
		log.Printf("Web authentication enabled for user %q", settings.AuthUsername)
	} else {
		log.Printf("WARNING: Web authentication is disabled")
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		app.stopAllPreviews()
		app.stopAll(false)
		_ = mediaMTX.Stop()
		log.Fatal(err)
	}
}

func loadSettings() Settings {
	layout := detectRuntimeLayout()
	poll := 2 * time.Second
	if s := os.Getenv("POLL_SECONDS"); s != "" {
		if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 0.5 {
			poll = time.Duration(f * float64(time.Second))
		}
	}
	sessionHours := envInt("AUTH_SESSION_HOURS", 24)
	if sessionHours < 1 {
		sessionHours = 24
	}

	mediaMTXBin := strings.TrimSpace(os.Getenv("MEDIAMTX_BIN"))
	mediaMTXManagedDefault := false

	if mediaMTXBin != "" {
		mediaMTXManagedDefault = true
	} else if layout.MediaMTXBin != "" {
		mediaMTXBin = layout.MediaMTXBin
		mediaMTXManagedDefault = layout.Portable
	}

	mediaMTXManaged := envBool(
		"MEDIAMTX_MANAGED",
		mediaMTXManagedDefault,
	)

	if mediaMTXManaged && mediaMTXBin == "" {
		mediaMTXBin = "mediamtx"
	}

	return Settings{
		Bind:              envDefault("BIND", ":8090"),
		DataDir:           envDefault("DATA_DIR", layout.DataDir),
		MTXAPI:            strings.TrimRight(envDefault("MTX_API", "http://127.0.0.1:9999"), "/"),
		MTXRTMPBase:       strings.TrimRight(envDefault("MTX_RTMP_BASE", "rtmp://127.0.0.1:1938"), "/"),
		MTXPathPrefix:     envDefault("MTX_PATH_PREFIX", "app/"),
		MTXSourcesPrefix:  envDefault("MTX_SOURCES_PREFIX", "sources/"),
		MediaMTXBin:       mediaMTXBin,
		MediaMTXManaged:   mediaMTXManaged,
		MediaMTXConfig:    strings.TrimSpace(os.Getenv("MEDIAMTX_CONFIG")),
		PollInterval:      poll,
		FFmpegBin:         envDefault("FFMPEG_BIN", layout.FFmpegBin),
		FFprobeBin:        envDefault("FFPROBE_BIN", layout.FFprobeBin),
		LogRingLines:      envInt("LOG_RING_LINES", 500),
		LogMaxBytes:       int64(envInt("LOG_MAX_BYTES", 2*1024*1024)),
		LogBackups:        envInt("LOG_BACKUPS", 3),
		ErrorHistoryLimit: envInt("ERROR_HISTORY_LIMIT", 500),
		AuthUsername:      strings.TrimSpace(os.Getenv("AUTH_USERNAME")),
		AuthPasswordHash:  strings.TrimSpace(os.Getenv("AUTH_PASSWORD_HASH")),
		AuthSessionTTL:    time.Duration(sessionHours) * time.Hour,
		AuthCookieSecure:  envBool("AUTH_COOKIE_SECURE", false),
	}
}

func envInt(k string, d int) int {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

func envBool(k string, d bool) bool {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return d
}

func envDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (a *App) configPath() string { return filepath.Join(a.settings.DataDir, "config.json") }

func (a *App) loadConfig() error {
	b, err := os.ReadFile(a.configPath())
	if errors.Is(err, os.ErrNotExist) {
		a.config = StoredConfig{Destinations: []Destination{}}
		return a.saveConfigLocked()
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a.config); err != nil {
		return err
	}

	migrated := false

	for i := range a.config.Destinations {
		d := &a.config.Destinations[i]

		if d.ID == "" {
			d.ID = slugID(d.Name)
			migrated = true
		}

		sourceID := normalizedSourceID(d.SourceID)
		if d.SourceID != sourceID {
			d.SourceID = sourceID
			migrated = true
		}

		if _, ok := a.runtime[d.ID]; !ok {
			a.runtime[d.ID] = &RuntimeState{}
		}
	}

	if migrated {
		return a.saveConfigLocked()
	}

	return nil
}
func (a *App) saveConfigLocked() error {
	b, err := json.MarshalIndent(a.config, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.configPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.configPath())
}

func slugID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = fmt.Sprintf("dest-%d", time.Now().UnixNano())
	}
	return out
}

func (a *App) monitor(ctx context.Context) {
	ticker := time.NewTicker(a.settings.PollInterval)
	defer ticker.Stop()
	a.pollOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollOnce()
		}
	}
}

func (a *App) pollOnce() {
	readyPaths, err := a.fetchReadyPaths()
	path, online := a.primarySourcePath(readyPaths)
	now := time.Now()

	a.mu.Lock()

	wasOnline := a.source.Online
	oldPath := a.sourcePath

	if err != nil {
		a.source.Online = false
		a.source.ProbeError = "MediaMTX API inaccessible"
	} else {
		a.source.Online = online
		a.sourcePath = path
		a.source.ProbeError = ""
	}

	primaryChanged := wasOnline && online && oldPath != path
	primaryCameOnline := online && (!wasOnline || oldPath != path)
	primaryWentOffline := wasOnline && !online

	if primaryCameOnline {
		a.source.Since = now
	}

	a.source.LastUpdated = now
	a.mu.Unlock()

	if online {
		a.mu.Lock()
		shouldProbe := a.lastProbedPath != path ||
			len(a.tracks) == 0 ||
			time.Since(a.lastProbeAt) >= 30*time.Second
		a.mu.Unlock()

		if shouldProbe {
			tracks, probeErr := a.probeTracks(path)

			a.mu.Lock()
			a.lastProbeAt = time.Now()
			a.lastProbedPath = path

			if probeErr != nil {
				a.source.ProbeError = shortErr(probeErr)
			} else {
				a.tracks = tracks
				a.source.TrackCount = len(tracks)
				a.source.VideoCount, a.source.AudioCount = countTracks(tracks)
				a.source.ProbeError = ""
			}

			a.mu.Unlock()
		}
	} else {
		a.mu.Lock()
		a.tracks = nil
		a.source.TrackCount = 0
		a.source.VideoCount = 0
		a.source.AudioCount = 0
		a.lastProbedPath = ""
		a.mu.Unlock()
	}

	changes := a.syncSourceCatalog(readyPaths, err, now)

	if primaryWentOffline || primaryChanged {
		changes.Offline = append(changes.Offline, primarySourceID)
	}

	if primaryCameOnline {
		changes.Online = append(changes.Online, primarySourceID)
	}

	for _, sourceID := range changes.Offline {
		a.stopOutputsForSource(sourceID)
	}

	for _, sourceID := range changes.Online {
		a.resetSourceRuntimes(sourceID)
	}

	a.ensureAutoStarts()
	a.sampleResources()
}
func (a *App) fetchSourcePath() (string, bool, error) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(a.settings.MTXAPI + "/v3/paths/list")
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", false, fmt.Errorf("MediaMTX API returned %s", resp.Status)
	}
	var data struct {
		Items []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", false, err
	}
	for _, it := range data.Items {
		if it.Ready && strings.HasPrefix(it.Name, a.settings.MTXPathPrefix) {
			return it.Name, true, nil
		}
	}
	return "", false, nil
}

func (a *App) sourceURL(path string) string { return a.settings.MTXRTMPBase + "/" + path }

func (a *App) probeTracks(path string) ([]Track, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.settings.FFprobeBin,
		"-v", "error",
		"-rtmp_enhanced_codecs", enhancedCodecs,
		"-show_entries", "stream=index,codec_type,codec_name,profile,level,has_b_frames,width,height,r_frame_rate,bit_rate,sample_rate,channels",
		"-of", "json",
		a.sourceURL(path),
	)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("ffprobe timeout")
	}
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Streams []struct {
			Index      int    `json:"index"`
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Profile    string `json:"profile"`
			Level      int    `json:"level"`
			HasBFrames int    `json:"has_b_frames"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			FrameRate  string `json:"r_frame_rate"`
			BitRate    string `json:"bit_rate"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, err
	}

	landscape, portrait, audio := 0, 0, 0
	videoOrder := 0
	tracks := make([]Track, 0, len(parsed.Streams))
	for _, s := range parsed.Streams {
		t := Track{
			Index: s.Index, CodecType: s.CodecType, CodecName: s.CodecName,
			Profile: s.Profile, Level: s.Level, HasBFrames: s.HasBFrames,
			Width: s.Width, Height: s.Height, FrameRate: s.FrameRate,
			BitRate: parseInt64(s.BitRate), SampleRate: parseInt(s.SampleRate), Channels: s.Channels,
		}
		if s.CodecType == "video" {
			t.VideoOrder = videoOrder
			videoOrder++
			if s.Width >= s.Height {
				landscape++
				t.Name = fmt.Sprintf("Landscape #%d", landscape)
			} else {
				portrait++
				t.Name = fmt.Sprintf("Portrait #%d", portrait)
			}
			parts := []string{fmt.Sprintf("%d×%d", s.Width, s.Height), prettyFPS(s.FrameRate) + " fps", prettyCodec(s.CodecName)}
			if t.BitRate > 0 {
				parts = append(parts, prettyBitRate(t.BitRate))
			}
			if t.Profile != "" {
				parts = append(parts, "profil "+t.Profile)
			}
			if t.HasBFrames > 0 {
				parts = append(parts, fmt.Sprintf("B-frames %d", t.HasBFrames))
			}
			t.Details = strings.Join(parts, " · ")
		} else if s.CodecType == "audio" {
			t.AudioOrder = audio
			audio++
			t.Name = fmt.Sprintf("Audio #%d", audio)
			parts := []string{prettyCodec(s.CodecName)}
			if t.SampleRate > 0 {
				parts = append(parts, prettySampleRate(t.SampleRate))
			}
			if t.Channels > 0 {
				parts = append(parts, prettyChannels(t.Channels))
			}
			if t.BitRate > 0 {
				parts = append(parts, prettyBitRate(t.BitRate))
			}
			t.Details = strings.Join(parts, " · ")
		} else {
			t.Name = fmt.Sprintf("Stream #%d", s.Index)
			t.Details = s.CodecType
		}
		if t.Details != "" {
			t.Label = t.Name + " — " + t.Details
		} else {
			t.Label = t.Name
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func prettyFPS(v string) string {
	parts := strings.Split(v, "/")
	if len(parts) != 2 {
		return v
	}
	n, e1 := strconv.ParseFloat(parts[0], 64)
	d, e2 := strconv.ParseFloat(parts[1], 64)
	if e1 != nil || e2 != nil || d == 0 {
		return v
	}
	f := n / d
	if f == float64(int(f)) {
		return strconv.Itoa(int(f))
	}
	return fmt.Sprintf("%.2f", f)
}

func parseInt64(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func parseInt(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}

func prettyCodec(v string) string {
	switch strings.ToLower(v) {
	case "h264":
		return "H.264"
	case "hevc", "h265":
		return "HEVC"
	case "aac":
		return "AAC"
	case "av1":
		return "AV1"
	case "vp9":
		return "VP9"
	case "opus":
		return "Opus"
	default:
		return strings.ToUpper(v)
	}
}

func prettyBitRate(v int64) string {
	if v >= 1_000_000 {
		return fmt.Sprintf("~%.1f Mb/s", float64(v)/1_000_000)
	}
	if v >= 1000 {
		return fmt.Sprintf("~%d kb/s", (v+500)/1000)
	}
	return fmt.Sprintf("%d b/s", v)
}

func prettySampleRate(v int) string {
	if v%1000 == 0 {
		return fmt.Sprintf("%d kHz", v/1000)
	}
	return fmt.Sprintf("%.1f kHz", float64(v)/1000)
}

func prettyChannels(v int) string {
	switch v {
	case 1:
		return "mono"
	case 2:
		return "stéréo"
	default:
		return fmt.Sprintf("%d canaux", v)
	}
}

func countTracks(ts []Track) (int, int) {
	v, au := 0, 0
	for _, t := range ts {
		if t.CodecType == "video" {
			v++
		}
		if t.CodecType == "audio" {
			au++
		}
	}
	return v, au
}

func (a *App) ensureAutoStarts() {
	a.mu.Lock()

	if a.shuttingDown {
		a.mu.Unlock()
		return
	}

	now := time.Now()
	ids := []string{}

	for _, d := range a.config.Destinations {
		sourcePath, sourceState, tracks, ok := a.sourceSelectionLocked(d.SourceID)
		if !ok || !sourceState.Online || sourcePath == "" || len(tracks) == 0 {
			continue
		}

		rt := a.getRuntimeLocked(d.ID)
		retryReady := rt.NextRetry.IsZero() || !now.Before(rt.NextRetry)

		if d.Enabled &&
			d.AutoStart &&
			!rt.Running &&
			!rt.ManualStopped &&
			retryReady {
			ids = append(ids, d.ID)
		}
	}

	a.mu.Unlock()

	for _, id := range ids {
		_ = a.startDestination(id, false)
	}
}
func (a *App) getRuntimeLocked(id string) *RuntimeState {
	if a.runtime[id] == nil {
		a.runtime[id] = &RuntimeState{}
	}
	return a.runtime[id]
}

func (a *App) findDestLocked(id string) (int, *Destination) {
	for i := range a.config.Destinations {
		if a.config.Destinations[i].ID == id {
			return i, &a.config.Destinations[i]
		}
	}
	return -1, nil
}

func (a *App) startDestination(id string, manual bool) error {
	a.mu.Lock()

	_, dptr := a.findDestLocked(id)
	if dptr == nil {
		a.mu.Unlock()
		return fmt.Errorf("destination not found")
	}

	d := *dptr
	d.SourceID = normalizedSourceID(d.SourceID)

	rt := a.getRuntimeLocked(id)
	if rt.Running {
		a.mu.Unlock()
		return nil
	}

	sourceID := d.SourceID
	sourcePath, sourceState, tracks, ok := a.sourceSelectionLocked(sourceID)

	if !ok || !sourceState.Online || sourcePath == "" {
		a.mu.Unlock()
		return fmt.Errorf("source %q is offline", sourceID)
	}

	if !d.Enabled {
		a.mu.Unlock()
		return fmt.Errorf("destination is disabled")
	}

	if d.Server == "" || d.StreamKey == "" {
		a.mu.Unlock()
		return fmt.Errorf("server or stream key missing")
	}

	if manual {
		rt.ManualStopped = false
		rt.RetryCount = 0
		rt.NextRetry = time.Time{}
		rt.RetryIn = 0
	}

	video := findTrackByOrder(tracks, "video", d.VideoTrack)
	audio := findTrackByOrder(tracks, "audio", d.AudioTrack)

	src := a.sourceURL(sourcePath)
	out := strings.TrimRight(d.Server, "/") + "/" + strings.TrimLeft(d.StreamKey, "/")

	a.mu.Unlock()

	args, audioPlan, err := buildDestinationFFmpegArgs(
		src,
		out,
		d,
		presetByID(d.Provider),
		video,
		audio,
	)
	if err != nil {
		message := shortErr(err)

		a.mu.Lock()
		rt := a.getRuntimeLocked(id)
		rt.LastError = message
		a.mu.Unlock()

		a.recordError("forward", id, d.Name, message, 0)
		return err
	}

	cmd := exec.Command(a.settings.FFmpegBin, args...)

	lines := a.settings.LogRingLines
	if lines <= 0 {
		lines = 500
	}

	logger := newRingLog(lines, d.StreamKey, sourcePath)

	if a.settings.DataDir != "" && a.settings.LogMaxBytes > 0 {
		logger = newPersistentRingLog(
			lines,
			filepath.Join(a.settings.DataDir, "logs", slugID(d.ID)+".log"),
			a.settings.LogMaxBytes,
			a.settings.LogBackups,
			d.StreamKey,
			sourcePath,
		)
	}

	cmd.Stdout = io.Discard
	cmd.Stderr = logger

	if err := cmd.Start(); err != nil {
		message := shortErr(err)

		a.mu.Lock()
		rt := a.getRuntimeLocked(id)
		rt.LastError = message
		a.mu.Unlock()

		a.recordError("forward", id, d.Name, message, 0)
		return err
	}

	a.mu.Lock()

	rt = a.getRuntimeLocked(id)
	rt.Running = true
	rt.PID = cmd.Process.Pid
	rt.StartedAt = time.Now()
	rt.LastError = ""

	a.processes[id] = &procState{
		cmd:      cmd,
		log:      logger,
		sourceID: sourceID,
	}

	a.mu.Unlock()

	log.Printf(
		"destination %s started from source %s (pid %d, audio %s)",
		d.Name,
		sourceID,
		cmd.Process.Pid,
		audioPlan.Mode,
	)

	go func() {
		err := cmd.Wait()
		rawLogs := logger.String()

		// FFmpeg can exit just before the MediaMTX poller sees the selected
		// source disappear. Give that source one poll cycle to catch up.
		if err != nil && isSourceDemuxInputClosed(rawLogs) {
			grace := a.settings.PollInterval + 500*time.Millisecond

			if grace < 750*time.Millisecond {
				grace = 750 * time.Millisecond
			}

			if grace > 15*time.Second {
				grace = 15 * time.Second
			}

			deadline := time.Now().Add(grace)

			for time.Now().Before(deadline) {
				a.mu.Lock()
				offline := !a.sourceOnlineLocked(sourceID)
				rtNow := a.getRuntimeLocked(id)
				stoppingNow := rtNow.Stopping ||
					rtNow.ManualStopped ||
					a.shuttingDown
				a.mu.Unlock()

				if offline || stoppingNow {
					break
				}

				time.Sleep(50 * time.Millisecond)
			}
		}

		var historyMessage string
		var historyRetry int

		a.mu.Lock()

		rt := a.getRuntimeLocked(id)
		stopping := rt.Stopping
		uptime := time.Since(rt.StartedAt)

		rt.Running = false
		rt.PID = 0
		rt.Stopping = false
		rt.LastExit = time.Now()
		rt.LastLogs = rawLogs
		rt.RetryIn = 0

		sourceEnded := err != nil &&
			isSourceDemuxInputClosed(rawLogs) &&
			!a.sourceOnlineLocked(sourceID)

		if sourceEnded ||
			rt.ManualStopped ||
			stopping ||
			a.shuttingDown {

			rt.LastError = ""
			rt.RetryCount = 0
			rt.NextRetry = time.Time{}
		} else if err != nil {
			if uptime >= 30*time.Second {
				rt.RetryCount = 0
			}

			rt.RetryCount++

			delay := retryDelay(rt.RetryCount)
			rt.NextRetry = time.Now().Add(delay)
			rt.LastError = summarizeFFmpegError(err, rt.LastLogs)

			historyMessage = rt.LastError
			historyRetry = rt.RetryCount
		}

		delete(a.processes, id)
		a.mu.Unlock()

		if historyMessage != "" {
			a.recordError(
				"forward",
				id,
				d.Name,
				historyMessage,
				historyRetry,
			)
		}

		log.Printf("destination %s stopped", d.Name)
	}()

	return nil
}
func (a *App) stopDestination(id string, manual bool) error {
	a.mu.Lock()
	rt := a.getRuntimeLocked(id)
	if manual {
		rt.ManualStopped = true
	}
	rt.Stopping = true
	ps := a.processes[id]
	a.mu.Unlock()
	if ps == nil || ps.cmd == nil || ps.cmd.Process == nil {
		return nil
	}
	if err := stopProcess(ps.cmd.Process); err != nil {
		_ = ps.cmd.Process.Kill()
		return err
	}
	return nil
}

func (a *App) stopAll(manual bool) {
	a.mu.Lock()
	if !manual {
		a.shuttingDown = true
	}
	ids := make([]string, 0, len(a.processes))
	for id := range a.processes {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		_ = a.stopDestination(id, manual)
	}
	if !manual {
		time.Sleep(300 * time.Millisecond)
		a.mu.Lock()
		a.shuttingDown = false
		a.mu.Unlock()
	}
}

func (a *App) waitStopped(id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		running := a.getRuntimeLocked(id).Running
		a.mu.Unlock()
		if !running {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func (a *App) restartWhenStopped(id string) {
	if !a.waitStopped(id, 3*time.Second) {
		return
	}
	_ = a.startDestination(id, false)
}

func retryDelay(count int) time.Duration {
	if count < 1 {
		count = 1
	}
	d := 10 * time.Second
	for i := 1; i < count && d < 60*time.Second; i++ {
		d *= 2
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

func summarizeFFmpegError(err error, logs string) string {
	lines := strings.Split(logs, "\n")
	keywords := []string{"error", "failed", "invalid", "refused", "denied", "timed out", "not found", "unsupported"}
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		low := strings.ToLower(line)
		for _, keyword := range keywords {
			if strings.Contains(low, keyword) {
				if len(line) > 420 {
					line = line[:420] + "…"
				}
				return line
			}
		}
	}
	return shortErr(err)
}

func normalizeServer(provider, raw string) string {
	s := strings.TrimSpace(raw)
	if strings.EqualFold(provider, "kick") && s == "" {
		return "rtmps://fa723fc1b171.global-contribute.live-video.net/app"
	}
	if s == "" {
		return s
	}
	u, err := url.Parse(s)
	if err == nil && (u.Scheme == "rtmp" || u.Scheme == "rtmps") {
		u.Path = strings.TrimRight(u.Path, "/")
		if strings.EqualFold(provider, "kick") && u.Path == "" {
			u.Path = "/app"
		}
		return strings.TrimRight(u.String(), "/")
	}
	return strings.TrimRight(s, "/")
}

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func (a *App) publicDestination(d Destination) PublicDestination {
	return PublicDestination{ID: d.ID, Name: d.Name, Provider: d.Provider, Enabled: d.Enabled, AutoStart: d.AutoStart, Server: d.Server, KeyConfigured: d.StreamKey != "", SourceID: normalizedSourceID(d.SourceID), VideoTrack: d.VideoTrack, AudioTrack: d.AudioTrack, AutoAdaptAudio: d.AutoAdaptAudio}
}

func (a *App) snapshotStatus() AppStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := AppStatus{Source: a.source, Tracks: append([]Track(nil), a.tracks...), Sources: a.publicSourcesLocked(), Presets: presetCatalogSnapshot(), Resources: a.resources, AuthEnabled: a.auth != nil && a.auth.Enabled(), AuthUsername: a.settings.AuthUsername}
	for _, d := range a.config.Destinations {
		rt := *a.getRuntimeLocked(d.ID)
		if !rt.Running && !rt.NextRetry.IsZero() {
			remaining := time.Until(rt.NextRetry)
			if remaining > 0 {
				rt.RetryIn = int((remaining + time.Second - 1) / time.Second)
			} else {
				rt.RetryIn = 0
			}
		}
		_, _, sourceTracks, _ := a.sourceSelectionLocked(d.SourceID)
		video := findTrackByOrder(sourceTracks, "video", d.VideoTrack)
		audio := findTrackByOrder(sourceTracks, "audio", d.AudioTrack)
		compat := compatibilityForSetting(presetByID(d.Provider), video, audio, d.AutoAdaptAudio)
		rawLogs := rt.LastLogs
		if ps := a.processes[d.ID]; ps != nil && ps.log != nil {
			rawLogs = ps.log.String()
		}
		_, logSummary := structuredLogs(rawLogs)
		out.Destinations = append(out.Destinations, DestinationStatus{
			Config:        a.publicDestination(d),
			Runtime:       rt,
			Compatibility: compat,
			Logs:          logSummary,
			Preview:       a.previewStateLocked(d.ID),
			Resources:     a.resourceByDest[d.ID],
		})
	}
	return out
}

func (a *App) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, a.snapshotStatus())
	})
	mux.HandleFunc("/api/destinations", a.destinationsHandler)
	mux.HandleFunc("/api/destinations/", a.destinationHandler)
	mux.HandleFunc("/api/compatibility", a.compatibilityHandler)
	mux.HandleFunc("/api/previews/", a.previewFileHandler)
}

func (a *App) destinationsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		out := make([]PublicDestination, 0, len(a.config.Destinations))
		for _, d := range a.config.Destinations {
			out = append(out, a.publicDestination(d))
		}
		a.mu.Unlock()
		writeJSON(w, http.StatusOK, out)
	case http.MethodPost:
		var d Destination
		if err := decodeJSON(r, &d); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		d.SourceID = normalizedSourceID(d.SourceID)

		if d.Provider == "" {
			d.Provider = "custom"
		}
		d.Server = normalizeServer(d.Provider, d.Server)
		if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Server) == "" {
			writeError(w, http.StatusBadRequest, "name and server are required")
			return
		}
		d.ID = slugID(d.Name)
		a.mu.Lock()
		base := d.ID
		n := 2
		for {
			_, ex := a.findDestLocked(d.ID)
			if ex == nil {
				break
			}
			d.ID = fmt.Sprintf("%s-%d", base, n)
			n++
		}
		a.config.Destinations = append(a.config.Destinations, d)
		a.runtime[d.ID] = &RuntimeState{}
		err := a.saveConfigLocked()
		a.mu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, a.publicDestination(d))
	default:
		methodNotAllowed(w)
	}
}

func (a *App) destinationHandler(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/destinations/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 {
		switch parts[1] {
		case "start":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}
			if err := a.startDestination(id, true); err != nil {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		case "stop":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}
			if err := a.stopDestination(id, true); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		case "toggle-enabled":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}

			a.mu.Lock()
			_, d := a.findDestLocked(id)
			if d == nil {
				a.mu.Unlock()
				http.NotFound(w, r)
				return
			}

			oldEnabled := d.Enabled
			d.Enabled = !d.Enabled

			if err := a.saveConfigLocked(); err != nil {
				d.Enabled = oldEnabled
				a.mu.Unlock()
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			enabled := d.Enabled
			autoStart := d.AutoStart

			if enabled {
				rt := a.getRuntimeLocked(id)
				rt.ManualStopped = false
				rt.RetryCount = 0
				rt.NextRetry = time.Time{}
				rt.RetryIn = 0
			}

			out := a.publicDestination(*d)
			a.mu.Unlock()

			if !enabled {
				_ = a.stopDestination(id, false)
			} else if autoStart {
				a.ensureAutoStarts()
			}

			writeJSON(w, http.StatusOK, out)
			return

		case "toggle-auto-start":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}

			a.mu.Lock()
			_, d := a.findDestLocked(id)
			if d == nil {
				a.mu.Unlock()
				http.NotFound(w, r)
				return
			}

			oldAutoStart := d.AutoStart
			d.AutoStart = !d.AutoStart

			if err := a.saveConfigLocked(); err != nil {
				d.AutoStart = oldAutoStart
				a.mu.Unlock()
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			autoStart := d.AutoStart
			enabled := d.Enabled

			if autoStart {
				rt := a.getRuntimeLocked(id)
				rt.ManualStopped = false
				rt.RetryCount = 0
				rt.NextRetry = time.Time{}
				rt.RetryIn = 0
			}

			out := a.publicDestination(*d)
			a.mu.Unlock()

			// Passer en MANUEL ne coupe pas un live deja en cours.
			// Passer en AUTO autorise un demarrage immediat si la destination est activee.
			if autoStart && enabled {
				a.ensureAutoStarts()
			}

			writeJSON(w, http.StatusOK, out)
			return

		case "logs":
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			a.mu.Lock()
			ps := a.processes[id]
			rt := a.getRuntimeLocked(id)
			s := rt.LastLogs
			if ps != nil && ps.log != nil {
				s = ps.log.String()
			}
			a.mu.Unlock()
			entries, summary := structuredLogs(s)
			writeJSON(w, http.StatusOK, map[string]any{"logs": s, "entries": entries, "summary": summary})
			return
		case "preview":
			switch r.Method {
			case http.MethodPost:
				a.mu.Lock()
				_, d := a.findDestLocked(id)
				if d == nil {
					a.mu.Unlock()
					http.NotFound(w, r)
					return
				}
				fallbackV, fallbackA := d.VideoTrack, d.AudioTrack
				a.mu.Unlock()
				video, audio, err := parsePreviewOrder(r, fallbackV, fallbackA)
				if err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				state, err := a.startPreview(id, video, audio)
				if err != nil {
					writeError(w, http.StatusConflict, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, state)
				return
			case http.MethodDelete:
				if err := a.stopPreview(id); err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
				return
			case http.MethodGet:
				a.mu.Lock()
				state := a.previewStateLocked(id)
				a.mu.Unlock()
				writeJSON(w, http.StatusOK, state)
				return
			default:
				methodNotAllowed(w)
				return
			}
		}
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.mu.Lock()
		_, d := a.findDestLocked(id)
		if d == nil {
			a.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		out := a.publicDestination(*d)
		a.mu.Unlock()

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, out)

	case http.MethodPut:
		var in Destination
		if err := decodeJSON(r, &in); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.mu.Lock()
		idx, old := a.findDestLocked(id)
		if old == nil {
			a.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		if strings.TrimSpace(in.Name) == "" {
			in.Name = old.Name
		}
		if strings.TrimSpace(in.Server) == "" {
			in.Server = old.Server
		}
		if in.StreamKey == "" {
			in.StreamKey = old.StreamKey
		}
		if strings.TrimSpace(in.SourceID) == "" {
			in.SourceID = old.SourceID
		}
		in.SourceID = normalizedSourceID(in.SourceID)
		if in.Provider == "" {
			in.Provider = old.Provider
		}
		in.Server = normalizeServer(in.Provider, in.Server)
		in.ID = id
		wasRunning := a.getRuntimeLocked(id).Running
		a.config.Destinations[idx] = in
		err := a.saveConfigLocked()
		a.mu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if wasRunning {
			_ = a.stopDestination(id, false)
			if in.Enabled {
				go a.restartWhenStopped(id)
			}
		}
		writeJSON(w, http.StatusOK, a.publicDestination(in))
	case http.MethodDelete:
		_ = a.stopPreview(id)
		_ = a.stopDestination(id, true)
		a.mu.Lock()
		idx, old := a.findDestLocked(id)
		if old == nil {
			a.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		a.config.Destinations = append(a.config.Destinations[:idx], a.config.Destinations[idx+1:]...)
		err := a.saveConfigLocked()
		a.mu.Unlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		go func() {
			a.waitStopped(id, 3*time.Second)
			a.mu.Lock()
			delete(a.runtime, id)
			a.mu.Unlock()
		}()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if !strings.HasPrefix(r.URL.Path, "/api/status") {
			log.Printf("%s %s", r.Method, r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}
