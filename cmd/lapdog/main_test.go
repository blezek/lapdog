package main

import (
	"bytes"
	"errors"
	"io"
	stdlog "log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/applog"
	"github.com/blezek/lapdog/internal/config"
)

func TestConsoleRequestedBeforeFlagParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "absent", args: []string{"--debug"}, want: false},
		{name: "long", args: []string{"--console", "--debug"}, want: true},
		{name: "short", args: []string{"-console"}, want: true},
		{name: "explicit true", args: []string{"--console=true"}, want: true},
		{name: "explicit false", args: []string{"--console=false"}, want: false},
		{name: "last value wins", args: []string{"--console", "--console=false"}, want: false},
		{name: "invalid value still attaches for error output", args: []string{"--console=perhaps"}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := consoleRequested(test.args); got != test.want {
				t.Errorf("consoleRequested(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestRecoverRunPanicWritesStackAndReturnsError(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	var runErr error
	func() {
		defer recoverRunPanic(logger, &runErr)
		panic("startup exploded")
	}()
	if runErr == nil || !strings.Contains(runErr.Error(), "startup exploded") {
		t.Fatalf("recovered error = %v, want startup panic", runErr)
	}
	for _, want := range []string{"application panicked", "startup exploded", "TestRecoverRunPanicWritesStackAndReturnsError"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("panic log missing %q: %s", want, output.String())
		}
	}
}

func TestApplyRuntimeConfigAppliesLiveSettings(t *testing.T) {
	cfg := config.Default()
	cfg.PollIntervalSeconds = 2.5
	cfg.MinSessionSeconds = 45
	cfg.CaptureEnabled = false
	cfg.CaptureMaxBytes = 512
	cfg.StartWithWindows = false

	var autostartEnabled bool
	var autostartPath string
	target := &fakeRuntimeTarget{}
	applyRuntimeConfig(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		target,
		`C:\Program Files\LapDog\lapdog.exe`,
		func(enabled bool, path string) error {
			autostartEnabled = enabled
			autostartPath = path
			return nil
		},
		cfg,
	)

	if target.interval != 2500*time.Millisecond {
		t.Errorf("interval = %s, want 2.5s", target.interval)
	}
	if target.minSession != 45*time.Second {
		t.Errorf("minSession = %s, want 45s", target.minSession)
	}
	if target.captureEnabled {
		t.Error("captureEnabled = true, want false")
	}
	if target.captureMaxBytes != 512 {
		t.Errorf("captureMaxBytes = %d, want 512", target.captureMaxBytes)
	}
	if autostartEnabled {
		t.Error("autostart enabled = true, want false")
	}
	if autostartPath != `C:\Program Files\LapDog\lapdog.exe` {
		t.Errorf("autostart path = %q", autostartPath)
	}
}

func TestBindInterfaceUsesPreferredPortWhenAvailable(t *testing.T) {
	var calls []string
	ln, binding, err := bindInterface(47047, func(network, address string) (net.Listener, error) {
		calls = append(calls, network+" "+address)
		return fakeListener{addr: tcpAddr(47047)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if len(calls) != 1 || calls[0] != "tcp 127.0.0.1:47047" {
		t.Fatalf("listen calls = %v, want only the preferred port", calls)
	}
	if binding.Port != 47047 || binding.URL != "http://127.0.0.1:47047" {
		t.Errorf("binding = %+v, want preferred port and URL", binding)
	}
	if binding.fallback || binding.Notice != "" {
		t.Errorf("fallback = %v notice = %q, want no fallback", binding.fallback, binding.Notice)
	}
}

func TestBindInterfaceFallsBackToRandomLoopbackPort(t *testing.T) {
	preferredErr := errors.New("preferred port busy")
	var calls []string
	ln, binding, err := bindInterface(47047, func(network, address string) (net.Listener, error) {
		calls = append(calls, network+" "+address)
		if address == "127.0.0.1:47047" {
			return nil, preferredErr
		}
		if address != "127.0.0.1:0" {
			t.Fatalf("fallback address = %q, want random loopback port", address)
		}
		return fakeListener{addr: tcpAddr(53124)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	if len(calls) != 2 {
		t.Fatalf("listen calls = %v, want preferred then random fallback", calls)
	}
	if !binding.fallback || !errors.Is(binding.preferredErr, preferredErr) {
		t.Errorf("fallback=%v preferredErr=%v, want preferred error retained", binding.fallback, binding.preferredErr)
	}
	if binding.Port != 53124 || binding.URL != "http://127.0.0.1:53124" {
		t.Errorf("binding = %+v, want random fallback port and URL", binding)
	}
	if !strings.Contains(binding.Notice, "53124") || !strings.Contains(binding.Notice, "47047") {
		t.Errorf("notice = %q, want both selected and preferred ports", binding.Notice)
	}
}

func TestBindInterfaceReportsWhenPreferredAndFallbackFail(t *testing.T) {
	_, _, err := bindInterface(47047, func(string, string) (net.Listener, error) {
		return nil, errors.New("bind failed")
	})
	if err == nil {
		t.Fatal("bindInterface returned nil error, want failure")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:47047") ||
		!strings.Contains(err.Error(), "random loopback fallback") {
		t.Errorf("error = %q, want preferred and fallback context", err)
	}
}

func TestStartInterfaceDoesNotTryPortsWhenHandlerFails(t *testing.T) {
	handlerErr := errors.New("embedded UI missing")
	calls := 0
	binding := startInterfaceWith(
		fakeInterfaceServer{handlerErr: handlerErr},
		47047,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(string, string) (net.Listener, error) {
			calls++
			return fakeListener{addr: tcpAddr(47047)}, nil
		},
	)
	if calls != 0 {
		t.Fatalf("listen calls = %d, want none for non-bind startup errors", calls)
	}
	if binding.Error != handlerErr.Error() {
		t.Errorf("binding error = %q, want %q", binding.Error, handlerErr.Error())
	}
}

type fakeRuntimeTarget struct {
	interval        time.Duration
	minSession      time.Duration
	captureEnabled  bool
	captureMaxBytes int64
}

func (t *fakeRuntimeTarget) SetInterval(d time.Duration) {
	t.interval = d
}

func (t *fakeRuntimeTarget) SetMinSession(d time.Duration) {
	t.minSession = d
}

func (t *fakeRuntimeTarget) SetCapture(enabled bool, maxBytes int64) {
	t.captureEnabled = enabled
	t.captureMaxBytes = maxBytes
}

type fakeInterfaceServer struct {
	handlerErr error
}

func (s fakeInterfaceServer) InterfaceHandler() (http.Handler, error) {
	if s.handlerErr != nil {
		return nil, s.handlerErr
	}
	return http.NewServeMux(), nil
}

func (fakeInterfaceServer) Serve(net.Listener, http.Handler) error {
	return http.ErrServerClosed
}

type fakeListener struct {
	addr net.Addr
}

func (l fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("fake listener does not accept")
}

func (l fakeListener) Close() error {
	return nil
}

func (l fakeListener) Addr() net.Addr {
	return l.addr
}

func tcpAddr(port int) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
}

// Exercise the real startup path: the GUI build cannot rely on stderr to
// explain why an existing database could not be opened.
func TestStartupFailureWrittenToLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the XDG data-directory override")
	}
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	originalWriter := stdlog.Writer()
	originalFlags := stdlog.Flags()
	originalPrefix := stdlog.Prefix()
	defer func() {
		stdlog.SetOutput(originalWriter)
		stdlog.SetFlags(originalFlags)
		stdlog.SetPrefix(originalPrefix)
	}()
	var standardLog bytes.Buffer
	stdlog.SetOutput(&standardLog)
	stdlog.SetFlags(0)
	stdlog.SetPrefix("before-run:")
	originalLevel := applog.Level.Level()
	applog.Level.Set(slog.LevelInfo)
	defer applog.Level.Set(originalLevel)
	dir := filepath.Join(root, "lapdog")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Debug = false
	if err := config.Save(config.ConfigPath(dir), cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.DBPath(dir), []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	args := os.Args
	os.Args = []string{"lapdog", "--debug"}
	defer func() { os.Args = args }()
	err := run()
	if err == nil {
		t.Fatal("startup succeeded with corrupt database")
	}
	body, readErr := os.ReadFile(config.LogPath(dir))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, want := range []string{
		"application failed", err.Error(), "opening database",
		"debug=true", "savedDebug=false", "commandLineDebug=true",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("startup log missing %q: %s", want, body)
		}
	}
	stdlog.Print("writer-restored")
	if got := standardLog.String(); !strings.Contains(got, "before-run:writer-restored") {
		t.Errorf("standard logger was not restored after run: %q", got)
	}
	if got := applog.Level.Level(); got != slog.LevelInfo {
		t.Errorf("application log level after run = %v, want restored info level", got)
	}
	crashBody, readErr := os.ReadFile(config.CrashPath(dir))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(crashBody), "LapDog process started") {
		t.Errorf("crash report missing process header: %s", crashBody)
	}
}
