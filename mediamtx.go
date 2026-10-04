package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type MediaMTXStatus struct {
	Managed   bool      `json:"managed"`
	Running   bool      `json:"running"`
	PID       int       `json:"pid,omitempty"`
	Binary    string    `json:"binary,omitempty"`
	Config    string    `json:"config,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

type MediaMTXSupervisor struct {
	mu sync.Mutex

	settings Settings

	managed         bool
	binary          string
	config          string
	generatedConfig bool

	cmd       *exec.Cmd
	done      chan struct{}
	startedAt time.Time
	exitErr   error
	stopping  bool
}

func newMediaMTXSupervisor(settings Settings) *MediaMTXSupervisor {
	configPath := strings.TrimSpace(settings.MediaMTXConfig)
	generated := false

	if settings.MediaMTXManaged && configPath == "" {
		configPath = filepath.Join(settings.DataDir, "mediamtx.yml")
		generated = true
	}

	return &MediaMTXSupervisor{
		settings:        settings,
		managed:         settings.MediaMTXManaged,
		binary:          settings.MediaMTXBin,
		config:          configPath,
		generatedConfig: generated,
	}
}

func (s *MediaMTXSupervisor) Status() MediaMTXStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := MediaMTXStatus{
		Managed:   s.managed,
		Running:   s.cmd != nil,
		Binary:    s.binary,
		Config:    s.config,
		StartedAt: s.startedAt,
	}

	if s.cmd != nil && s.cmd.Process != nil {
		status.PID = s.cmd.Process.Pid
	}

	if s.exitErr != nil {
		status.LastError = s.exitErr.Error()
	}

	return status
}

func (s *MediaMTXSupervisor) Start(ctx context.Context) error {
	if !s.managed {
		log.Printf(
			"MediaMTX supervision: external instance expected at %s",
			s.settings.MTXAPI,
		)
		return nil
	}

	if strings.TrimSpace(s.binary) == "" {
		return fmt.Errorf("managed MediaMTX requested but no binary is configured")
	}

	s.mu.Lock()
	if s.cmd != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	// Never take ownership of a MediaMTX instance that was already running.
	if mediaMTXAPIReady(s.settings.MTXAPI) {
		return fmt.Errorf(
			"MediaMTX API is already reachable at %s; refusing to start and own another instance",
			s.settings.MTXAPI,
		)
	}

	if s.generatedConfig {
		config, err := mediaMTXGeneratedConfig(s.settings)
		if err != nil {
			return fmt.Errorf("generate MediaMTX configuration: %w", err)
		}

		if err := os.WriteFile(s.config, []byte(config), 0o600); err != nil {
			return fmt.Errorf("write MediaMTX configuration: %w", err)
		}
	}

	if err := validateMediaMTXConfig(ctx, s.binary, s.config); err != nil {
		return err
	}

	cmd := exec.Command(s.binary, s.config)
	cmd.Env = mediaMTXChildEnvironment(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start MediaMTX: %w", err)
	}

	done := make(chan struct{})

	s.mu.Lock()
	s.cmd = cmd
	s.done = done
	s.startedAt = time.Now()
	s.exitErr = nil
	s.stopping = false
	s.mu.Unlock()

	go s.waitProcess(cmd, done)

	if err := s.waitUntilReady(ctx, 5*time.Second); err != nil {
		_ = s.Stop()
		return err
	}

	log.Printf(
		"Managed MediaMTX ready ; PID: %d ; config: %s",
		cmd.Process.Pid,
		s.config,
	)

	return nil
}

func (s *MediaMTXSupervisor) waitProcess(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()

	s.mu.Lock()

	stopping := s.stopping

	if s.cmd == cmd {
		s.cmd = nil

		if !stopping {
			if err != nil {
				s.exitErr = err
			} else {
				s.exitErr = fmt.Errorf("MediaMTX exited unexpectedly")
			}
		}
	}

	s.mu.Unlock()
	close(done)
}

func (s *MediaMTXSupervisor) waitUntilReady(
	ctx context.Context,
	timeout time.Duration,
) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		if mediaMTXAPIReady(s.settings.MTXAPI) {
			return nil
		}

		s.mu.Lock()
		done := s.done
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-done:
			status := s.Status()
			if status.LastError != "" {
				return fmt.Errorf(
					"MediaMTX exited before its API became ready: %s",
					status.LastError,
				)
			}
			return fmt.Errorf("MediaMTX exited before its API became ready")

		case <-timer.C:
			return fmt.Errorf(
				"timeout waiting for MediaMTX API at %s",
				s.settings.MTXAPI,
			)

		case <-ticker.C:
		}
	}
}

func (s *MediaMTXSupervisor) Stop() error {
	if !s.managed {
		return nil
	}

	s.mu.Lock()

	cmd := s.cmd
	done := s.done

	if cmd == nil {
		s.mu.Unlock()
		return nil
	}

	s.stopping = true
	process := cmd.Process

	s.mu.Unlock()

	if process != nil {
		// SIGINT works on Unix. On Windows os.Interrupt is not implemented,
		// therefore fall back to Process.Kill().
		if err := process.Signal(os.Interrupt); err != nil {
			_ = process.Kill()
		}
	}

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	select {
	case <-done:

	case <-timer.C:
		if process != nil {
			_ = process.Kill()
		}

		forceTimer := time.NewTimer(2 * time.Second)
		defer forceTimer.Stop()

		select {
		case <-done:
		case <-forceTimer.C:
			return fmt.Errorf("timeout stopping MediaMTX")
		}
	}

	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()

	log.Printf("Managed MediaMTX stopped")
	return nil
}

func mediaMTXGeneratedConfig(settings Settings) (string, error) {
	apiAddress, err := mediaMTXAPIListenAddress(settings.MTXAPI)
	if err != nil {
		return "", err
	}

	rtmpAddress, err := mediaMTXRTMPListenAddress(settings.MTXRTMPBase)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`logLevel: info
logDestinations: [stdout]

api: true
apiAddress: %s

rtmp: true
rtmpEncryption: "no"
rtmpAddress: %s

rtsp: false
hls: false
webrtc: false
srt: false
moq: false

paths:
  all_others:
`, apiAddress, rtmpAddress), nil
}

func mediaMTXAPIListenAddress(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid MTX_API: %w", err)
	}

	if u.Scheme != "http" {
		return "", fmt.Errorf(
			"managed MediaMTX requires MTX_API to use http, got %q",
			u.Scheme,
		)
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()

	if host == "" || port == "" {
		return "", fmt.Errorf(
			"managed MediaMTX requires MTX_API with host and port",
		)
	}

	if host == "localhost" {
		host = "127.0.0.1"
	} else {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf(
				"managed MediaMTX API must be bound to localhost, got %q",
				host,
			)
		}
	}

	return net.JoinHostPort(host, port), nil
}

func mediaMTXRTMPListenAddress(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid MTX_RTMP_BASE: %w", err)
	}

	if u.Scheme != "rtmp" {
		return "", fmt.Errorf(
			"managed MediaMTX requires MTX_RTMP_BASE to use rtmp, got %q",
			u.Scheme,
		)
	}

	port := u.Port()
	if port == "" {
		return "", fmt.Errorf(
			"managed MediaMTX requires MTX_RTMP_BASE with an explicit port",
		)
	}

	// MediaMTX listens on all interfaces so OBS can be local or remote.
	return ":" + port, nil
}

func validateMediaMTXConfig(
	ctx context.Context,
	binary string,
	config string,
) error {
	validateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(
		validateCtx,
		binary,
		"--validate-conf="+config,
	)
	cmd.Env = mediaMTXChildEnvironment(os.Environ())

	out, err := cmd.CombinedOutput()

	if validateCtx.Err() != nil {
		return fmt.Errorf(
			"MediaMTX configuration validation timeout: %w",
			validateCtx.Err(),
		)
	}

	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}

		return fmt.Errorf(
			"invalid MediaMTX configuration: %s",
			message,
		)
	}

	return nil
}

func mediaMTXChildEnvironment(environ []string) []string {
	// These variables belong to Multistream Manager. MediaMTX also uses
	// the MTX_* namespace for its own configuration overrides, therefore
	// passing them to the child process would make MediaMTX interpret
	// Manager settings as native MediaMTX settings.
	blocked := map[string]struct{}{
		"MTX_API":            {},
		"MTX_RTMP_BASE":      {},
		"MTX_PATH_PREFIX":    {},
		"MTX_SOURCES_PREFIX": {},
	}

	out := make([]string, 0, len(environ))

	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, found := blocked[key]; found {
				continue
			}
		}

		out = append(out, entry)
	}

	return out
}

func mediaMTXAPIReady(base string) bool {
	client := &http.Client{
		Timeout: 500 * time.Millisecond,
	}

	resp, err := client.Get(
		strings.TrimRight(base, "/") + "/v3/paths/list",
	)
	if err != nil {
		return false
	}

	defer resp.Body.Close()
	return resp.StatusCode/100 == 2
}
