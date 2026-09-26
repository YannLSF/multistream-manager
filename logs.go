package main

import "strings"

type LogEntry struct {
	Level   string `json:"level"` // info, warning, error, fatal
	Message string `json:"message"`
}

type LogSummary struct {
	Warnings int `json:"warnings"`
	Errors   int `json:"errors"`
	Fatals   int `json:"fatals"`
}

func classifyLogLine(line string) string {
	low := strings.ToLower(strings.TrimSpace(line))
	if low == "" {
		return "info"
	}
	// When OBS stops publishing, the local RTMP input disappears and FFmpeg's
	// FLV demuxer commonly emits this scary-looking line. It describes the
	// source closing, not a failure of the remote destination.
	if strings.Contains(low, "error during demuxing: input/output error") {
		return "info"
	}
	fatalMarkers := []string{"fatal", "panic", "assertion failed"}
	for _, m := range fatalMarkers {
		if strings.Contains(low, m) {
			return "fatal"
		}
	}
	warningMarkers := []string{
		"warning", "negative cts", "invalid timestamps", "non-monoton", "non monoton",
		"deprecated", "past duration", "timestamp discontinuity", "queue input is backward",
	}
	for _, m := range warningMarkers {
		if strings.Contains(low, m) {
			return "warning"
		}
	}
	errorMarkers := []string{
		"error", "failed", "connection refused", "invalid stream key", "permission denied",
		"access denied", "authentication failed", "broken pipe", "could not write", "server returned 4",
		"server returned 5", "no such file", "not found", "unsupported codec", "i/o error",
	}
	for _, m := range errorMarkers {
		if strings.Contains(low, m) {
			return "error"
		}
	}
	return "info"
}

func structuredLogs(raw string) ([]LogEntry, LogSummary) {
	entries := []LogEntry{}
	summary := LogSummary{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		level := classifyLogLine(line)
		entries = append(entries, LogEntry{Level: level, Message: line})
		switch level {
		case "warning":
			summary.Warnings++
		case "error":
			summary.Errors++
		case "fatal":
			summary.Fatals++
		}
	}
	return entries, summary
}

func isSourceDemuxInputClosed(logs string) bool {
	return strings.Contains(strings.ToLower(logs), "error during demuxing: input/output error")
}
