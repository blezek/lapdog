// Package tray runs LapDog's system tray icon and menu.
//
// The systray dependency is confined to tray_windows.go. It needs CGO on macOS
// and Linux to talk to Cocoa and D-Bus, and CGO_ENABLED=0 is load-bearing here —
// it is what lets modernc.org/sqlite be used and the Windows binary be
// cross-compiled without a mingw toolchain. On Windows systray is pure Go, so the
// shipped target is unaffected. Everything that can be decided without a menu
// lives in this file and is tested on every platform.
package tray

import (
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"time"

	"github.com/blezek/lapdog/internal/collector"
)

// refreshInterval is how often the menu and icon are brought up to date.
//
// Two seconds is fast enough that the state never looks stale to someone who has
// just alt-tabbed out of the sim, and slow enough to be free.
const refreshInterval = 2 * time.Second

// Options configures the tray.
type Options struct {
	// Status reports what the collector is doing.
	Status func() collector.Status
	// SetPaused pauses or resumes recording.
	SetPaused func(bool)
	// URL is the address the interface is served at.
	URL string
	// DataDir is opened by the "Open data folder" item.
	DataDir string
	// PortConflict is non-empty when the interface could not bind its port. The
	// tray reports it rather than offering a menu item that opens a dead page.
	PortConflict string
	// Quit is called when the user chooses Quit.
	Quit func()
	// Done is closed when the process should shut down for a reason other than
	// the menu — an interrupt, or a service stop.
	//
	// The tray watches this rather than installing its own signal handler. Having
	// two handlers meant a signal arriving before the tray finished starting was
	// consumed by the other one, leaving the tray waiting for a signal that had
	// already been delivered: an interrupt during start-up hung the process until
	// it was killed outright.
	Done <-chan struct{}
	// Log receives errors from menu actions.
	Log *slog.Logger
}

// stateFor maps collector status onto an icon state.
//
// Paused outranks connected: someone who has paused recording wants to see that
// nothing is being recorded, whether or not the sim happens to be running.
func stateFor(s collector.Status) iconState {
	switch {
	case s.Paused:
		return statePaused
	case s.Connected:
		return stateConnected
	default:
		return stateDisconnected
	}
}

// stateText is the human-readable form of the same three states.
func stateText(s collector.Status) string {
	switch stateFor(s) {
	case statePaused:
		return "Paused"
	case stateConnected:
		return "Connected"
	default:
		return "Not connected"
	}
}

// formatDuration renders seconds as h:mm.
func formatDuration(sec float64) string {
	d := time.Duration(sec) * time.Second
	return fmt.Sprintf("%d:%02d", int(d.Hours()), int(d.Minutes())%60)
}

// openURL opens a URL in the default browser.
func openURL(url string) error {
	switch runtime.GOOS {
	case "windows":
		// rundll32 rather than `start`, which is a shell builtin and would need a
		// cmd.exe wrapper and its quoting rules.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// openPath opens a directory in the system file manager.
func openPath(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
