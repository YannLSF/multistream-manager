package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRuntimeTool(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("test"), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeExecutableName(t *testing.T) {
	if got := runtimeExecutableName("ffmpeg", "linux"); got != "ffmpeg" {
		t.Fatalf("linux executable = %q", got)
	}

	if got := runtimeExecutableName("ffmpeg", "windows"); got != "ffmpeg.exe" {
		t.Fatalf("windows executable = %q", got)
	}
}

func TestDetectRuntimeLayoutSystemFallback(t *testing.T) {
	dir := t.TempDir()

	got := detectRuntimeLayoutAt(dir, "linux")

	if got.Portable {
		t.Fatal("unexpected portable mode")
	}
	if got.DataDir != "/data" {
		t.Fatalf("data dir = %q", got.DataDir)
	}
	if got.FFmpegBin != "ffmpeg" {
		t.Fatalf("ffmpeg = %q", got.FFmpegBin)
	}
	if got.FFprobeBin != "ffprobe" {
		t.Fatalf("ffprobe = %q", got.FFprobeBin)
	}
}

func TestDetectRuntimeLayoutRequiresBothTools(t *testing.T) {
	dir := t.TempDir()

	writeRuntimeTool(
		t,
		filepath.Join(dir, "bin", "ffmpeg"),
	)

	got := detectRuntimeLayoutAt(dir, "linux")

	if got.Portable {
		t.Fatal("portable mode must require ffmpeg and ffprobe")
	}
}

func TestDetectRuntimeLayoutPortableLinux(t *testing.T) {
	dir := t.TempDir()

	ffmpeg := filepath.Join(dir, "bin", "ffmpeg")
	ffprobe := filepath.Join(dir, "bin", "ffprobe")

	writeRuntimeTool(t, ffmpeg)
	writeRuntimeTool(t, ffprobe)

	got := detectRuntimeLayoutAt(dir, "linux")

	if !got.Portable {
		t.Fatal("portable mode not detected")
	}
	if got.DataDir != filepath.Join(dir, "data") {
		t.Fatalf("data dir = %q", got.DataDir)
	}
	if got.FFmpegBin != ffmpeg {
		t.Fatalf("ffmpeg = %q", got.FFmpegBin)
	}
	if got.FFprobeBin != ffprobe {
		t.Fatalf("ffprobe = %q", got.FFprobeBin)
	}
}

func TestDetectRuntimeLayoutPortableWindows(t *testing.T) {
	dir := t.TempDir()

	ffmpeg := filepath.Join(dir, "bin", "ffmpeg.exe")
	ffprobe := filepath.Join(dir, "bin", "ffprobe.exe")

	writeRuntimeTool(t, ffmpeg)
	writeRuntimeTool(t, ffprobe)

	got := detectRuntimeLayoutAt(dir, "windows")

	if !got.Portable {
		t.Fatal("portable Windows mode not detected")
	}
	if got.FFmpegBin != ffmpeg {
		t.Fatalf("ffmpeg = %q", got.FFmpegBin)
	}
	if got.FFprobeBin != ffprobe {
		t.Fatalf("ffprobe = %q", got.FFprobeBin)
	}
}

func TestDetectRuntimeLayoutPortableMediaMTX(t *testing.T) {
	dir := t.TempDir()

	ffmpeg := filepath.Join(dir, "bin", "ffmpeg")
	ffprobe := filepath.Join(dir, "bin", "ffprobe")
	mediamtx := filepath.Join(dir, "bin", "mediamtx")

	writeRuntimeTool(t, ffmpeg)
	writeRuntimeTool(t, ffprobe)
	writeRuntimeTool(t, mediamtx)

	got := detectRuntimeLayoutAt(dir, "linux")

	if !got.Portable {
		t.Fatal("portable mode not detected")
	}
	if got.MediaMTXBin != mediamtx {
		t.Fatalf("mediamtx = %q", got.MediaMTXBin)
	}
}
