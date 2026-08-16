// Command lapdog is the LapDog tray application. It records iRacing session time
// to a local database and serves a web interface on loopback.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/blezek/lapdog/internal/api"
	"github.com/blezek/lapdog/internal/applog"
	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/config"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/tray"
	"github.com/blezek/lapdog/internal/updater"
	"github.com/blezek/lapdog/internal/version"
)

// shutdownGrace is how long the collector gets to flush the active session after
// the user quits. A session in progress is worth waiting for; a hung one is not
// worth blocking exit over.
const shutdownGrace = 3 * time.Second

type runtimeConfigTarget interface {
	SetInterval(time.Duration)
	SetMinSession(time.Duration)
	SetCapture(bool, int64)
}

type interfaceServer interface {
	InterfaceHandler() (http.Handler, error)
	Serve(net.Listener, http.Handler) error
}

type interfaceBinding struct {
	URL           string
	Port          int
	PreferredPort int
	Notice        string
	Error         string

	fallback     bool
	preferredErr error
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "lapdog: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if handoff, ok, err := updater.ParseHandoff(os.Args[1:]); ok {
		if err != nil {
			return err
		}
		if err := updater.RunHandoff(handoff); err != nil {
			updater.RecordHandoffFailure(handoff.StatePath, err)
			return err
		}
		return nil
	}
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

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate running executable: %w", err)
	}
	applyAutostart(log, cfg.StartWithWindows, exePath, config.SetAutostart)
	if version.Version != "dev" {
		if err := config.ReconcileInstalledVersion(exePath, version.Version); err != nil {
			log.Warn("could not reconcile installed version", "err", err)
		}
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

	// A port already in use is not fatal: losing the configured interface port must
	// not lose session data, which is the part that cannot be recovered later.
	// The preferred port remains the user's setting, but when it cannot be bound we
	// keep the same single binary running by asking the OS for a random free
	// loopback port with 127.0.0.1:0. The listener returned for that random port is
	// passed directly to the HTTP server, so there is no gap where another process
	// can claim the port between discovery and use.
	srv := api.New(st, coll, cfgStore, log)
	updates, err := updater.New(updater.Options{
		Version: version.Version, Revision: version.Revision, DataDir: dataDir, Executable: exePath,
		Gate: coll, Reindexing: srv.Reindexing, Shutdown: stop, Log: log,
	})
	if err != nil {
		return err
	}
	srv.SetUpdater(updates)
	updates.Start(ctx)
	iface := startInterface(srv, cfg.Port, log)

	tray.Run(tray.Options{
		Status:          coll.Status,
		SetPaused:       coll.SetPaused,
		URL:             iface.URL,
		PreferredPort:   iface.PreferredPort,
		InterfacePort:   iface.Port,
		InterfaceNotice: iface.Notice,
		InterfaceError:  iface.Error,
		DataDir:         dataDir,
		Quit:            stop,
		// One signal handler, owned here. The tray watches this instead of
		// installing its own, which used to race: an interrupt during start-up
		// went only to the context and the tray then waited forever for a signal
		// that had already been delivered.
		Done:         ctx.Done(),
		Log:          log,
		UpdateStatus: updates.Snapshot,
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

func startInterface(srv interfaceServer, preferredPort int, log *slog.Logger) interfaceBinding {
	return startInterfaceWith(srv, preferredPort, log, net.Listen)
}

func startInterfaceWith(srv interfaceServer, preferredPort int, log *slog.Logger, listen listenFunc) interfaceBinding {
	binding := interfaceBinding{PreferredPort: preferredPort}

	h, err := srv.InterfaceHandler()
	if err != nil {
		log.Error("user interface unavailable", "err", err)
		binding.Error = err.Error()
		return binding
	}

	ln, binding, err := bindInterface(preferredPort, listen)
	if err != nil {
		log.Error("user interface unavailable", "preferredPort", preferredPort, "err", err)
		binding.Error = err.Error()
		return binding
	}
	if binding.fallback {
		log.Warn("preferred interface port unavailable; using random fallback port",
			"preferredPort", preferredPort,
			"port", binding.Port,
			"url", binding.URL,
			"err", binding.preferredErr)
	}

	go func() {
		if err := srv.Serve(ln, h); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("user interface stopped", "url", binding.URL, "err", err)
		}
	}()
	return binding
}

type listenFunc func(network, address string) (net.Listener, error)

func bindInterface(preferredPort int, listen listenFunc) (net.Listener, interfaceBinding, error) {
	binding := interfaceBinding{PreferredPort: preferredPort}
	preferredAddr := interfaceListenAddr(preferredPort)
	ln, err := listen("tcp", preferredAddr)
	if err != nil {
		binding.fallback = true
		binding.preferredErr = err
		ln, err = listen("tcp", interfaceListenAddr(0))
		if err != nil {
			return nil, binding, fmt.Errorf(
				"cannot listen on %s and random loopback fallback: preferred: %v; fallback: %w",
				preferredAddr, binding.preferredErr, err,
			)
		}
	}

	binding.Port = listenerPort(ln)
	binding.URL = interfaceURL(binding.Port)
	if binding.fallback {
		binding.Notice = fmt.Sprintf(
			"Interface on port %d; %d unavailable",
			binding.Port,
			preferredPort,
		)
	}
	return ln, binding, nil
}

func interfaceListenAddr(port int) string {
	return net.JoinHostPort(api.LoopbackHost, strconv.Itoa(port))
}

func interfaceURL(port int) string {
	return "http://" + interfaceListenAddr(port)
}

func listenerPort(ln net.Listener) int {
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		return addr.Port
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 0
	}
	return n
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
