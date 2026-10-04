//go:build !windows

package main

import "context"

type desktopController struct {
	ctx context.Context
}

func preparePlatformCLI() {}

func newDesktopController(Settings) *desktopController {
	return &desktopController{
		ctx: context.Background(),
	}
}

func (d *desktopController) Context() context.Context {
	return d.ctx
}

func (d *desktopController) AttachApp(*App) {}

func (d *desktopController) Close() {}
