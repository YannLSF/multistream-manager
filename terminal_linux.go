//go:build linux

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
)

func ioctlTermios(fd uintptr, req uintptr, value *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(value)))
	if errno != 0 {
		return errno
	}
	return nil
}

func readPasswordLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		fragment, prefix, err := r.ReadLine()
		if len(out)+len(fragment) > 4096 {
			zeroBytes(out)
			return nil, fmt.Errorf("password too long")
		}
		out = append(out, fragment...)
		if err != nil {
			if err == io.EOF && len(out) > 0 {
				return out, nil
			}
			zeroBytes(out)
			return nil, err
		}
		if !prefix {
			return out, nil
		}
	}
}

func readPasswordInteractive() ([]byte, error) {
	fd := os.Stdin.Fd()
	var original syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &original); err != nil {
		return nil, fmt.Errorf("interactive password entry requires a TTY; use --hash-password-stdin for automation")
	}

	hidden := original
	hidden.Lflag &^= syscall.ECHO
	if err := ioctlTermios(fd, syscall.TCSETS, &hidden); err != nil {
		return nil, fmt.Errorf("cannot disable terminal echo: %w", err)
	}

	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(func() {
			_ = ioctlTermios(fd, syscall.TCSETS, &original)
		})
	}
	defer restore()

	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		select {
		case sig := <-sigCh:
			restore()
			fmt.Fprintln(os.Stderr)
			signal.Stop(sigCh)
			signal.Reset(sig)
			if proc, err := os.FindProcess(os.Getpid()); err == nil {
				_ = proc.Signal(sig)
			}
		case <-done:
		}
	}()
	defer func() {
		close(done)
		signal.Stop(sigCh)
	}()

	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "Mot de passe : ")
	password, err := readPasswordLine(reader)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("cannot read password: %w", err)
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("password must not be empty")
	}

	fmt.Fprint(os.Stderr, "Confirmer le mot de passe : ")
	confirmation, err := readPasswordLine(reader)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		zeroBytes(password)
		return nil, fmt.Errorf("cannot read password confirmation: %w", err)
	}
	defer zeroBytes(confirmation)

	if len(password) != len(confirmation) {
		zeroBytes(password)
		return nil, fmt.Errorf("passwords do not match")
	}
	var diff byte
	for i := range password {
		diff |= password[i] ^ confirmation[i]
	}
	if diff != 0 {
		zeroBytes(password)
		return nil, fmt.Errorf("passwords do not match")
	}
	return password, nil
}
