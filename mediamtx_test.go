package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestMediaMTXGeneratedConfig(t *testing.T) {
	settings := Settings{
		MTXAPI:      "http://127.0.0.1:9999",
		MTXRTMPBase: "rtmp://127.0.0.1:1938",
	}

	config, err := mediaMTXGeneratedConfig(settings)
	if err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"api: true",
		"apiAddress: 127.0.0.1:9999",
		"rtmp: true",
		"rtmpAddress: :1938",
		"rtsp: false",
		"hls: false",
		"webrtc: false",
		"srt: false",
		"moq: false",
		"paths:",
		"  all_others:",
	}

	for _, want := range expected {
		if !strings.Contains(config, want) {
			t.Fatalf("generated config does not contain %q:\n%s", want, config)
		}
	}

	if strings.Contains(config, "forward:") {
		t.Fatalf("portable MediaMTX configuration must not contain forwarding:\n%s", config)
	}
}

func TestMediaMTXAPIListenAddressLocalhost(t *testing.T) {
	got, err := mediaMTXAPIListenAddress("http://localhost:9999")
	if err != nil {
		t.Fatal(err)
	}

	if got != "127.0.0.1:9999" {
		t.Fatalf("address = %q", got)
	}
}

func TestMediaMTXAPIListenAddressRejectsRemote(t *testing.T) {
	_, err := mediaMTXAPIListenAddress("http://192.168.1.20:9999")
	if err == nil {
		t.Fatal("remote API address should be rejected for managed MediaMTX")
	}
}

func TestMediaMTXRTMPListenAddress(t *testing.T) {
	got, err := mediaMTXRTMPListenAddress("rtmp://127.0.0.1:1938")
	if err != nil {
		t.Fatal(err)
	}

	if got != ":1938" {
		t.Fatalf("address = %q", got)
	}
}

func TestMediaMTXAPIReady(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v3/paths/list" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer server.Close()

	if !mediaMTXAPIReady(server.URL) {
		t.Fatal("MediaMTX API should be considered ready")
	}
}

func TestExternalMediaMTXSupervisorIsNoop(t *testing.T) {
	settings := Settings{
		MediaMTXManaged: false,
		MTXAPI:          "http://127.0.0.1:9999",
	}

	supervisor := newMediaMTXSupervisor(settings)

	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	status := supervisor.Status()

	if status.Managed {
		t.Fatal("external supervisor must not be managed")
	}
	if status.Running {
		t.Fatal("external supervisor must not start a process")
	}
}

func TestGeneratedMediaMTXConfigPath(t *testing.T) {
	dataDir := t.TempDir()

	settings := Settings{
		DataDir:         dataDir,
		MediaMTXManaged: true,
		MediaMTXBin:     "mediamtx",
	}

	supervisor := newMediaMTXSupervisor(settings)

	expected := filepath.Join(dataDir, "mediamtx.yml")

	if supervisor.config != expected {
		t.Fatalf(
			"config path = %q, expected %q",
			supervisor.config,
			expected,
		)
	}

	if !supervisor.generatedConfig {
		t.Fatal("configuration should be generated")
	}
}

func TestMediaMTXChildEnvironmentFiltersManagerVariables(t *testing.T) {
	input := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/test",
		"MTX_API=http://127.0.0.1:19999",
		"MTX_RTMP_BASE=rtmp://127.0.0.1:11938",
		"MTX_PATH_PREFIX=app/",
		"MTX_SOURCES_PREFIX=sources/",
		"MTX_LOGLEVEL=debug",
	}

	got := mediaMTXChildEnvironment(input)
	joined := strings.Join(got, "\n")

	blocked := []string{
		"MTX_API=",
		"MTX_RTMP_BASE=",
		"MTX_PATH_PREFIX=",
		"MTX_SOURCES_PREFIX=",
	}

	for _, key := range blocked {
		if strings.Contains(joined, key) {
			t.Fatalf("child environment still contains %q:\n%s", key, joined)
		}
	}

	expected := []string{
		"PATH=/usr/bin",
		"HOME=/tmp/test",
		"MTX_LOGLEVEL=debug",
	}

	for _, value := range expected {
		if !strings.Contains(joined, value) {
			t.Fatalf("child environment lost %q:\n%s", value, joined)
		}
	}
}
