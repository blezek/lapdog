package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ErrNetworkPath indicates the data directory is on a network filesystem, where
// SQLite WAL is unsafe.
var ErrNetworkPath = errors.New("config: data directory is on a network path")

// DataDir returns the directory holding the database, config, log and captures.
//
// On Windows this is %LOCALAPPDATA%\lapdog, which is deliberate: LOCALAPPDATA is
// not synced by OneDrive, and SQLite WAL requires a real local filesystem.
func DataDir() (string, error) {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "lapdog"), nil
		}
	}
	if base := os.Getenv("XDG_DATA_HOME"); base != "" {
		return filepath.Join(base, "lapdog"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: locate home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "lapdog"), nil
}

// ConfigPath returns the settings file path within dir.
func ConfigPath(dir string) string { return filepath.Join(dir, "config.json") }

// DBPath returns the SQLite database path within dir.
func DBPath(dir string) string { return filepath.Join(dir, "lapdog.db") }

// CapturesDir returns the capture directory within dir.
func CapturesDir(dir string) string { return filepath.Join(dir, "captures") }

// LogPath returns the log file path within dir.
func LogPath(dir string) string { return filepath.Join(dir, "lapdog.log") }

// CheckLocalFilesystem reports whether dir is safe to hold a WAL-mode SQLite
// database.
//
// The WAL shared-memory file misbehaves on SMB shares and under file-sync tools,
// so a network path must be refused loudly rather than producing intermittent
// corruption much later.
func CheckLocalFilesystem(dir string) error {
	if strings.HasPrefix(dir, `\\`) || strings.HasPrefix(dir, "//") {
		return fmt.Errorf("%w: %s", ErrNetworkPath, dir)
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	if drive := filepath.VolumeName(dir); len(drive) == 2 && drive[1] == ':' {
		if remote, err := isRemoteDrive(drive); err == nil && remote {
			return fmt.Errorf("%w: %s is a mapped network drive", ErrNetworkPath, drive)
		}
	}
	return nil
}

// Store holds the live configuration and persists changes.
//
// It notifies subscribers on every accepted change, which is how a poll-interval
// adjustment takes effect without restarting the process.
type Store struct {
	mu   sync.RWMutex
	path string
	cur  Config
	subs []func(Config)
}

// NewStore loads the config at path, falling back to defaults when the file does
// not exist.
func NewStore(path string) (*Store, error) {
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, cur: c}, nil
}

// Get returns the current configuration.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Path returns the file the store persists to.
func (s *Store) Path() string { return s.path }

// Set validates, persists and applies a new configuration.
//
// An invalid configuration is rejected without changing anything and without
// notifying subscribers, so a bad update cannot leave the process half-applied.
func (s *Store) Set(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := Save(s.path, c); err != nil {
		return err
	}

	s.mu.Lock()
	s.cur = c
	// Copy the subscriber list so it can be walked outside the lock.
	subs := make([]func(Config), len(s.subs))
	copy(subs, s.subs)
	s.mu.Unlock()

	// Notify outside the lock so a subscriber may call Get without deadlocking.
	for _, fn := range subs {
		fn(c)
	}
	return nil
}

// OnChange registers a callback invoked after each accepted change.
func (s *Store) OnChange(fn func(Config)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, fn)
}
