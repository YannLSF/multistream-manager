package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RuntimeLayout describes where the application and its bundled runtime
// components are located.
//
// Portable mode is deliberately conservative: it is enabled only when both
// FFmpeg and FFprobe are present in the bin directory next to the Manager.
type RuntimeLayout struct {
	AppDir      string
	BinDir      string
	DataDir     string
	Portable    bool
	FFmpegBin   string
	FFprobeBin  string
	MediaMTXBin string
}

func detectRuntimeLayout() RuntimeLayout {
	return detectRuntimeLayoutAt(executableDir(), runtime.GOOS)
}

func detectRuntimeLayoutAt(appDir, goos string) RuntimeLayout {
	appDir = filepath.Clean(appDir)
	binDir := filepath.Join(appDir, "bin")

	ffmpegBundled := filepath.Join(
		binDir,
		runtimeExecutableName("ffmpeg", goos),
	)
	ffprobeBundled := filepath.Join(
		binDir,
		runtimeExecutableName("ffprobe", goos),
	)

	mediaMTXBundled := filepath.Join(
		binDir,
		runtimeExecutableName("mediamtx", goos),
	)

	portable := regularFile(ffmpegBundled) && regularFile(ffprobeBundled)

	layout := RuntimeLayout{
		AppDir:     appDir,
		BinDir:     binDir,
		DataDir:    "/data",
		Portable:   portable,
		FFmpegBin:  "ffmpeg",
		FFprobeBin: "ffprobe",
	}

	if portable {
		layout.DataDir = filepath.Join(appDir, "data")
		layout.FFmpegBin = ffmpegBundled
		layout.FFprobeBin = ffprobeBundled
	}

	if regularFile(mediaMTXBundled) {
		layout.MediaMTXBin = mediaMTXBundled
	}

	return layout
}

func executableDir() string {
	executable, err := os.Executable()
	if err != nil {
		if wd, wdErr := os.Getwd(); wdErr == nil {
			return wd
		}
		return "."
	}

	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	return filepath.Dir(executable)
}

func runtimeExecutableName(base, goos string) string {
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

func prepareRuntimeDataDir(settings Settings) error {
	return os.MkdirAll(settings.DataDir, 0700)
}

func logRuntimeDiagnostics(settings Settings) {
	layout := detectRuntimeLayout()

	mode := "system/container"
	if layout.Portable {
		mode = "portable"
	}

	log.Printf(
		"Runtime mode: %s ; app dir: %s ; data dir: %s",
		mode,
		layout.AppDir,
		settings.DataDir,
	)

	logBinaryVersion("FFmpeg", settings.FFmpegBin)
	logBinaryVersion("FFprobe", settings.FFprobeBin)

	if settings.MediaMTXManaged {
		log.Printf("MediaMTX supervision: managed")
		logBinaryVersionWithArgs(
			"MediaMTX",
			settings.MediaMTXBin,
			"--version",
		)
	} else {
		log.Printf(
			"MediaMTX supervision: external ; API: %s",
			settings.MTXAPI,
		)
	}
}

func logBinaryVersion(name, binary string) {
	logBinaryVersionWithArgs(name, binary, "-version")
}

func logBinaryVersionWithArgs(
	name string,
	binary string,
	args ...string,
) {
	version, err := binaryVersion(binary, args...)
	if err != nil {
		log.Printf(
			"WARNING: %s unavailable at %q: %v",
			name,
			binary,
			err,
		)
		return
	}

	log.Printf("%s: %s ; binary: %s", name, version, binary)
}

func binaryVersion(binary string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.Output()

	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}

	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if line == "" {
		return "unknown version", nil
	}

	return line, nil
}
