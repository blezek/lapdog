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
	"github.com/blezek/lapdog/internal/updater"
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
	// PreferredPort is the configured port the app tries first.
	PreferredPort int
	// InterfacePort is the actual port the interface is served on. It may differ
	// from PreferredPort when the app fell back to a random loopback port.
	InterfacePort int
	// InterfaceNotice is non-empty when the interface is available but not exactly
	// as configured, for example because a random fallback port was selected.
	InterfaceNotice string
	// InterfaceError is non-empty when the interface could not be served at all.
	// The tray reports it rather than offering a menu item that opens a dead page.
	InterfaceError string
	// DataDir is opened by the "Open data folder" item.
	DataDir string
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
	// UpdateStatus drives the dynamic cue without making the tray own updater work.
	UpdateStatus func() updater.Snapshot
}

func updateTitle(s updater.Snapshot) string {
	if s.Available == nil {
		return ""
	}
	switch s.State {
	case updater.Downloading:
		if s.Download != nil && s.Download.TotalBytes != nil && *s.Download.TotalBytes > 0 {
			percent := min(int64(100), s.Download.DownloadedBytes*100 / *s.Download.TotalBytes)
			return fmt.Sprintf("Downloading update %s (%d%%)", s.Available.Version, percent)
		}
		return "Downloading update " + s.Available.Version
	case updater.Waiting:
		return "Update ready; waiting for session to finish"
	case updater.Applying:
		return "Applying update " + s.Available.Version
	case updater.RestartRequired:
		return "Update ready; restart required"
	case updater.Failed:
		if s.AcceptedVersion != nil {
			return "Update failed; open LapDog for details"
		}
	case updater.Available, updater.Deferred, updater.Skipped:
		return "Update available: " + s.Available.Version
	}
	return ""
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

func openTitle(opts Options) string {
	if opts.InterfaceError != "" || opts.InterfacePort == 0 || opts.InterfacePort == opts.PreferredPort {
		return "Open LapDog"
	}
	return fmt.Sprintf("Open LapDog (port %d)", opts.InterfacePort)
}

func openHint(opts Options) string {
	if opts.InterfaceError != "" {
		return "User interface unavailable"
	}
	if opts.URL == "" {
		return "Open the user interface in a browser"
	}
	return "Open " + opts.URL
}

func idleDetail(opts Options) string {
	if opts.InterfaceNotice != "" {
		return opts.InterfaceNotice
	}
	return "No active session"
}

func interfaceTooltipSuffix(opts Options) string {
	if opts.InterfaceNotice != "" {
		return "\n" + opts.InterfaceNotice
	}
	return ""
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
