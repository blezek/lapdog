// Package config loads and saves LapDog's user settings.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Poll interval bounds, in seconds. Below the minimum the collector burns CPU
// for no extra fidelity; above the maximum, time accounting and lap attribution
// get too coarse to be useful.
const (
	MinPollSeconds = 0.25
	MaxPollSeconds = 30.0
)

// DefaultPort is the fixed web UI port. The bind address is always loopback and
// is deliberately not configurable.
const DefaultPort = 47047

// DefaultCaptureMaxBytes is the default capture retention cap, 2 GiB. A value of
// 0 means unlimited.
const DefaultCaptureMaxBytes int64 = 2 << 30

// Config is the persisted user settings.
//
// Deliberately absent, per the design spec section 14: no offline-testing toggle
// (always counted), no replay-time toggle (never counted), no anonymisation
// toggle (never anonymised).
type Config struct {
	PollIntervalSeconds float64 `json:"pollIntervalSeconds"`
	MinSessionSeconds   float64 `json:"minSessionSeconds"`
	CaptureEnabled      bool    `json:"captureEnabled"`
	CaptureMaxBytes     int64   `json:"captureMaxBytes"`
	Port                int     `json:"port"`
	StartWithWindows    bool    `json:"startWithWindows"`
	Units               string  `json:"units"`
	Theme               string  `json:"theme"`
}

// Default returns the settings a fresh install starts with.
func Default() Config {
	return Config{
		PollIntervalSeconds: 1.0,
		MinSessionSeconds:   30,
		CaptureEnabled:      true,
		CaptureMaxBytes:     DefaultCaptureMaxBytes,
		Port:                DefaultPort,
		StartWithWindows:    true,
		Units:               "metric",
		Theme:               "system",
	}
}

// PollInterval returns PollIntervalSeconds as a duration.
func (c Config) PollInterval() time.Duration {
	return time.Duration(c.PollIntervalSeconds * float64(time.Second))
}

// MinSession returns MinSessionSeconds as a duration.
func (c Config) MinSession() time.Duration {
	return time.Duration(c.MinSessionSeconds * float64(time.Second))
}

// Validate reports whether every field holds a legal value.
func (c Config) Validate() error {
	if c.PollIntervalSeconds < MinPollSeconds || c.PollIntervalSeconds > MaxPollSeconds {
		return fmt.Errorf("config: pollIntervalSeconds %v outside [%v, %v]",
			c.PollIntervalSeconds, MinPollSeconds, MaxPollSeconds)
	}
	if c.MinSessionSeconds < 0 {
		return fmt.Errorf("config: minSessionSeconds %v is negative", c.MinSessionSeconds)
	}
	if c.CaptureMaxBytes < 0 {
		return fmt.Errorf("config: captureMaxBytes %d is negative (0 means unlimited)", c.CaptureMaxBytes)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: port %d outside [1, 65535]", c.Port)
	}
	switch c.Units {
	case "metric", "imperial":
	default:
		return fmt.Errorf("config: units %q must be metric or imperial", c.Units)
	}
	switch c.Theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("config: theme %q must be system, light or dark", c.Theme)
	}
	return nil
}

// Normalise clamps out-of-range values to the nearest legal value so a
// hand-edited file cannot prevent the application from starting.
func (c *Config) Normalise() {
	if c.PollIntervalSeconds < MinPollSeconds {
		c.PollIntervalSeconds = MinPollSeconds
	}
	if c.PollIntervalSeconds > MaxPollSeconds {
		c.PollIntervalSeconds = MaxPollSeconds
	}
	if c.MinSessionSeconds < 0 {
		c.MinSessionSeconds = 0
	}
	if c.CaptureMaxBytes < 0 {
		c.CaptureMaxBytes = 0
	}
	if c.Port < 1 || c.Port > 65535 {
		c.Port = DefaultPort
	}
	switch c.Units {
	case "metric", "imperial":
	default:
		c.Units = "metric"
	}
	switch c.Theme {
	case "system", "light", "dark":
	default:
		c.Theme = "system"
	}
}

// Load reads the config at path.
//
// A missing file returns Default with no error, because that is a first run
// rather than a fault. A file that exists but cannot be decoded IS an error:
// silently reverting a user's settings is worse than refusing to continue.
// Values that decode but are out of range are clamped.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	c := Default()
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	c.Normalise()
	return c, nil
}

// Save writes c to path atomically, so an interrupted write cannot leave a
// truncated config behind.
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create directory: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // a no-op once the rename succeeds

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("config: write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: rename into place: %w", err)
	}
	return nil
}
