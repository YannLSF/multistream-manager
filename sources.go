package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const primarySourceID = "primary"

type SourceStatus struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Primary bool        `json:"primary"`
	State   SourceState `json:"state"`
	Tracks  []Track     `json:"tracks"`
}

type sourceEntry struct {
	ID             string
	Label          string
	Path           string
	Primary        bool
	State          SourceState
	Tracks         []Track
	LastProbeAt    time.Time
	LastProbedPath string
}

type sourceProbeTarget struct {
	ID   string
	Path string
}

func normalizedSourceID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return primarySourceID
	}
	return id
}

func (a *App) fetchReadyPaths() ([]string, error) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(a.settings.MTXAPI + "/v3/paths/list")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("MediaMTX API returned %s", resp.Status)
	}

	var data struct {
		Items []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(data.Items))
	for _, it := range data.Items {
		if it.Ready {
			paths = append(paths, it.Name)
		}
	}

	sort.Strings(paths)
	return paths, nil
}

func (a *App) primarySourcePath(paths []string) (string, bool) {
	for _, path := range paths {
		if strings.HasPrefix(path, a.settings.MTXPathPrefix) {
			return path, true
		}
	}
	return "", false
}

func additionalSourceLabel(path, prefix string) string {
	label := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if label == "" {
		return path
	}
	return label
}

func (a *App) syncSourceCatalog(paths []string, apiErr error, now time.Time) {
	var probes []sourceProbeTarget

	a.mu.Lock()

	if a.sources == nil {
		a.sources = make(map[string]*sourceEntry)
	}

	primary := a.sources[primarySourceID]
	if primary == nil {
		primary = &sourceEntry{
			ID:      primarySourceID,
			Label:   "Twitch / Enhanced RTMP",
			Primary: true,
		}
		a.sources[primarySourceID] = primary
	}

	// La source historique app/... reste la source principale et son path
	// réel n'est jamais exposé par l'API publique.
	primary.Path = a.sourcePath
	primary.State = a.source
	primary.Tracks = append([]Track(nil), a.tracks...)
	primary.LastProbeAt = a.lastProbeAt
	primary.LastProbedPath = a.lastProbedPath

	if apiErr != nil {
		for id, src := range a.sources {
			if id == primarySourceID {
				continue
			}
			src.State.Online = false
			src.State.ProbeError = "MediaMTX API inaccessible"
			src.State.LastUpdated = now
		}
		a.mu.Unlock()
		return
	}

	seen := map[string]bool{
		primarySourceID: true,
	}

	for _, path := range paths {
		// Les paths app/... appartiennent tous au namespace principal.
		// Un seul est sélectionné par primarySourcePath().
		if strings.HasPrefix(path, a.settings.MTXPathPrefix) {
			continue
		}

		if !strings.HasPrefix(path, a.settings.MTXSourcesPrefix) {
			continue
		}

		label := additionalSourceLabel(path, a.settings.MTXSourcesPrefix)
		if label == "" {
			continue
		}

		// Pour les sources additionnelles, le path MediaMTX est aussi
		// l'identifiant stable. Exemple : sources/obs2.
		id := path
		seen[id] = true

		src := a.sources[id]
		if src == nil {
			src = &sourceEntry{
				ID:      id,
				Label:   label,
				Path:    path,
				Primary: false,
			}
			a.sources[id] = src
		}

		wasOnline := src.State.Online
		pathChanged := src.Path != path

		src.ID = id
		src.Label = label
		src.Path = path
		src.Primary = false
		src.State.Online = true
		src.State.LastUpdated = now

		if !wasOnline || pathChanged {
			src.State.Since = now
		}

		if src.State.ProbeError == "MediaMTX API inaccessible" {
			src.State.ProbeError = ""
		}

		if pathChanged {
			src.Tracks = nil
			src.State.TrackCount = 0
			src.State.VideoCount = 0
			src.State.AudioCount = 0
			src.LastProbedPath = ""
		}

		shouldProbe := !wasOnline ||
			pathChanged ||
			len(src.Tracks) == 0 ||
			time.Since(src.LastProbeAt) >= 30*time.Second

		if shouldProbe {
			probes = append(probes, sourceProbeTarget{
				ID:   id,
				Path: path,
			})
		}
	}

	for id, src := range a.sources {
		if id == primarySourceID || seen[id] {
			continue
		}

		src.State.Online = false
		src.State.ProbeError = ""
		src.State.LastUpdated = now
	}

	a.mu.Unlock()

	// Les ffprobe sont effectués hors verrou.
	for _, target := range probes {
		tracks, probeErr := a.probeTracks(target.Path)

		a.mu.Lock()
		src := a.sources[target.ID]

		// La source a pu disparaître pendant le probe.
		if src == nil || src.Path != target.Path || !src.State.Online {
			a.mu.Unlock()
			continue
		}

		src.LastProbeAt = time.Now()
		src.LastProbedPath = target.Path

		if probeErr != nil {
			src.State.ProbeError = shortErr(probeErr)
		} else {
			src.Tracks = tracks
			src.State.TrackCount = len(tracks)
			src.State.VideoCount, src.State.AudioCount = countTracks(tracks)
			src.State.ProbeError = ""
		}

		a.mu.Unlock()
	}
}

func (a *App) publicSourcesLocked() []SourceStatus {
	if len(a.sources) == 0 {
		return []SourceStatus{
			{
				ID:      primarySourceID,
				Label:   "Twitch / Enhanced RTMP",
				Primary: true,
				State:   a.source,
				Tracks:  append([]Track(nil), a.tracks...),
			},
		}
	}

	out := make([]SourceStatus, 0, len(a.sources))

	for _, src := range a.sources {
		out = append(out, SourceStatus{
			ID:      src.ID,
			Label:   src.Label,
			Primary: src.Primary,
			State:   src.State,
			Tracks:  append([]Track(nil), src.Tracks...),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}

		li := strings.ToLower(out[i].Label)
		lj := strings.ToLower(out[j].Label)

		if li == lj {
			return out[i].ID < out[j].ID
		}
		return li < lj
	})

	return out
}
