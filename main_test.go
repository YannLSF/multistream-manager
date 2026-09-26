package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugID(t *testing.T) {
	if got := slugID("Kick Principal"); got != "kick-principal" {
		t.Fatalf("unexpected slug: %q", got)
	}
}

func TestPrettyFPS(t *testing.T) {
	if got := prettyFPS("60/1"); got != "60" {
		t.Fatalf("unexpected fps: %q", got)
	}
	if got := prettyFPS("30000/1001"); got != "29.97" {
		t.Fatalf("unexpected fps: %q", got)
	}
}

func TestPublicDestinationHidesKey(t *testing.T) {
	a := &App{}
	p := a.publicDestination(Destination{Name: "Kick", StreamKey: "secret"})
	if !p.KeyConfigured {
		t.Fatal("expected key configured")
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatal("stream key leaked from public destination")
	}
}

func TestTrackZeroOrdersAreSerialized(t *testing.T) {
	b, err := json.Marshal(Track{CodecType: "video", VideoOrder: 0, AudioOrder: 0})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"video_order":0`) || !strings.Contains(s, `"audio_order":0`) {
		t.Fatalf("zero track orders must be serialized, got %s", s)
	}
}

func TestNormalizeKickServer(t *testing.T) {
	if got := normalizeServer("kick", "rtmps://fa723fc1b171.global-contribute.live-video.net"); got != "rtmps://fa723fc1b171.global-contribute.live-video.net/app" {
		t.Fatalf("unexpected kick server: %q", got)
	}
	if got := normalizeServer("kick", "rtmps://fa723fc1b171.global-contribute.live-video.net/app/"); got != "rtmps://fa723fc1b171.global-contribute.live-video.net/app" {
		t.Fatalf("unexpected normalized server: %q", got)
	}
}

func TestRingLogRedactsSecrets(t *testing.T) {
	r := newRingLog(10, "secret-key", "app/private-path")
	_, _ = r.Write([]byte("output secret-key input app/private-path\n"))
	got := r.String()
	if strings.Contains(got, "secret-key") || strings.Contains(got, "app/private-path") {
		t.Fatalf("secret leaked in logs: %q", got)
	}
}

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		count int
		want  time.Duration
	}{{1, 10 * time.Second}, {2, 20 * time.Second}, {3, 40 * time.Second}, {4, 60 * time.Second}, {8, 60 * time.Second}}
	for _, tc := range cases {
		if got := retryDelay(tc.count); got != tc.want {
			t.Fatalf("retryDelay(%d)=%s want %s", tc.count, got, tc.want)
		}
	}
}

func TestSummarizeFFmpegErrorUsesLog(t *testing.T) {
	got := summarizeFFmpegError(assertErr("exit status 251"), "noise\nError opening output files: Input/output error\n")
	if !strings.Contains(got, "Error opening output files") {
		t.Fatalf("unexpected summary: %q", got)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestClassifyNegativeCTSAsWarning(t *testing.T) {
	line := "[in#0/flv @ 0x123] Negative cts, previous timestamps might be wrong. Invalid timestamps"
	if got := classifyLogLine(line); got != "warning" {
		t.Fatalf("classifyLogLine()=%q want warning", got)
	}
}

func TestCompatibilityTrovoDirect(t *testing.T) {
	video := &Track{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080, FrameRate: "60/1", BitRate: 6_000_000, Profile: "High"}
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 160_000}
	got := compatibilityFor(presetByID("trovo"), video, audio)
	if got.Status != "direct" {
		t.Fatalf("unexpected status %q: %+v", got.Status, got.Issues)
	}
}

func TestCompatibilityFacebookAdaptsAudio(t *testing.T) {
	video := &Track{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080, FrameRate: "60/1", BitRate: 6_000_000, Profile: "Main"}
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 160_000}
	got := compatibilityFor(presetByID("facebook"), video, audio)
	if got.Status != "adapt_audio" {
		t.Fatalf("unexpected status %q: %+v", got.Status, got.Issues)
	}
}

func TestCompatibilityOnlyFansBFramesRequiresVideoTranscode(t *testing.T) {
	video := &Track{CodecType: "video", CodecName: "h264", Width: 1280, Height: 720, FrameRate: "60/1", BitRate: 2_500_000, Profile: "Main", HasBFrames: 2}
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 128_000}
	got := compatibilityFor(presetByID("onlyfans"), video, audio)
	if got.Status != "transcode_video" {
		t.Fatalf("unexpected status %q: %+v", got.Status, got.Issues)
	}
}

func TestCompatibilityLinkedIn60FPSRequiresVideoTranscode(t *testing.T) {
	video := &Track{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080, FrameRate: "60/1", BitRate: 5_000_000, Profile: "Baseline"}
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 128_000}
	got := compatibilityFor(presetByID("linkedin"), video, audio)
	if got.Status != "transcode_video" {
		t.Fatalf("unexpected status %q: %+v", got.Status, got.Issues)
	}
}

func TestPresetCatalogKeepsLegacyProviders(t *testing.T) {
	for _, id := range []string{"kick", "trovo", "custom"} {
		if got := presetByID(id); got.ID != id {
			t.Fatalf("preset %q resolved to %q", id, got.ID)
		}
	}
}

func TestPreviewPlaylistReady(t *testing.T) {
	dir := t.TempDir()
	playlist := filepath.Join(dir, "index.m3u8")
	if previewPlaylistReady(dir, playlist) {
		t.Fatal("missing playlist must not be ready")
	}
	if err := os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:2.0,\nsegment000001.ts\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if previewPlaylistReady(dir, playlist) {
		t.Fatal("playlist without media segment must not be ready")
	}
	if err := os.WriteFile(filepath.Join(dir, "segment000001.ts"), []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !previewPlaylistReady(dir, playlist) {
		t.Fatal("playlist with first media segment should be ready")
	}
}

func TestStartPreviewWaitsForReadyPlaylist(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "fake-ffmpeg")
	body := `#!/bin/sh
segfmt=""
playlist=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-hls_segment_filename" ]; then segfmt="$arg"; fi
  prev="$arg"
  playlist="$arg"
done
sleep 0.25
seg=$(printf "$segfmt" 0)
printf 'media' > "$seg"
printf '#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:2.0,\n%s\n' "$(basename "$seg")" > "$playlist"
trap 'exit 0' INT TERM
while :; do sleep 0.1; done
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	a := &App{
		settings:   Settings{DataDir: dataDir, FFmpegBin: script, MTXRTMPBase: "rtmp://127.0.0.1:1938"},
		config:     StoredConfig{Destinations: []Destination{{ID: "kick", Name: "Kick", Provider: "kick", VideoTrack: 0, AudioTrack: 0}}},
		source:     SourceState{Online: true},
		sourcePath: "app/test",
		tracks: []Track{
			{CodecType: "video", CodecName: "h264", VideoOrder: 0},
			{CodecType: "audio", CodecName: "aac", AudioOrder: 0},
		},
		previews:      make(map[string]*previewProc),
		previewErrors: make(map[string]string),
	}

	started := time.Now()
	state, err := a.startPreview("kick", 0, 0)
	if err != nil {
		t.Fatalf("startPreview failed: %v", err)
	}
	if !state.Running || !state.Ready {
		t.Fatalf("preview should be running and ready: %+v", state)
	}
	if time.Since(started) < 200*time.Millisecond {
		t.Fatal("startPreview returned before fake first HLS segment existed")
	}
	if err := a.stopPreview("kick"); err != nil {
		t.Fatal(err)
	}
}

