package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ProcessResources struct {
	PID        int     `json:"pid,omitempty"`
	CPUPercent float64 `json:"cpu_percent"`
	RSSBytes   uint64  `json:"rss_bytes"`
}

type ResourceSummary struct {
	Manager   ProcessResources `json:"manager"`
	Forwards  ProcessResources `json:"forwards"`
	Previews  ProcessResources `json:"previews"`
	Total     ProcessResources `json:"total"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
}

type procCPUSample struct {
	Ticks uint64
	At    time.Time
}

func detectClockTicks() float64 {
	out, err := exec.Command("getconf", "CLK_TCK").Output()
	if err == nil {
		if n, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); err == nil && n > 0 {
			return n
		}
	}
	return 100
}

func readProcCounters(pid int) (uint64, uint64, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	s := string(b)
	closeIdx := strings.LastIndex(s, ")")
	if closeIdx < 0 || closeIdx+2 >= len(s) {
		return 0, 0, fmt.Errorf("invalid /proc stat")
	}
	fields := strings.Fields(s[closeIdx+2:])
	if len(fields) <= 12 {
		return 0, 0, fmt.Errorf("short /proc stat")
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("invalid cpu counters")
	}

	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	var rss uint64
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseUint(f[1], 10, 64)
				rss = kb * 1024
			}
			break
		}
	}
	return utime + stime, rss, nil
}

func addResources(dst *ProcessResources, src ProcessResources) {
	dst.CPUPercent += src.CPUPercent
	dst.RSSBytes += src.RSSBytes
}

func (a *App) sampleResources() {
	now := time.Now()
	a.mu.Lock()
	forwardPIDs := make(map[string]int, len(a.processes))
	for id, ps := range a.processes {
		if ps != nil && ps.cmd != nil && ps.cmd.Process != nil {
			forwardPIDs[id] = ps.cmd.Process.Pid
		}
	}
	previewPIDs := make(map[string]int, len(a.previews))
	for id, ps := range a.previews {
		if ps != nil && ps.cmd != nil && ps.cmd.Process != nil {
			previewPIDs[id] = ps.cmd.Process.Pid
		}
	}
	prev := a.resourcePrev
	if prev == nil {
		prev = make(map[int]procCPUSample)
	}
	clockTicks := a.clockTicks
	if clockTicks <= 0 {
		clockTicks = 100
	}
	a.mu.Unlock()

	current := make(map[int]procCPUSample)
	sampleOne := func(pid int) ProcessResources {
		ticks, rss, err := readProcCounters(pid)
		if err != nil {
			return ProcessResources{PID: pid}
		}
		res := ProcessResources{PID: pid, RSSBytes: rss}
		current[pid] = procCPUSample{Ticks: ticks, At: now}
		if p, ok := prev[pid]; ok && ticks >= p.Ticks {
			elapsed := now.Sub(p.At).Seconds()
			if elapsed > 0 {
				res.CPUPercent = (float64(ticks-p.Ticks) / clockTicks) / elapsed * 100
				if res.CPUPercent < 0 {
					res.CPUPercent = 0
				}
			}
		}
		return res
	}

	manager := sampleOne(os.Getpid())
	byDest := make(map[string]ProcessResources, len(forwardPIDs))
	forwards := ProcessResources{}
	for id, pid := range forwardPIDs {
		res := sampleOne(pid)
		byDest[id] = res
		addResources(&forwards, res)
	}
	byPreview := make(map[string]ProcessResources, len(previewPIDs))
	previews := ProcessResources{}
	for id, pid := range previewPIDs {
		res := sampleOne(pid)
		byPreview[id] = res
		addResources(&previews, res)
	}
	total := ProcessResources{}
	addResources(&total, manager)
	addResources(&total, forwards)
	addResources(&total, previews)

	a.mu.Lock()
	a.resourcePrev = current
	a.resourceByDest = byDest
	a.resourceByPreview = byPreview
	a.resources = ResourceSummary{Manager: manager, Forwards: forwards, Previews: previews, Total: total, UpdatedAt: now}
	a.mu.Unlock()
}
