package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type previewProc struct {
	cmd        *exec.Cmd
	log        *ringLog
	dir        string
	videoTrack int
	audioTrack int
	startedAt  time.Time
	ready      bool
	stopping   bool
	done       chan struct{}
	exitErr    error
}

type PreviewState struct {
	Running    bool             `json:"running"`
	Ready      bool             `json:"ready"`
	VideoTrack int              `json:"video_track,omitempty"`
	AudioTrack int              `json:"audio_track,omitempty"`
	StartedAt  time.Time        `json:"started_at,omitempty"`
	Playlist   string           `json:"playlist,omitempty"`
	LastError  string           `json:"last_error,omitempty"`
	Resources  ProcessResources `json:"resources"`
}

type previewRequest struct {
	VideoTrack int `json:"video_track"`
	AudioTrack int `json:"audio_track"`
}

func (a *App) previewStateLocked(id string) PreviewState {
	ps := a.previews[id]
	if ps == nil {
		return PreviewState{LastError: a.previewErrors[id]}
	}
	return PreviewState{
		Running:    true,
		Ready:      ps.ready,
		VideoTrack: ps.videoTrack,
		AudioTrack: ps.audioTrack,
		StartedAt:  ps.startedAt,
		Playlist:   "/api/previews/" + id + "/index.m3u8",
		Resources:  a.resourceByPreview[id],
	}
}

func (a *App) startPreview(id string, videoOrder, audioOrder int) (PreviewState, error) {
	a.mu.Lock()
	if !a.source.Online || a.sourcePath == "" {
		a.mu.Unlock()
		return PreviewState{}, fmt.Errorf("source is offline")
	}
	_, d := a.findDestLocked(id)
	if d == nil {
		a.mu.Unlock()
		return PreviewState{}, fmt.Errorf("destination not found")
	}
	video := findTrackByOrder(a.tracks, "video", videoOrder)
	audio := findTrackByOrder(a.tracks, "audio", audioOrder)
	if video == nil || audio == nil {
		a.mu.Unlock()
		return PreviewState{}, fmt.Errorf("selected preview track is not available")
	}
	if !strings.EqualFold(audio.CodecName, "aac") {
		a.mu.Unlock()
		return PreviewState{}, fmt.Errorf("preview HLS in copy mode currently requires AAC audio")
	}
	if a.previewErrors == nil {
		a.previewErrors = make(map[string]string)
	}
	delete(a.previewErrors, id)
	old := a.previews[id]
	if old != nil && old.videoTrack == videoOrder && old.audioTrack == audioOrder && old.ready {
		state := a.previewStateLocked(id)
		a.mu.Unlock()
		return state, nil
	}
	destinationName := d.Name
	sourcePath := a.sourcePath
	src := a.sourceURL(sourcePath)
	a.mu.Unlock()

	if old != nil {
		_ = a.stopPreview(id)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			a.mu.Lock()
			still := a.previews[id] != nil
			a.mu.Unlock()
			if !still {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	dir := filepath.Join(a.settings.DataDir, "previews", id)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return PreviewState{}, err
	}
	playlist := filepath.Join(dir, "index.m3u8")
	segments := filepath.Join(dir, "segment%06d.ts")
	args := []string{
		"-nostdin", "-hide_banner", "-loglevel", "warning",
		"-rtmp_enhanced_codecs", enhancedCodecs,
		"-i", src,
		"-map", fmt.Sprintf("0:v:%d", videoOrder),
		"-map", fmt.Sprintf("0:a:%d", audioOrder),
		"-c:v", "copy", "-c:a", "copy",
		"-avoid_negative_ts", "make_zero",
		"-f", "hls",
		"-hls_segment_type", "mpegts",
		"-hls_time", "2",
		"-hls_list_size", "6",
		"-hls_delete_threshold", "2",
		"-hls_flags", "delete_segments+omit_endlist+program_date_time",
		"-hls_segment_filename", segments,
		playlist,
	}
	cmd := exec.Command(a.settings.FFmpegBin, args...)
	logger := newRingLog(120, sourcePath)
	cmd.Stdout = io.Discard
	cmd.Stderr = logger
	if err := cmd.Start(); err != nil {
		a.recordError("preview", id, destinationName, shortErr(err), 0)
		return PreviewState{}, err
	}
	ps := &previewProc{
		cmd:        cmd,
		log:        logger,
		dir:        dir,
		videoTrack: videoOrder,
		audioTrack: audioOrder,
		startedAt:  time.Now(),
		done:       make(chan struct{}),
	}
	a.mu.Lock()
	a.previews[id] = ps
	a.mu.Unlock()

	go func() {
		err := cmd.Wait()
		var historyMessage string
		a.mu.Lock()
		ps.exitErr = err
		if a.previews[id] == ps {
			delete(a.previews, id)
		}
		wasStopping := ps.stopping
		if err != nil && !wasStopping {
			logText := logger.Tail(30)
			message := summarizeFFmpegError(err, logText)
			historyMessage = message
			if a.previewErrors == nil {
				a.previewErrors = make(map[string]string)
			}
			if strings.TrimSpace(a.previewErrors[id]) == "" {
				a.previewErrors[id] = message
			}
		}
		close(ps.done)
		a.mu.Unlock()
		if historyMessage != "" {
			a.recordError("preview", id, destinationName, historyMessage, 0)
		}
		if err != nil && !wasStopping {
			logText := logger.Tail(30)
			if logText != "" {
				log.Printf("preview %s stopped: %s", id, summarizeFFmpegError(err, logText))
			}
		}
	}()

	// In copy mode the HLS muxer can only cut on a suitable keyframe. Do not tell
	// the browser that the preview is ready until a real playlist and its first
	// media segment are present. This removes the former arbitrary 800 ms race.
	readyDeadline := time.NewTimer(12 * time.Second)
	defer readyDeadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if previewPlaylistReady(dir, playlist) {
			a.mu.Lock()
			if a.previews[id] == ps {
				ps.ready = true
				delete(a.previewErrors, id)
				state := a.previewStateLocked(id)
				a.mu.Unlock()
				return state, nil
			}
			a.mu.Unlock()
			return PreviewState{}, fmt.Errorf("preview stopped before becoming ready")
		}

		select {
		case <-ps.done:
			logText := logger.Tail(30)
			message := summarizeFFmpegError(ps.exitErr, logText)
			if strings.TrimSpace(message) == "" {
				message = "FFmpeg preview stopped before the first HLS segment was created"
			}
			return PreviewState{LastError: message}, fmt.Errorf("%s", message)
		case <-readyDeadline.C:
			_ = a.stopPreview(id)
			message := "aperçu HLS non prêt après 12 s"
			if logText := strings.TrimSpace(logger.Tail(12)); logText != "" {
				message += ": " + summarizeFFmpegError(fmt.Errorf("timeout"), logText)
			}
			a.mu.Lock()
			if a.previewErrors == nil {
				a.previewErrors = make(map[string]string)
			}
			a.previewErrors[id] = message
			a.mu.Unlock()
			a.recordError("preview", id, destinationName, message, 0)
			return PreviewState{LastError: message}, fmt.Errorf("%s", message)
		case <-ticker.C:
		}
	}
}

