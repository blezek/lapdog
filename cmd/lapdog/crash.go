package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"
)

// installCrashOutput preserves runtime panics and fatal errors from any
// goroutine. A windowsgui executable normally loses this output with stderr.
func installCrashOutput(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open crash report: %w", err)
	}
	if _, err := fmt.Fprintf(f, "\n--- LapDog process started %s ---\n", time.Now().Format(time.RFC3339)); err != nil {
		f.Close()
		return nil, fmt.Errorf("write crash report header: %w", err)
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		f.Close()
		return nil, fmt.Errorf("configure crash report: %w", err)
	}
	// SetCrashOutput duplicates the descriptor, so this copy is no longer needed.
	if err := f.Close(); err != nil {
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
		return nil, fmt.Errorf("close crash report: %w", err)
	}
	return func() { _ = debug.SetCrashOutput(nil, debug.CrashOptions{}) }, nil
}
