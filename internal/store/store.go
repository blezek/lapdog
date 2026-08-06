// Package store persists sessions, laps and position events to SQLite.
//
// Concurrency model: SQLite in WAL mode permits one writer and many concurrent
// readers. This package therefore exposes two *sql.DB handles. The writer is
// capped at a single connection and is owned by the collector, which means two
// writes can never race and SQLITE_BUSY cannot occur by construction rather than
// being retried away. The reader is a pool used by the HTTP API; in WAL mode
// readers see the last committed snapshot and are never blocked by the writer.
package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// CurrentSchemaVersion is the schema this build understands.
const CurrentSchemaVersion = 2

// ErrSchemaTooNew indicates the database was written by a newer build. Downgrade
// is not supported, so this is refused rather than risked.
var ErrSchemaTooNew = errors.New("store: database schema is newer than this build")

// Store owns the database connections.
type Store struct {
	path   string
	writer *sql.DB
	reader *sql.DB
}

// dsn builds a connection string with the pragmas LapDog depends on.
//
// busy_timeout is set even though single-writer discipline should make it
// unnecessary: if it ever fires, that is a genuine anomaly worth surviving
// rather than crashing on.
func dsn(path string) string {
	return "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)"
}

// Open opens or creates the database at path and applies any pending migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("store: create directory: %w", err)
		}
	}

	writer, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("store: open writer: %w", err)
	}
	// Exactly one writer connection. This is the core of the concurrency model,
	// not a tuning knob.
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)

	if err := writer.Ping(); err != nil {
		writer.Close()
		return nil, fmt.Errorf("store: ping writer: %w", err)
	}

	s := &Store{path: path, writer: writer}
	if err := s.migrate(); err != nil {
		writer.Close()
		return nil, err
	}

	// The reader pool is opened after migration so it never observes a half-built
	// schema.
	reader, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		writer.Close()
		return nil, fmt.Errorf("store: open reader: %w", err)
	}
	reader.SetMaxOpenConns(4)
	reader.SetMaxIdleConns(4)
	if err := reader.Ping(); err != nil {
		writer.Close()
		reader.Close()
		return nil, fmt.Errorf("store: ping reader: %w", err)
	}
	s.reader = reader
	return s, nil
}

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// Writer returns the single-connection write handle.
func (s *Store) Writer() *sql.DB { return s.writer }

// Reader returns the pooled read handle.
func (s *Store) Reader() *sql.DB { return s.reader }

// Close releases both connection pools.
func (s *Store) Close() error {
	var firstErr error
	if s.reader != nil {
		if err := s.reader.Close(); err != nil {
			firstErr = err
		}
		s.reader = nil
	}
	if s.writer != nil {
		if err := s.writer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.writer = nil
	}
	return firstErr
}

// SchemaVersion returns the schema version recorded in the database, or 0 if the
// schema_version table does not exist yet.
func (s *Store) SchemaVersion() (int, error) {
	var name string
	err := s.writer.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='schema_version'`,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: probe schema_version: %w", err)
	}
	var v int
	if err := s.writer.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return v, nil
}

// migrate applies every migration newer than the recorded version, each inside
// its own transaction. Downgrade is not supported.
func (s *Store) migrate() error {
	have, err := s.SchemaVersion()
	if err != nil {
		return err
	}
	if have > CurrentSchemaVersion {
		return fmt.Errorf("%w: database is version %d, this build understands %d",
			ErrSchemaTooNew, have, CurrentSchemaVersion)
	}
	if have == CurrentSchemaVersion {
		return nil
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}
	for i, name := range names {
		version := i + 1
		if version <= have {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		tx, err := s.writer.Begin()
		if err != nil {
			return fmt.Errorf("store: begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		// 0001 seeds schema_version itself; later migrations update it.
		if version > 1 {
			if _, err := tx.Exec(`UPDATE schema_version SET version = ?`, version); err != nil {
				tx.Rollback()
				return fmt.Errorf("store: record version %d: %w", version, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", name, err)
		}
	}
	return nil
}

// migrationNames returns the embedded migration filenames in apply order.
func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("store: list migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
