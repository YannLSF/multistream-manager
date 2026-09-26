package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ErrorEvent struct {
	ID            string    `json:"id"`
	Time          time.Time `json:"time"`
	Kind          string    `json:"kind"` // forward, preview, system
	DestinationID string    `json:"destination_id,omitempty"`
	Destination   string    `json:"destination,omitempty"`
	Message       string    `json:"message"`
	RetryCount    int       `json:"retry_count,omitempty"`
}

func (a *App) errorHistoryPath() string {
	return filepath.Join(a.settings.DataDir, "error-history.json")
}

func (a *App) loadErrorHistory() error {
	b, err := os.ReadFile(a.errorHistoryPath())
	if errors.Is(err, os.ErrNotExist) {
		a.errorHistory = []ErrorEvent{}
		return a.saveErrorHistoryLocked()
	}
	if err != nil {
		return err
	}
	var items []ErrorEvent
	if err := json.Unmarshal(b, &items); err != nil {
		return err
	}
	limit := a.settings.ErrorHistoryLimit
	if limit <= 0 {
		limit = 500
	}
	if len(items) > limit {
		items = append([]ErrorEvent(nil), items[len(items)-limit:]...)
	}
	a.errorHistory = items
	return nil
}

func (a *App) saveErrorHistoryLocked() error {
	b, err := json.MarshalIndent(a.errorHistory, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.errorHistoryPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.errorHistoryPath())
}

func (a *App) recordError(kind, destinationID, destination, message string, retryCount int) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if len(message) > 800 {
		message = message[:800] + "…"
	}
	event := ErrorEvent{
		ID:            fmt.Sprintf("%d", time.Now().UnixNano()),
		Time:          time.Now(),
		Kind:          kind,
		DestinationID: destinationID,
		Destination:   destination,
		Message:       message,
		RetryCount:    retryCount,
	}
	a.historyMu.Lock()
	a.errorHistory = append(a.errorHistory, event)
	limit := a.settings.ErrorHistoryLimit
	if limit <= 0 {
		limit = 500
	}
	if len(a.errorHistory) > limit {
		a.errorHistory = append([]ErrorEvent(nil), a.errorHistory[len(a.errorHistory)-limit:]...)
	}
	_ = a.saveErrorHistoryLocked()
	a.historyMu.Unlock()
}

func (a *App) errorHistorySnapshot(limit int) []ErrorEvent {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if limit <= 0 || limit > len(a.errorHistory) {
		limit = len(a.errorHistory)
	}
	start := len(a.errorHistory) - limit
	out := append([]ErrorEvent(nil), a.errorHistory[start:]...)
	// API returns newest first for the WebUI.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (a *App) clearErrorHistory() error {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	a.errorHistory = []ErrorEvent{}
	return a.saveErrorHistoryLocked()
}
