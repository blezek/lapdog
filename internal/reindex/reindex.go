// Package reindex replays saved telemetry captures through the collector.
package reindex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
)

// ErrNoCaptures reports that the configured capture directory has nothing to replay.
var ErrNoCaptures = errors.New("reindex: no saved capture files")

// Progress is a point-in-time count for a capture reindex run.
type Progress struct {
	Total     int `json:"total"`
	Processed int `json:"processed"`
	Replayed  int `json:"replayed"`
	Failed    int `json:"failed"`
	Segments  int `json:"segments"`
}

// Failure records one capture that could not be replayed. File is only the base
// name: the API already reports the capture directory separately and should not
// repeat an absolute user path in every diagnostic.
type Failure struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// Result is the final outcome of replaying a discovered set of captures.
type Result struct {
	Progress
	Failures []Failure `json:"failures,omitempty"`
}

// Options controls observability of a reindex run.
type Options struct {
	Logger     *slog.Logger
	OnProgress func(Progress)
}

// Discover returns saved captures in deterministic filename order.
func Discover(dir string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*"+capture.Ext))
	if err != nil {
		return nil, fmt.Errorf("reindex: list captures: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNoCaptures, dir)
	}
	sort.Strings(paths)
	return paths, nil
}

// Run replays paths through the same collector that records live telemetry.
// Existing session keys are upserted, so replaying a capture repairs its derived
// records without creating a duplicate session.
func Run(ctx context.Context, paths []string, st *store.Store, opts Options) (Result, error) {
	if st == nil {
		return Result{}, errors.New("reindex: store is required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	result := Result{Progress: Progress{Total: len(paths)}}
	notify := func() {
		if opts.OnProgress != nil {
			opts.OnProgress(result.Progress)
		}
	}
	notify()

	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sessions, failure := replayOne(ctx, path, i, st, log)
		result.Processed++
		if failure != nil {
			result.Failed++
			message := strings.ReplaceAll(failure.Error(), path, filepath.Base(path))
			result.Failures = append(result.Failures, Failure{
				File: filepath.Base(path), Error: message,
			})
		} else {
			result.Replayed++
			result.Segments += sessions
		}
		notify()
	}
	return result, nil
}

func replayOne(ctx context.Context, path string, offset int, st *store.Store, log *slog.Logger) (int, error) {
	src, err := source.NewReplay(path)
	if err != nil {
		return 0, err
	}
	defer src.Close()

	started := TimeFromName(filepath.Base(path), time.Now().UTC()).Add(time.Duration(offset) * time.Second)
	c, err := collector.New(collector.Options{
		Source:            src,
		Store:             st,
		Clock:             collector.NewFakeClock(started),
		Interval:          time.Second,
		MinSession:        0,
		ReplayCaptureFile: filepath.Base(path),
		StopOnError:       true,
		Logger:            log,
	})
	if err != nil {
		return 0, err
	}
	if err := c.Run(ctx); err != nil {
		return c.Status().SessionsRecorded, err
	}
	return c.Status().SessionsRecorded, nil
}

// TimeFromName recovers the UTC start encoded by live and generated captures.
// fallback is explicit so callers and tests never depend on a hidden wall clock.
func TimeFromName(name string, fallback time.Time) time.Time {
	for _, layout := range []struct {
		format string
		length int
	}{
		{"20060102T150405Z", 16}, // live capture: 20260812T014837Z
		{"20060102-150405", 15},  // generated capture
	} {
		if len(name) < layout.length {
			continue
		}
		if t, err := time.Parse(layout.format, name[:layout.length]); err == nil {
			return t.UTC()
		}
	}
	return fallback.UTC()
}
