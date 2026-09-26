package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	presetCatalogMu sync.RWMutex
	presetCatalog   = append([]Preset(nil), builtInPresetCatalog...)
)

func presetCatalogSnapshot() []Preset {
	presetCatalogMu.RLock()
	defer presetCatalogMu.RUnlock()
	return append([]Preset(nil), presetCatalog...)
}

func presetByID(id string) Preset {
	presetCatalogMu.RLock()
	defer presetCatalogMu.RUnlock()
	for _, p := range presetCatalog {
		if p.ID == id {
			return p
		}
	}
	for _, p := range presetCatalog {
		if p.ID == "custom" {
			return p
		}
	}
	return Preset{ID: "custom", Name: "RTMP / RTMPS personnalisé", Constraints: PresetConstraints{Orientation: "any"}}
}

func validatePresetCatalog(items []Preset) error {
	if len(items) == 0 {
		return fmt.Errorf("catalogue vide")
	}
	seen := make(map[string]bool, len(items))
	hasCustom := false
	for i := range items {
		p := &items[i]
		p.ID = strings.TrimSpace(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		p.Category = strings.TrimSpace(p.Category)
		p.DefaultServer = strings.TrimSpace(p.DefaultServer)
		p.ServerHint = strings.TrimSpace(p.ServerHint)
		p.KeyMode = strings.TrimSpace(p.KeyMode)
		if p.ID == "" || p.Name == "" {
			return fmt.Errorf("preset #%d: id et name sont obligatoires", i+1)
		}
		if seen[p.ID] {
			return fmt.Errorf("id de preset dupliqué: %s", p.ID)
		}
		seen[p.ID] = true
		if p.ID == "custom" {
			hasCustom = true
		}
		switch p.Constraints.Orientation {
		case "", "any", "landscape", "portrait":
		default:
			return fmt.Errorf("preset %s: orientation invalide", p.ID)
		}
	}
	if !hasCustom {
		return fmt.Errorf("le catalogue doit contenir le preset custom")
	}
	return nil
}

func setPresetCatalog(items []Preset) error {
	copyItems := append([]Preset(nil), items...)
	if err := validatePresetCatalog(copyItems); err != nil {
		return err
	}
	presetCatalogMu.Lock()
	presetCatalog = copyItems
	presetCatalogMu.Unlock()
	return nil
}

func presetsPath(dataDir string) string { return filepath.Join(dataDir, "presets.json") }

func writePresetCatalogFile(path string, items []Preset) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadPresetCatalog(dataDir string) error {
	path := presetsPath(dataDir)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		items := append([]Preset(nil), builtInPresetCatalog...)
		if err := setPresetCatalog(items); err != nil {
			return err
		}
		return writePresetCatalogFile(path, items)
	}
	if err != nil {
		return err
	}
	var items []Preset
	if err := json.Unmarshal(b, &items); err != nil {
		return fmt.Errorf("presets.json invalide: %w", err)
	}
	return setPresetCatalog(items)
}

func importPresetCatalog(dataDir string, body []byte) error {
	var items []Preset
	if err := json.Unmarshal(body, &items); err != nil {
		var wrapper struct {
			Presets []Preset `json:"presets"`
		}
		if err2 := json.Unmarshal(body, &wrapper); err2 != nil || len(wrapper.Presets) == 0 {
			return fmt.Errorf("JSON de catalogue invalide")
		}
		items = wrapper.Presets
	}
	if err := validatePresetCatalog(items); err != nil {
		return err
	}
	if err := writePresetCatalogFile(presetsPath(dataDir), items); err != nil {
		return err
	}
	return setPresetCatalog(items)
}