func TestAudioPlanFacebookAutomatic128(t *testing.T) {
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 160_000}
	plan := audioPlanFor(presetByID("facebook"), audio)
	if plan.Mode != "transcode" || !plan.Automatic {
		t.Fatalf("unexpected audio plan: %+v", plan)
	}
	if plan.Codec != "aac" || plan.BitrateKbps != 128 || plan.SampleRate != 48000 || plan.Channels != 2 {
		t.Fatalf("unexpected Facebook target: %+v", plan)
	}
	args, err := audioFFmpegArgs(plan, audio)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if joined != "-c:a aac -b:a 128k" {
		t.Fatalf("unexpected Facebook audio args: %s", joined)
	}
}

func TestAudioPlanRUTUBEAutomaticResample(t *testing.T) {
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 160_000}
	plan := audioPlanFor(presetByID("rutube"), audio)
	if plan.Mode != "transcode" || !plan.Automatic {
		t.Fatalf("unexpected audio plan: %+v", plan)
	}
	if plan.BitrateKbps != 128 || plan.SampleRate != 44100 {
		t.Fatalf("unexpected RUTUBE target: %+v", plan)
	}
	args, err := audioFFmpegArgs(plan, audio)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if joined != "-c:a aac -b:a 128k -ar 44100" {
		t.Fatalf("unexpected RUTUBE audio args: %s", joined)
	}
}

