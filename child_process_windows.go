//go:build windows

package main

import (
	"context"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configureChildProcess(
	cmd *exec.Cmd,
) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow

	return cmd
}

func newChildCommand(
	name string,
	args ...string,
) *exec.Cmd {
	return configureChildProcess(
		exec.Command(name, args...),
	)
}

func newChildCommandContext(
	ctx context.Context,
	name string,
	args ...string,
) *exec.Cmd {
	return configureChildProcess(
		exec.CommandContext(
			ctx,
			name,
			args...,
		),
	)
}
