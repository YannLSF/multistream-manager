//go:build !linux

package main

import "fmt"

func readPasswordInteractive() ([]byte, error) {
	return nil, fmt.Errorf("interactive hidden password entry is supported by the production Linux build; use --hash-password-stdin for automation")
}