func TestAudioPlanDoesNotReencodeCompatibleLowerBitrate(t *testing.T) {
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 96_000}
	plan := audioPlanFor(presetByID("facebook"), audio)
	if plan.Mode != "copy" {
		t.Fatalf("96 kb/s AAC should stay in copy mode: %+v", plan)
	}
}

func TestBuildDestinationArgsFacebookCopiesVideoAndAdaptsAudio(t *testing.T) {
	d := Destination{Provider: "facebook", VideoTrack: 1, AudioTrack: 0, AutoAdaptAudio: true}
	video := &Track{CodecType: "video", CodecName: "h264", VideoOrder: 1}
	audio := &Track{CodecType: "audio", CodecName: "aac", AudioOrder: 0, SampleRate: 48000, Channels: 2, BitRate: 160_000}
	args, plan, err := buildDestinationFFmpegArgs("rtmp://source/app/live", "rtmps://dest/app/key", d, presetByID("facebook"), video, audio)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "transcode" {
		t.Fatalf("expected audio transcode: %+v", plan)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-map 0:v:1", "-map 0:a:0", "-c:v copy", "-c:a aac", "-b:a 128k"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in args: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-c:v h264") || strings.Contains(joined, "-c:v libx264") {
		t.Fatalf("video must not be transcoded: %s", joined)
	}
}

func TestBuildDestinationArgsTrovoKeepsPureCopy(t *testing.T) {
	d := Destination{Provider: "trovo", VideoTrack: 0, AudioTrack: 0}
	video := &Track{CodecType: "video", CodecName: "h264", VideoOrder: 0}
	audio := &Track{CodecType: "audio", CodecName: "aac", AudioOrder: 0, SampleRate: 48000, Channels: 2, BitRate: 160_000}
	args, plan, err := buildDestinationFFmpegArgs("rtmp://source/app/live", "rtmp://dest/live/key", d, presetByID("trovo"), video, audio)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "copy" {
		t.Fatalf("unexpected Trovo plan: %+v", plan)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v copy -c:a copy") {
		t.Fatalf("Trovo should remain pure copy: %s", joined)
	}
}

func TestAudioPlanUnsupportedCodecDoesNotSilentlyGuess(t *testing.T) {
	p := Preset{ID: "test", Name: "Test", Constraints: PresetConstraints{AudioCodecs: []string{"opus"}, MaxAudioBitrateKbps: 128}}
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 160_000}
	plan := audioPlanFor(p, audio)
	if plan.Mode != "unsupported" || plan.Automatic {
		t.Fatalf("unsupported target codec must not be silently encoded: %+v", plan)
	}
}

func TestAudioBitrateToleranceAvoidsNeedlessTrovoTranscode(t *testing.T) {
	audio := &Track{CodecType: "audio", CodecName: "aac", SampleRate: 48000, Channels: 2, BitRate: 164_000}
	plan := audioPlanFor(presetByID("trovo"), audio)
	if plan.Mode != "copy" {
		t.Fatalf("~164 kb/s should be treated as nominal 160 kb/s for Trovo: %+v", plan)
	}
	video := &Track{CodecType: "video", CodecName: "h264", Width: 1920, Height: 1080, FrameRate: "60/1", BitRate: 6_000_000, Profile: "High"}
	compat := compatibilityFor(presetByID("trovo"), video, audio)
	if compat.Status != "direct" {
		t.Fatalf("Trovo should remain direct with bitrate tolerance: %+v", compat)
	}
}

