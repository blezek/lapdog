// Package applog opens LapDog's application log.
package applog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// maxLogBytes is the size at which the log is rotated to a .1 file.
//
// Steady-state operation is nearly silent, so this only fills up when something is
// genuinely wrong, and one generation of history is enough to diagnose it. An
// unbounded log on a machine that races every week is a slow disk leak nobody
// notices until it matters.
const maxLogBytes = 4 << 20

// Open returns a logger writing to both the log file and stderr, plus a Closer for
// the file.
//
// Writing to stderr as well costs nothing in the tray build, where there is no
// console to receive it, and makes `go run` during development readable without a
// second flag.
func Open(path string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, fmt.Errorf("applog: create directory: %w", err)
	}
	if err := rotateIfLarge(path); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("applog: open %s: %w", path, err)
	}
	h := slog.NewTextHandler(io.MultiWriter(f, os.Stderr), &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(h), f, nil
}

// rotateIfLarge moves an oversized log aside, keeping one generation.
//
// Rotation happens at open rather than during writing: the process is long-lived
// but restarts often enough, and checking on every line would mean a stat per log
// entry to guard against a case that arises once.
func rotateIfLarge(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("applog: stat %s: %w", path, err)
	}
	if fi.Size() < maxLogBytes {
		return nil
	}
	// Rename over any existing .1, keeping exactly one previous generation.
	if err := os.Rename(path, path+".1"); err != nil {
		return fmt.Errorf("applog: rotate %s: %w", path, err)
	}
	return nil
}