func previewPlaylistReady(dir, playlist string) bool {
	b, err := os.ReadFile(playlist)
	if err != nil || len(b) == 0 {
		return false
	}
	text := string(b)
	if !strings.Contains(text, "#EXTM3U") || !strings.Contains(text, "#EXTINF") {
		return false
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name := strings.SplitN(line, "?", 2)[0]
		name = filepath.Base(name)
		if !strings.HasSuffix(strings.ToLower(name), ".ts") {
			continue
		}
		st, err := os.Stat(filepath.Join(dir, name))
		return err == nil && st.Size() > 0
	}
	return false
}

func (a *App) stopPreview(id string) error {
	a.mu.Lock()
	ps := a.previews[id]
	if ps != nil {
		ps.stopping = true
	}
	a.mu.Unlock()
	if ps == nil || ps.cmd == nil || ps.cmd.Process == nil {
		return nil
	}
	if err := ps.cmd.Process.Signal(os.Interrupt); err != nil {
		_ = ps.cmd.Process.Kill()
		return err
	}
	return nil
}

func (a *App) stopAllPreviews() {
	a.mu.Lock()
	ids := make([]string, 0, len(a.previews))
	for id := range a.previews {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		_ = a.stopPreview(id)
	}
}

func (a *App) previewFileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/previews/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || filepath.Base(parts[1]) != parts[1] {
		http.NotFound(w, r)
		return
	}
	id, name := parts[0], parts[1]
	if !(name == "index.m3u8" || strings.HasSuffix(name, ".ts")) {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	ps := a.previews[id]
	a.mu.Unlock()
	if ps == nil {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(ps.dir, name)
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if name == "index.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeContent(w, r, name, st.ModTime(), f)
}

func (a *App) compatibilityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var req struct {
		Provider       string `json:"provider"`
		VideoTrack     int    `json:"video_track"`
		AudioTrack     int    `json:"audio_track"`
		AutoAdaptAudio bool   `json:"auto_adapt_audio"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.mu.Lock()
	video := findTrackByOrder(a.tracks, "video", req.VideoTrack)
	audio := findTrackByOrder(a.tracks, "audio", req.AudioTrack)
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, compatibilityForSetting(presetByID(req.Provider), video, audio, req.AutoAdaptAudio))
}

func findTrackByOrder(tracks []Track, kind string, order int) *Track {
	for i := range tracks {
		t := &tracks[i]
		if kind == "video" && t.CodecType == "video" && t.VideoOrder == order {
			copy := *t
			return &copy
		}
		if kind == "audio" && t.CodecType == "audio" && t.AudioOrder == order {
			copy := *t
			return &copy
		}
	}
	return nil
}

func parsePreviewOrder(r *http.Request, fallbackV, fallbackA int) (int, int, error) {
	req := previewRequest{VideoTrack: fallbackV, AudioTrack: fallbackA}
	if r.Body == nil {
		return fallbackV, fallbackA, nil
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil && err != io.EOF {
		return 0, 0, err
	}
	if v := r.URL.Query().Get("video_track"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			req.VideoTrack = n
		}
	}
	if v := r.URL.Query().Get("audio_track"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			req.AudioTrack = n
		}
	}
	return req.VideoTrack, req.AudioTrack, nil
}
