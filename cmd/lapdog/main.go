// Command lapdog is the LapDog tray application. It records iRacing session time
// to a local database and serves a web interface on loopback.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/blezek/lapdog/internal/api"
	"github.com/blezek/lapdog/internal/applog"
	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/tray"
	"github.com/blezek/lapdog/internal/version"
)

// listenSettleTime is how long to wait for the HTTP listener to fail before the
// tray reads the outcome.
//
// Binding fails immediately when the port is taken, so this only has to outlast a
// syscall. Without it the tray would draw its menu before the result was known and
// a port conflict would appear only at the first refresh tick.
const listenSettleTime = 250 * time.Millisecond

// shutdownGrace is how long the collector gets to flush the active session after
// the user quits. A session in progress is worth waiting for; a hung one is not
// worth blocking exit over.
const shutdownGrace = 3 * time.Second

type runtimeConfigTarget interface {
	SetInterval(time.Duration)
	SetMinSession(time.Duration)
	SetCapture(bool, int64)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "lapdog: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	// WAL is unsafe on a network filesystem, so refuse loudly rather than risking
	// intermittent corruption that would look like random data loss.
	if err := config.CheckLocalFilesystem(dataDir); err != nil {
		return err
	}

	log, logCloser, err := applog.Open(config.LogPath(dataDir))
	if err != nil {
		return err
	}
	defer logCloser.Close()
	log.Info("starting", "version", version.Version, "dataDir", dataDir,
		"platform", runtime.GOOS, "arch", runtime.GOARCH,
		"logPath", config.LogPath(dataDir))

	cfgStore, err := config.NewStore(config.ConfigPath(dataDir))
	if err != nil {
		return err
	}
	cfg := cfgStore.Get()

	// Apply the log level before anything else runs, so the start-up sequence itself is
	// captured at the level the user chose.
	applog.SetDebug(cfg.Debug)
	log.Info("configuration loaded",
		"debug", cfg.Debug,
		"pollIntervalSeconds", cfg.PollIntervalSeconds,
		"minSessionSeconds", cfg.MinSessionSeconds,
		"captureEnabled", cfg.CaptureEnabled,
		"port", cfg.Port,
		"configPath", config.ConfigPath(dataDir))

	exePath := ""
	if exe, err := os.Executable(); err == nil {
		exePath = exe
		applyAutostart(log, cfg.StartWithWindows, exePath, config.SetAutostart)
	}

	st, err := store.Open(config.DBPath(dataDir))
	if err != nil {
		return err
	}
	defer st.Close()

	// The live source narrates its read path through this logger. On the machine that
	// matters there is no debugger, so the trace is the only instrument.
	src, err := source.NewLiveWithLogger(log)
	if err != nil {
		return err
	}
	defer src.Close()
	log.Debug("telemetry source created",
		"platform", runtime.GOOS,
		"liveReadingAvailable", runtime.GOOS == "windows")

	coll, err := collector.New(collector.Options{
		Source:     src,
		Store:      st,
		Clock:      collector.RealClock{},
		Interval:   cfg.PollInterval(),
		MinSession: cfg.MinSession(),

		CaptureEnabled:  cfg.CaptureEnabled,
		CaptureDir:      config.CapturesDir(dataDir),
		CaptureMaxBytes: cfg.CaptureMaxBytes,

		Logger: log,
	})
	if err != nil {
		return err
	}

	// Runtime settings take effect live; a port change does not, which the settings
	// API reports to the user rather than silently ignoring.
	cfgStore.OnChange(func(c config.Config) {
		applyRuntimeConfig(log, coll, exePath, config.SetAutostart, c)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	collDone := make(chan struct{})
	go func() {
		defer close(collDone)
		if err := coll.Run(ctx); err != nil {
			log.Error("collector stopped", "err", err)
		}
	}()

	// A port already in use is not fatal: losing the interface must not lose
	// session data, which is the part that cannot be recovered later.
	//
	// portConflict is written by the server goroutine and read by the tray on the
	// main goroutine, so it is atomic rather than a plain string.
	srv := api.New(st, coll, cfgStore, log)
	var portConflict atomic.Value
	portConflict.Store("")
	go func() {
		err := srv.ListenAndServe(cfg.Port)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("user interface unavailable", "port", cfg.Port, "err", err)
			portConflict.Store(fmt.Sprintf("port %d is in use", cfg.Port))
		}
	}()
	time.Sleep(listenSettleTime)

	tray.Run(tray.Options{
		Status:       coll.Status,
		SetPaused:    coll.SetPaused,
		URL:          fmt.Sprintf("http://127.0.0.1:%d", cfg.Port),
		DataDir:      dataDir,
		PortConflict: portConflict.Load().(string),
		Quit:         stop,
		// One signal handler, owned here. The tray watches this instead of
		// installing its own, which used to race: an interrupt during start-up
		// went only to the context and the tray then waited forever for a signal
		// that had already been delivered.
		Done: ctx.Done(),
		Log:  log,
	})

	// The tray returned, so the user chose Quit. Give the collector a moment to
	// flush the active session before exiting.
	stop()
	select {
	case <-collDone:
	case <-time.After(shutdownGrace):
		log.Warn("collector did not stop within the shutdown grace period",
			"grace", shutdownGrace)
	}
	log.Info("stopped")
	return nil
}

func applyRuntimeConfig(log *slog.Logger, target runtimeConfigTarget, exePath string, setAutostart func(bool, string) error, c config.Config) {
	target.SetInterval(c.PollInterval())
	target.SetMinSession(c.MinSession())
	target.SetCapture(c.CaptureEnabled, c.CaptureMaxBytes)
	if exePath != "" {
		applyAutostart(log, c.StartWithWindows, exePath, setAutostart)
	}
	// The level changes immediately, so switching debug on in settings starts
	// producing detail without a restart — which matters when the only way to
	// observe the problem is to be running while it happens.
	applog.SetDebug(c.Debug)
	log.Info("configuration updated",
		"pollIntervalSeconds", c.PollIntervalSeconds,
		"minSessionSeconds", c.MinSessionSeconds,
		"captureEnabled", c.CaptureEnabled,
		"captureMaxBytes", c.CaptureMaxBytes,
		"startWithWindows", c.StartWithWindows,
		"debug", c.Debug)
}

func applyAutostart(log *slog.Logger, enabled bool, exePath string, setAutostart func(bool, string) error) {
	if setAutostart == nil {
		return
	}
	if err := setAutostart(enabled, exePath); err != nil {
		// Not being able to write the Run key is not worth refusing to start over:
		// the application works, it just will not launch itself.
		log.Warn("could not update the startup entry", "err", err)
	}
}
