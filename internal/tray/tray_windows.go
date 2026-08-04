//go:build windows

package tray

import (
	"fmt"
	"time"

	"fyne.io/systray"
)

// Run takes over the calling goroutine and runs the tray until Quit.
//
// systray requires the main goroutine, so the caller must have started the
// collector and the HTTP server as goroutines first.
func Run(opts Options) {
	systray.Run(func() { onReady(opts) }, func() {})
}

func onReady(opts Options) {
	// Shut the menu down when the process is asked to stop by any other route, so
	// an interrupt or a service stop is not left with a live tray icon attached to
	// a collector that has already exited.
	if opts.Done != nil {
		go func() {
			<-opts.Done
			systray.Quit()
		}()
	}

	systray.SetIcon(icon(stateDisconnected))
	systray.SetTitle("LapDog")
	systray.SetTooltip("LapDog — waiting for iRacing")

	header := systray.AddMenuItem("LapDog", "")
	header.Disable()
	detail := systray.AddMenuItem("Not connected", "")
	detail.Disable()

	systray.AddSeparator()
	open := systray.AddMenuItem("Open LapDog", "Open the user interface in a browser")
	pause := systray.AddMenuItemCheckbox("Pause recording", "Stop recording without exiting", false)

	systray.AddSeparator()
	folder := systray.AddMenuItem("Open data folder", "Show the database and captures")

	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Exit LapDog")

	if opts.PortConflict != "" {
		open.Disable()
		detail.SetTitle("Interface unavailable: " + opts.PortConflict)
	}

	go refresh(opts, header, detail)

	for {
		select {
		case <-open.ClickedCh:
			if err := openURL(opts.URL); err != nil {
				opts.Log.Error("could not open the browser", "url", opts.URL, "err", err)
			}
		case <-pause.ClickedCh:
			// The checkbox is the source of truth for the desired state, so it is
			// toggled first and the collector told what it now says.
			if pause.Checked() {
				pause.Uncheck()
				opts.SetPaused(false)
			} else {
				pause.Check()
				opts.SetPaused(true)
			}
		case <-folder.ClickedCh:
			if err := openPath(opts.DataDir); err != nil {
				opts.Log.Error("could not open the data folder", "path", opts.DataDir, "err", err)
			}
		case <-quit.ClickedCh:
			systray.Quit()
			if opts.Quit != nil {
				opts.Quit()
			}
			return
		}
	}
}

// refresh keeps the icon, tooltip and menu header in step with the collector.
func refresh(opts Options, header, detail *systray.MenuItem) {
	tick := time.NewTicker(refreshInterval)
	defer tick.Stop()

	for range tick.C {
		s := opts.Status()
		systray.SetIcon(icon(stateFor(s)))
		header.SetTitle("LapDog · " + stateText(s))

		// A port conflict is more important than session detail, and the detail
		// line is where it is already displayed.
		if opts.PortConflict != "" {
			continue
		}
		if s.SessionLabel == "" {
			detail.SetTitle("No active session")
			systray.SetTooltip("LapDog — " + stateText(s))
			continue
		}
		line := fmt.Sprintf("%s · %s", s.SessionLabel, s.TrackName)
		detail.SetTitle(line)
		systray.SetTooltip(fmt.Sprintf("LapDog — %s\nDriving %s · %d laps",
			line, formatDuration(s.DrivingSeconds), s.Laps))
	}
}
