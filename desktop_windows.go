//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/systray"
)

const desktopAppName = "Ylyxium Multistream Manager"

//go:embed assets/ylyxium-multistream-manager.ico
var desktopIcon []byte

type desktopController struct {
	ctx    context.Context
	cancel context.CancelFunc

	settings Settings

	mu      sync.RWMutex
	app     *App
	restart bool
	logFile *os.File

	trayReady chan struct{}
	trayDone  chan struct{}
	closeOnce sync.Once
}

func preparePlatformCLI() {
	wantsConsole := false

	for _, arg := range os.Args[1:] {
		if arg == "--hash-password" {
			wantsConsole = true
			break
		}
	}

	if !wantsConsole {
		return
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	attachConsole := kernel32.NewProc("AttachConsole")

	const attachParentProcess = ^uintptr(0)

	ok, _, _ := attachConsole.Call(attachParentProcess)
	if ok == 0 {
		return
	}

	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}

	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
	}

	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stderr = f
		log.SetOutput(f)
	}
}

func newDesktopController(settings Settings) *desktopController {
	ctx, cancel := context.WithCancel(context.Background())

	d := &desktopController{
		ctx:       ctx,
		cancel:    cancel,
		settings:  settings,
		trayReady: make(chan struct{}),
		trayDone:  make(chan struct{}),
	}

	logPath := filepath.Join(
		settings.LogDir,
		"ylyxium-multistream-manager.log",
	)

	f, err := os.OpenFile(
		logPath,
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o600,
	)
	if err != nil {
		showDesktopMessage(
			"Ylyxium Multistream Manager",
			"Impossible d'ouvrir le fichier de log :\n\n"+err.Error(),
			0x10,
		)
		os.Exit(1)
	}

	d.logFile = f
	log.SetOutput(f)

	go d.runTray()

	return d
}

func (d *desktopController) Context() context.Context {
	return d.ctx
}

func (d *desktopController) AttachApp(app *App) {
	d.mu.Lock()
	d.app = app
	d.mu.Unlock()
}

func (d *desktopController) runTray() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(d.trayDone)

	systray.Run(
		d.onTrayReady,
		func() {
			d.cancel()
		},
	)
}

func (d *desktopController) onTrayReady() {
	systray.SetIcon(desktopIcon)
	systray.SetTooltip(desktopAppName)

	systray.SetOnTapped(func() {
		if err := d.openDashboard(); err != nil {
			showDesktopMessage(
				"Ouverture impossible",
				err.Error(),
				0x10,
			)
		}
	})

	status := systray.AddMenuItem(
		"État : démarrage...",
		"État de Ylyxium Multistream Manager",
	)
	status.Disable()

	open := systray.AddMenuItem(
		"Ouvrir le tableau de bord",
		"Ouvrir l'interface Web",
	)

	systray.AddSeparator()

	restart := systray.AddMenuItem(
		"Redémarrer",
		"Redémarrer Ylyxium Multistream Manager",
	)

	about := systray.AddMenuItem(
		"À propos",
		"Informations sur l'application",
	)

	systray.AddSeparator()

	stop := systray.AddMenuItem(
		"Arrêter",
		"Arrêter l'application et ses services",
	)

	close(d.trayReady)

	go d.statusLoop(status)

	go func() {
		for {
			select {
			case <-open.ClickedCh:
				if err := d.openDashboard(); err != nil {
					showDesktopMessage(
						"Ouverture impossible",
						err.Error(),
						0x10,
					)
				}

			case <-restart.ClickedCh:
				status.SetTitle(
					"État : redémarrage en cours...",
				)

				d.mu.Lock()
				d.restart = true
				d.mu.Unlock()

				d.cancel()

			case <-about.ClickedCh:
				showDesktopMessage(
					"À propos",
					fmt.Sprintf(
						"%s v%s\n\n"+
							"MediaMTX + FFmpeg 9\n\n"+
							"Gestionnaire local de multistreaming.",
						desktopAppName,
						appVersion,
					),
					0x40,
				)

			case <-stop.ClickedCh:
				status.SetTitle(
					"État : arrêt en cours...",
				)
				d.cancel()

			case <-d.ctx.Done():
				return
			}
		}
	}()
}

func (d *desktopController) statusLoop(
	item *systray.MenuItem,
) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	update := func() {
		title := d.statusTitle()

		item.SetTitle(title)

		systray.SetTooltip(
			desktopAppName + " - " + title,
		)
	}

	update()

	for {
		select {
		case <-ticker.C:
			update()

		case <-d.ctx.Done():
			return
		}
	}
}

func (d *desktopController) statusTitle() string {
	d.mu.RLock()
	app := d.app
	d.mu.RUnlock()

	if app == nil {
		return "État : démarrage..."
	}

	app.mu.Lock()

	online := app.source.Online
	running := 0

	for _, state := range app.runtime {
		if state != nil && state.Running {
			running++
		}
	}

	app.mu.Unlock()

	source := "source hors ligne"

	if online {
		source = "source en ligne"
	}

	if running == 0 {
		return "État : prêt · " + source
	}

	return fmt.Sprintf(
		"État : prêt · %s · %d sortie(s)",
		source,
		running,
	)
}

func (d *desktopController) openDashboard() error {
	url := desktopDashboardURL(d.settings.Bind)

	cmd := exec.Command(
		"rundll32.exe",
		"url.dll,FileProtocolHandler",
		url,
	)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	return cmd.Start()
}

func desktopDashboardURL(bind string) string {
	bind = strings.TrimSpace(bind)

	if bind == "" {
		return "http://127.0.0.1:8090/"
	}

	if strings.HasPrefix(bind, ":") {
		return "http://127.0.0.1" + bind + "/"
	}

	host, port, err := net.SplitHostPort(bind)

	if err != nil || port == "" {
		return "http://127.0.0.1:8090/"
	}

	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}

	return "http://" +
		net.JoinHostPort(host, port) +
		"/"
}

func (d *desktopController) Close() {
	d.closeOnce.Do(func() {
		d.cancel()

		select {
		case <-d.trayReady:
			systray.Quit()

		default:
		}

		select {
		case <-d.trayDone:

		case <-time.After(2 * time.Second):
		}

		d.mu.Lock()
		restart := d.restart
		logFile := d.logFile
		d.logFile = nil
		d.mu.Unlock()

		if logFile != nil {
			_ = logFile.Sync()
			_ = logFile.Close()
		}

		if restart {
			if err := restartDesktopApplication(); err != nil {
				showDesktopMessage(
					"Redémarrage impossible",
					err.Error(),
					0x10,
				)
			}
		}
	})
}

func restartDesktopApplication() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(
		exe,
		os.Args[1:]...,
	)

	if cwd, err := os.Getwd(); err == nil {
		cmd.Dir = cwd
	}

	cmd.Env = os.Environ()

	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	return cmd.Process.Release()
}

func showDesktopMessage(
	title string,
	message string,
	flags uintptr,
) {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}

	messagePtr, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return
	}

	_, _, _ = messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		flags,
	)
}