func TestDestinationAudioAdaptationStaysIndependent(t *testing.T) {
	root := t.TempDir()
	captureDir := filepath.Join(root, "capture")
	if err := os.MkdirAll(captureDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE_DIR", captureDir)
	script := filepath.Join(root, "fake-ffmpeg")
	body := `#!/bin/sh
out=""
for arg in "$@"; do out="$arg"; done
case "$out" in
  *facebook*) file="$CAPTURE_DIR/facebook.args" ;;
  *kick*) file="$CAPTURE_DIR/kick.args" ;;
  *) file="$CAPTURE_DIR/other.args" ;;
esac
printf '%s\n' "$@" > "$file"
trap 'exit 0' INT TERM
while :; do sleep 0.05; done
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}

	a := &App{
		settings: Settings{FFmpegBin: script, MTXRTMPBase: "rtmp://127.0.0.1:1938"},
		config: StoredConfig{Destinations: []Destination{
			{ID: "kick", Name: "Kick", Provider: "kick", Enabled: true, Server: "rtmp://dest/kick", StreamKey: "key", VideoTrack: 0, AudioTrack: 0},
			{ID: "facebook", Name: "Facebook", Provider: "facebook", Enabled: true, Server: "rtmp://dest/facebook", StreamKey: "key", VideoTrack: 0, AudioTrack: 0, AutoAdaptAudio: true},
		}},
		source:     SourceState{Online: true},
		sourcePath: "app/test",
		tracks: []Track{
			{CodecType: "video", CodecName: "h264", VideoOrder: 0},
			{CodecType: "audio", CodecName: "aac", AudioOrder: 0, SampleRate: 48000, Channels: 2, BitRate: 164_000},
		},
		runtime:       make(map[string]*RuntimeState),
		processes:     make(map[string]*procState),
		previews:      make(map[string]*previewProc),
		previewErrors: make(map[string]string),
	}
	defer a.stopAll(false)

	if err := a.startDestination("kick", true); err != nil {
		t.Fatal(err)
	}
	if err := a.startDestination("facebook", true); err != nil {
		t.Fatal(err)
	}

	waitFile := func(path string) {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(path); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", path)
	}
	kickArgsPath := filepath.Join(captureDir, "kick.args")
	facebookArgsPath := filepath.Join(captureDir, "facebook.args")
	waitFile(kickArgsPath)
	waitFile(facebookArgsPath)

	kickArgs, err := os.ReadFile(kickArgsPath)
	if err != nil {
		t.Fatal(err)
	}
	facebookArgs, err := os.ReadFile(facebookArgsPath)
	if err != nil {
		t.Fatal(err)
	}
	kickJoined := strings.Join(strings.Fields(string(kickArgs)), " ")
	facebookJoined := strings.Join(strings.Fields(string(facebookArgs)), " ")
	if !strings.Contains(kickJoined, "-c:v copy -c:a copy") {
		t.Fatalf("Kick should remain pure copy: %s", kickJoined)
	}
	if !strings.Contains(facebookJoined, "-c:v copy -c:a aac -b:a 128k") {
		t.Fatalf("Facebook should adapt only audio: %s", facebookJoined)
	}

	a.mu.Lock()
	kickPID := a.getRuntimeLocked("kick").PID
	facebookPID := a.getRuntimeLocked("facebook").PID
	a.mu.Unlock()
	if kickPID == 0 || facebookPID == 0 || kickPID == facebookPID {
		t.Fatalf("destinations must have separate FFmpeg processes: kick=%d facebook=%d", kickPID, facebookPID)
	}

	if err := a.stopDestination("facebook", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		fbRunning := a.getRuntimeLocked("facebook").Running
		kickRunning := a.getRuntimeLocked("kick").Running
		a.mu.Unlock()
		if !fbRunning {
			if !kickRunning {
				t.Fatal("stopping Facebook also stopped Kick")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("Facebook did not stop within timeout")
}

func TestAudioAdaptationDisabledByDefault(t *testing.T) {
	var d Destination
	if err := json.Unmarshal([]byte(`{"id":"steam","name":"Steam","provider":"steam","audio_track":0,"video_track":0}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.AutoAdaptAudio {
		t.Fatal("legacy destination must keep automatic audio adaptation disabled by default")
	}
}

func TestBuildDestinationArgsFacebookPreservesAudioWhenAdaptationDisabled(t *testing.T) {
	d := Destination{Provider: "facebook", VideoTrack: 0, AudioTrack: 0}
	video := &Track{CodecType: "video", CodecName: "h264", VideoOrder: 0}
	audio := &Track{CodecType: "audio", CodecName: "aac", AudioOrder: 0, SampleRate: 48000, Channels: 2, BitRate: 160_000}
	args, plan, err := buildDestinationFFmpegArgs("rtmp://source/app/live", "rtmps://dest/app/key", d, presetByID("facebook"), video, audio)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Mode != "copy" || plan.Automatic || len(plan.Reasons) == 0 {
		t.Fatalf("expected source audio to be preserved with a visible constraint warning: %+v", plan)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-c:v copy -c:a copy") {
		t.Fatalf("audio must remain copy when adaptation is disabled: %s", joined)
	}
	compat := compatibilityForSetting(presetByID("facebook"), video, audio, false)
	if compat.Status != "warning" || !strings.Contains(strings.ToLower(compat.Label), "désactivée") {
		t.Fatalf("unexpected compatibility result: %+v", compat)
	}
}

func TestClassifySourceDemuxCloseAsInfo(t *testing.T) {
	line := "[in#0/flv @ 0x123] Error during demuxing: Input/output error"
	if got := classifyLogLine(line); got != "info" {
		t.Fatalf("classifyLogLine()=%q want info", got)
	}
	if !isSourceDemuxInputClosed(line) {
		t.Fatal("source demux close marker was not detected")
	}
}

func TestSourceDemuxCloseDoesNotBecomeDestinationErrorWhenSourceGoesOffline(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-ffmpeg")
	body := `#!/bin/sh
sleep 0.05
printf '%s\n' '[in#0/flv @ 0x123] Error during demuxing: Input/output error' >&2
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	a := &App{
		settings: Settings{FFmpegBin: script, MTXRTMPBase: "rtmp://127.0.0.1:1938", PollInterval: 20 * time.Millisecond},
		config:   StoredConfig{Destinations: []Destination{{ID: "kick", Name: "Kick", Provider: "kick", Enabled: true, Server: "rtmp://dest/kick", StreamKey: "key", VideoTrack: 0, AudioTrack: 0}}},
		source:   SourceState{Online: true}, sourcePath: "app/test",
		tracks:  []Track{{CodecType: "video", CodecName: "h264", VideoOrder: 0}, {CodecType: "audio", CodecName: "aac", AudioOrder: 0, SampleRate: 48000, Channels: 2, BitRate: 160_000}},
		runtime: make(map[string]*RuntimeState), processes: make(map[string]*procState), previews: make(map[string]*previewProc), previewErrors: make(map[string]string),
	}
	if err := a.startDestination("kick", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	a.mu.Lock()
	a.source.Online = false
	a.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		rt := *a.getRuntimeLocked("kick")
		a.mu.Unlock()
		if !rt.Running {
			if rt.LastError != "" || rt.RetryCount != 0 || !rt.NextRetry.IsZero() {
				t.Fatalf("source shutdown must not become a destination error: %+v", rt)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("destination did not settle after source shutdown")
}

func TestSourceDemuxCloseStillRetriesWhenSourceRemainsOnline(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "fake-ffmpeg")
	body := `#!/bin/sh
printf '%s\n' '[in#0/flv @ 0x123] Error during demuxing: Input/output error' >&2
exit 1
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	a := &App{
		settings: Settings{FFmpegBin: script, MTXRTMPBase: "rtmp://127.0.0.1:1938", PollInterval: 10 * time.Millisecond},
		config:   StoredConfig{Destinations: []Destination{{ID: "kick", Name: "Kick", Provider: "kick", Enabled: true, Server: "rtmp://dest/kick", StreamKey: "key", VideoTrack: 0, AudioTrack: 0}}},
		source:   SourceState{Online: true}, sourcePath: "app/test",
		tracks:  []Track{{CodecType: "video", CodecName: "h264", VideoOrder: 0}, {CodecType: "audio", CodecName: "aac", AudioOrder: 0, SampleRate: 48000, Channels: 2, BitRate: 160_000}},
		runtime: make(map[string]*RuntimeState), processes: make(map[string]*procState), previews: make(map[string]*previewProc), previewErrors: make(map[string]string),
	}
	if err := a.startDestination("kick", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		rt := *a.getRuntimeLocked("kick")
		a.mu.Unlock()
		if !rt.Running && rt.RetryCount > 0 {
			if rt.LastError == "" || rt.NextRetry.IsZero() {
				t.Fatalf("real demux failure while source remains online must retry: %+v", rt)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("online demux failure was not converted into a retry")
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$pbkdf2-sha256$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !verifyPasswordHash(hash, "correct horse battery staple") {
		t.Fatal("valid password rejected")
	}
	if verifyPasswordHash(hash, "wrong password") {
		t.Fatal("invalid password accepted")
	}
}

func TestAuthMiddlewareLoginSession(t *testing.T) {
	hash, err := hashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{AuthUsername: "admin", AuthPasswordHash: hash, AuthSessionTTL: time.Hour}
	auth, err := newAuthManager(settings)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{settings: settings, auth: auth}
	mux := http.NewServeMux()
	app.registerAuthAPI(mux)
	mux.HandleFunc("/private", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := app.authMiddleware(mux)

	r := httptest.NewRequest(http.MethodGet, "/private", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("anonymous page request=%d want 303", w.Code)
	}

	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"s3cret"}`))
	login.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, login)
	if lw.Code != http.StatusOK {
		t.Fatalf("login=%d body=%s", lw.Code, lw.Body.String())
	}
	cookies := lw.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login returned no session cookie")
	}

	r2 := httptest.NewRequest(http.MethodGet, "/private", nil)
	r2.AddCookie(cookies[0])
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("authenticated page request=%d", w2.Code)
	}
}

func TestPersistentRingLogRotatesAndRedacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "kick.log")
	r := newPersistentRingLog(20, path, 70, 2, "secret-key")
	for i := 0; i < 12; i++ {
		_, _ = r.Write([]byte("line secret-key payload payload\n"))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated backup: %v", err)
	}
	for _, f := range []string{path, path + ".1", path + ".2"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "secret-key") {
			t.Fatalf("secret leaked in %s", f)
		}
	}
}

func TestErrorHistoryPersistsAndLimits(t *testing.T) {
	dir := t.TempDir()
	a := &App{settings: Settings{DataDir: dir, ErrorHistoryLimit: 2}}
	a.recordError("forward", "a", "A", "first", 1)
	a.recordError("forward", "b", "B", "second", 2)
	a.recordError("preview", "c", "C", "third", 0)
	got := a.errorHistorySnapshot(10)
	if len(got) != 2 || got[0].Message != "third" || got[1].Message != "second" {
		t.Fatalf("unexpected bounded history: %+v", got)
	}
	b := &App{settings: Settings{DataDir: dir, ErrorHistoryLimit: 2}}
	if err := b.loadErrorHistory(); err != nil {
		t.Fatal(err)
	}
	if len(b.errorHistorySnapshot(10)) != 2 {
		t.Fatal("history was not persisted")
	}
}

func TestExternalPresetCatalogCanBeUpdated(t *testing.T) {
	original := presetCatalogSnapshot()
	defer func() { _ = setPresetCatalog(original) }()
	dir := t.TempDir()
	if err := loadPresetCatalog(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "presets.json")); err != nil {
		t.Fatal(err)
	}
	body := []byte(`[
	  {"id":"foo","name":"Foo Live","category":"Test","constraints":{"video_codecs":["h264"],"audio_codecs":["aac"],"orientation":"any"}},
	  {"id":"custom","name":"RTMP / RTMPS personnalisé","category":"Autre","constraints":{"orientation":"any"}}
	]`)
	if err := importPresetCatalog(dir, body); err != nil {
		t.Fatal(err)
	}
	if got := presetByID("foo"); got.Name != "Foo Live" {
		t.Fatalf("external preset not active: %+v", got)
	}
}

func TestReadOwnProcessResources(t *testing.T) {
	_, rss, err := readProcCounters(os.Getpid())
	if err != nil {
		t.Skipf("/proc unavailable: %v", err)
	}
	if rss == 0 {
		t.Fatal("expected non-zero RSS")
	}
}

func TestValidateImportedConfigSanitizesIDs(t *testing.T) {
	cfg := StoredConfig{Destinations: []Destination{{ID: "../Kick Main", Name: "Kick", Provider: "kick", Server: "rtmp://example/live"}}}
	if err := validateImportedConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.Destinations[0].ID, "/") || strings.Contains(cfg.Destinations[0].ID, "..") {
		t.Fatalf("unsafe imported id: %q", cfg.Destinations[0].ID)
	}
}

func TestReadPasswordFromReader(t *testing.T) {
	got, err := readPasswordFromReader(strings.NewReader("s3cret\n"))
	if err != nil {
		t.Fatal(err)
	}
	defer zeroBytes(got)
	if string(got) != "s3cret" {
		t.Fatalf("password=%q", string(got))
	}
}

func TestReadPasswordFromReaderRejectsMultipleLines(t *testing.T) {
	if _, err := readPasswordFromReader(strings.NewReader("first\nsecond\n")); err == nil {
		t.Fatal("expected multiline password input to be rejected")
	}
}

func TestHashPasswordBytesRoundTrip(t *testing.T) {
	password := []byte("byte-oriented-secret")
	hash, err := hashPasswordBytes(password)
	if err != nil {
		t.Fatal(err)
	}
	if !verifyPasswordHash(hash, "byte-oriented-secret") {
		t.Fatal("valid password rejected")
	}
}
