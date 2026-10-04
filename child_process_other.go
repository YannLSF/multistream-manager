//go:build !windows

package main

import (
	"context"
	"os/exec"
)

func newChildCommand(
	name string,
	args ...string,
) *exec.Cmd {
	return exec.Command(name, args...)
}

func newChildCommandContext(
	ctx context.Context,
	name string,
	args ...string,
) *exec.Cmd {
	return exec.CommandContext(
		ctx,
		name,
		args...,
	)
}
