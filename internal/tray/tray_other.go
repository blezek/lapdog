//go:build !windows

package tray

// Run serves as the tray on platforms that have none.
//
// There is no menu here — the systray dependency needs CGO off Windows, and
// CGO_ENABLED=0 is load-bearing for this project. Rather than stub the tray out
// into something that returns immediately, which would make `go run ./cmd/lapdog`
// exit before serving anything, it blocks until the caller signals shutdown. That
// makes the whole application runnable on a development machine: the interface is
// served, the collector runs, and an interrupt exits cleanly.
//
// It waits on opts.Done rather than handling signals itself. Doing its own
// signal.Notify raced with the caller's handler: an interrupt arriving before this
// function ran was delivered only to the caller, so the wait here never woke and
// the process hung until killed. One handler, owned by the caller, cannot race
// with itself.
func Run(opts Options) {
	if opts.Log != nil {
		msg := "no system tray on this platform; interrupt to stop"
		if opts.PortConflict != "" {
			opts.Log.Warn(msg, "url", opts.URL, "portConflict", opts.PortConflict)
		} else {
			opts.Log.Info(msg, "url", opts.URL)
		}
	}
	if opts.Done == nil {
		// A nil channel blocks forever, which would be the same hang by another
		// route. Refusing to start is worse than saying so and returning.
		if opts.Log != nil {
			opts.Log.Error("tray: Done channel is nil; nothing would stop the process")
		}
		return
	}
	<-opts.Done
}
