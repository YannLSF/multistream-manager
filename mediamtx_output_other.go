//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func configureMediaMTXOutput(
	cmd *exec.Cmd,
	_ Settings,
) (func(), error) {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return func() {}, nil
}
