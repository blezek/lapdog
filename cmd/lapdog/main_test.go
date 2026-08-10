package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/config"
)

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
