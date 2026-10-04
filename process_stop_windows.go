//go:build windows

package main

import "os"

func stopProcess(p *os.Process) error {
	if p == nil {
		return nil
	}

	// Interrupt signals are not implemented for os.Process on Windows.
	// Kill the managed child directly instead.
	return p.Kill()
}
