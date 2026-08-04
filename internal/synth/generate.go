package synth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blezek/lapdog/internal/capture"
)

// Options configures dataset generation.
type Options struct {
	// Dir is where capture files are written. It is created if absent.
	Dir string
	// End is the last day of history. Defaults to today when zero.
	End time.Time
	// Seed makes the whole dataset deterministic. The same seed always produces
	// byte-identical captures, which is what allows golden tests.
	Seed int64
	// Weeks overrides the two-year span, for tests that want a small dataset.
	Weeks int
	// Progress, when set, is called after each weekend is written.
	Progress func(done, total int, path string)
}

// Summary reports what a generation run produced.
type Summary struct {
	Weekends       int
	Sessions       int
	Files          int
	Bytes          int64
	DrivingHours   float64
	ConnectedHours float64
	Laps           int
	First          time.Time
	Last           time.Time
	ByFlavour      map[EventFlavour]int
	WeeklyHours    []float64
}

// Generate writes the whole synthetic dataset and returns a summary.
func Generate(opts Options) (Summary, error) {
	if opts.Dir == "" {
		return Summary{}, fmt.Errorf("synth: Dir is required")
	}
	if opts.End.IsZero() {
		opts.End = time.Now().UTC()
	}
	if opts.Seed == 0 {
		opts.Seed = 20260804
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return Summary{}, fmt.Errorf("synth: create %s: %w", opts.Dir, err)
	}

	weeks := opts.Weeks
	if weeks <= 0 {
		weeks = Weeks
	}
	schedule := buildScheduleN(ScheduleOptions{End: opts.End, Seed: opts.Seed}, weeks)

	sum := Summary{ByFlavour: map[EventFlavour]int{}}
	for i, w := range schedule {
		path := filepath.Join(opts.Dir, captureName(w))
		// The weekend's own seed is derived from the run seed and the
		// subsession, so regenerating one weekend in isolation reproduces it.
		if err := SimulateWeekend(w, path, opts.Seed+int64(i)*7919); err != nil {
			return sum, fmt.Errorf("synth: weekend %d (%s): %w", i, path, err)
		}

		fi, err := os.Stat(path)
		if err != nil {
			return sum, err
		}
		sum.Files++
		sum.Bytes += fi.Size()
		sum.Weekends++
		sum.Sessions += len(w.Sessions)
		sum.ByFlavour[w.Flavour]++
		if sum.First.IsZero() || w.StartedAt.Before(sum.First) {
			sum.First = w.StartedAt
		}
		if w.StartedAt.After(sum.Last) {
			sum.Last = w.StartedAt
		}
		for j := range w.Sessions {
			s := &w.Sessions[j]
			sum.Laps += s.Laps
			drive := float64(s.Laps) * w.LapSeconds()
			sum.DrivingHours += drive / 3600
			sum.ConnectedHours += (drive + s.GarageSeconds + s.PitSeconds) / 3600
		}
		if opts.Progress != nil {
			opts.Progress(i+1, len(schedule), path)
		}
	}
	sum.WeeklyHours = weeklyDrivingHours(schedule)
	return sum, nil
}

// buildScheduleN builds a schedule of an arbitrary number of weeks, so tests can
// generate a handful rather than two years.
func buildScheduleN(opts ScheduleOptions, weeks int) []*Weekend {
	if weeks >= Weeks {
		return BuildSchedule(opts)
	}
	full := BuildSchedule(opts)
	// Keep the most recent weeks, which is the slice a UI would show first.
	cutoff := opts.End.AddDate(0, 0, -7*weeks)
	var out []*Weekend
	for _, w := range full {
		if w.StartedAt.After(cutoff) {
			out = append(out, w)
		}
	}
	return out
}

// weeklyDrivingHours totals driving time per ISO week, for the summary's
// sanity check that every week lands inside the intended band.
func weeklyDrivingHours(schedule []*Weekend) []float64 {
	byWeek := map[string]float64{}
	var keys []string
	for _, w := range schedule {
		y, wk := w.StartedAt.ISOWeek()
		key := fmt.Sprintf("%d-%02d", y, wk)
		if _, seen := byWeek[key]; !seen {
			keys = append(keys, key)
		}
		for i := range w.Sessions {
			byWeek[key] += float64(w.Sessions[i].Laps) * w.LapSeconds() / 3600
		}
	}
	out := make([]float64, 0, len(keys))
	for _, k := range keys {
		out = append(out, byWeek[k])
	}
	return out
}

// captureName builds a filename that sorts chronologically and identifies the
// weekend at a glance.
func captureName(w *Weekend) string {
	stamp := w.StartedAt.UTC().Format("20060102-150405")
	flav := strings.ReplaceAll(string(w.Flavour), "-", "")
	return fmt.Sprintf("%s-%s-%d%s", stamp, flav, w.SubSessionID, capture.Ext)
}
