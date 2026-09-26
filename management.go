package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) registerManagementAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/config/export", a.configExportHandler)
	mux.HandleFunc("/api/config/import", a.configImportHandler)
	mux.HandleFunc("/api/presets/export", a.presetsExportHandler)
	mux.HandleFunc("/api/presets/import", a.presetsImportHandler)
	mux.HandleFunc("/api/presets/reload", a.presetsReloadHandler)
	mux.HandleFunc("/api/errors", a.errorHistoryHandler)
}

func (a *App) configExportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	a.mu.Lock()
	cfg := StoredConfig{Destinations: append([]Destination{}, a.config.Destinations...)}
	a.mu.Unlock()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="multistream-config.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(append(b, '\n'))
}

func validateImportedConfig(cfg *StoredConfig) error {
	seen := make(map[string]bool)
	for i := range cfg.Destinations {
		d := &cfg.Destinations[i]
		d.Name = strings.TrimSpace(d.Name)
		d.Provider = strings.TrimSpace(d.Provider)
		d.Server = strings.TrimSpace(d.Server)
		if d.Name == "" {
			return fmt.Errorf("destination #%d: name est obligatoire", i+1)
		}
		if d.Provider == "" {
			d.Provider = "custom"
		}
		d.Server = normalizeServer(d.Provider, d.Server)
		if d.ID == "" {
			d.ID = slugID(d.Name)
		} else {
			d.ID = slugID(d.ID)
		}
		base := d.ID
		n := 2
		for seen[d.ID] {
			d.ID = fmt.Sprintf("%s-%d", base, n)
			n++
		}
		seen[d.ID] = true
	}
	return nil
}

func (a *App) waitAllDestinationsStopped(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		n := len(a.processes)
		a.mu.Unlock()
		if n == 0 {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func (a *App) configImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cfg StoredConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		writeError(w, http.StatusBadRequest, "config JSON invalide")
		return
	}
	if err := validateImportedConfig(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	a.stopAllPreviews()
	a.stopAll(true)
	if !a.waitAllDestinationsStopped(4 * time.Second) {
		writeError(w, http.StatusConflict, "impossible d'arrêter tous les forwards avant l'import")
		return
	}

	a.mu.Lock()
	a.config = cfg
	a.runtime = make(map[string]*RuntimeState)
	for _, d := range cfg.Destinations {
		a.runtime[d.ID] = &RuntimeState{}
	}
	err = a.saveConfigLocked()
	a.mu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "destinations": len(cfg.Destinations)})
}

func (a *App) presetsExportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	b, err := json.MarshalIndent(presetCatalogSnapshot(), "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="multistream-presets.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(append(b, '\n'))
}

func (a *App) presetsImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := importPresetCatalog(a.settings.DataDir, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "presets": len(presetCatalogSnapshot())})
}

func (a *App) presetsReloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if err := loadPresetCatalog(a.settings.DataDir); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "presets": len(presetCatalogSnapshot())})
}

func (a *App) errorHistoryHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 1000 {
				limit = n
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": a.errorHistorySnapshot(limit)})
	case http.MethodDelete:
		if err := a.clearErrorHistory(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
