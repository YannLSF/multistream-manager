//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

func configureMediaMTXOutput(
	cmd *exec.Cmd,
	settings Settings,
) (func(), error) {
	path := filepath.Join(
		settings.LogDir,
		"mediamtx.log",
	)

	f, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o600,
	)
	if err != nil {
		return nil, err
	}

	cmd.Stdout = f
	cmd.Stderr = f

	return func() {
		_ = f.Sync()
		_ = f.Close()
	}, nil
}
