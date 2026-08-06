package applog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesAndWrites(t *testing.T) {
	// A nested path, because the data directory may not exist on first run.
	path := filepath.Join(t.TempDir(), "sub", "lapdog.log")
	log, closer, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	log.Info("hello", "key", "value")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file was not created: %v", err)
	}
	if !strings.Contains(string(b), "hello") || !strings.Contains(string(b), "key=value") {
		t.Errorf("log contents = %q", b)
	}
}

func TestOpenAppendsRatherThanTruncating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.log")
	log, c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("first")
	c.Close()

	log, c, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("second")
	c.Close()

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "first") {
		t.Error("reopening truncated the log; entries from before the restart must survive")
	}
	if !strings.Contains(string(b), "second") {
		t.Error("the second entry is missing")
	}
}

func TestRotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lapdog.log")
	if err := os.WriteFile(path, make([]byte, maxLogBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	log, c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	log.Info("after rotation")
	c.Close()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("no .1 generation after rotation: %v", err)
	}
	b, _ := os.ReadFile(path)
	if len(b) > 1024 {
		t.Errorf("the new log is %d bytes; it should start fresh", len(b))
	}
}

// Exactly one generation is kept, so the log cannot grow without bound.
func TestRotationKeepsOnlyOneGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lapdog.log")

	for i := 0; i < 3; i++ {
		if err := os.WriteFile(path, make([]byte, maxLogBytes+1), 0o644); err != nil {
			t.Fatal(err)
		}
		_, c, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want exactly the log and one .1 generation", names)
	}
}

// A log below the threshold is left alone rather than rotated on every start.
func TestDoesNotRotateSmallLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.log")
	if err := os.WriteFile(path, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("a small log was rotated; history would be discarded on every restart")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "existing") {
		t.Error("the existing entry was lost")
	}
}

// The level is shared and switchable, so turning debug on in settings affects loggers
// that were already handed out. A logger captured before the change must honour it
// too, or the toggle would appear to do nothing until a restart.
func TestSetDebugAffectsAnExistingLogger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lapdog.log")
	log, c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	SetDebug(false)
	log.Debug("suppressed-line")
	SetDebug(true)
	log.Debug("emitted-line")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "suppressed-line") {
		t.Error("a debug line was written while the level was info")
	}
	if !strings.Contains(got, "emitted-line") {
		t.Error("a debug line was dropped after debug was switched on; the toggle does not reach existing loggers")
	}
}

// Debug is on by default, because the machine that needs diagnosing has no
// development environment and a quiet log there is no information at all.
func TestDefaultLevelIsDebug(t *testing.T) {
	if Level.Level() != slog.LevelDebug {
		t.Errorf("default level = %v, want debug", Level.Level())
	}
}
