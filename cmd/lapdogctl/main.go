// Command lapdogctl is a development tool for LapDog databases and captures.
//
// It is a separate binary from cmd/lapdog because that one is linked
// -H windowsgui and therefore has no console. lapdogctl is not shipped in
// releases.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/collector"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/version"
)

const usage = `lapdogctl — LapDog development tool

Usage:
  lapdogctl ingest <captures-dir> <lapdog.db>   replay captures into a database
  lapdogctl summary <lapdog.db>                 print what a database contains
  lapdogctl reclassify <lapdog.db>              re-derive classification from stored provenance
  lapdogctl version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "lapdogctl: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "ingest":
		if len(args) != 2 {
			return fmt.Errorf("ingest takes a captures directory and a database path")
		}
		return ingest(args[0], args[1])

	case "summary":
		if len(args) != 1 {
			return fmt.Errorf("summary takes a database path")
		}
		return summary(args[0])

	case "reclassify":
		if len(args) != 1 {
			return fmt.Errorf("reclassify takes a database path")
		}
		s, err := store.Open(args[0])
		if err != nil {
			return err
		}
		defer s.Close()
		n, err := s.Reclassify()
		if err != nil {
			return err
		}
		fmt.Printf("reclassified %d session(s)\n", n)
		return nil

	case "version":
		fmt.Println(version.String())
		return nil

	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// ingest replays every capture in dir through a real collector into dbPath.
//
// This is the same code path the live application uses, with the replay source
// substituted for the shared-memory reader. Nothing about ingestion is bypassed.
func ingest(dir, dbPath string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "*"+capture.Ext))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no capture files in %s", dir)
	}
	sort.Strings(paths)

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := time.Now()
	var failures int

	for i, path := range paths {
		src, err := source.NewReplay(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", filepath.Base(path), err)
			failures++
			continue
		}

		// The clock supplies each segment's started_at. Deriving it from the
		// capture's filename timestamp keeps an ingested dataset's dates matching
		// the history it represents, rather than stamping everything with now.
		//
		// The per-capture offset matters: an offline session's key is
		// "offline/<num>/<started_at>" at one-second resolution, because that is
		// all the live application ever needs. Bulk ingest processes many captures
		// within the same second, so without a distinct time per capture two
		// offline sessions would collide on that key and the second would silently
		// overwrite the first.
		started := timeFromName(filepath.Base(path)).Add(time.Duration(i) * time.Second)

		c, err := collector.New(collector.Options{
			Source:   src,
			Store:    st,
			Clock:    collector.NewFakeClock(started),
			Interval: time.Second,
			// Captures are already-recorded history, so nothing is discarded for
			// being short; that filter belongs to live recording.
			MinSession: 0,
			Logger:     log,
		})
		if err != nil {
			src.Close()
			return err
		}
		if err := c.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", filepath.Base(path), err)
			failures++
		}
		src.Close()

		if (i+1)%50 == 0 || i+1 == len(paths) {
			fmt.Printf("\r  %d/%d captures", i+1, len(paths))
		}
	}
	fmt.Printf("\n\nIngested %d captures in %s", len(paths)-failures, time.Since(start).Round(time.Millisecond))
	if failures > 0 {
		fmt.Printf(" (%d failed)", failures)
	}
	fmt.Println()
	return summaryOf(st)
}

// timeFromName recovers the start time a generated capture encodes in its
// filename, falling back to now when the name carries no timestamp.
func timeFromName(name string) time.Time {
	if len(name) >= 15 {
		if t, err := time.Parse("20060102-150405", name[:15]); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

func summary(dbPath string) error {
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()
	return summaryOf(s)
}

// summaryOf prints what a database contains, using the same aggregation queries
// the dashboard reads through.
func summaryOf(s *store.Store) error {
	all := store.Filter{}

	totals, err := s.Totals(all)
	if err != nil {
		return err
	}
	fmt.Printf("\nDatabase: %s\n", s.Path())
	fmt.Printf("  sessions        %d\n", totals.Sessions)
	fmt.Printf("  laps completed  %d\n", totals.Laps)
	fmt.Printf("  driving         %.1f h\n", totals.DrivingHours)
	fmt.Printf("  in car          %.1f h\n", totals.InCarHours)
	fmt.Printf("  connected       %.1f h\n", totals.ConnectedHours)
	fmt.Printf("  utilisation     %.0f%%\n", totals.Utilisation*100)
	fmt.Printf("  incidents       %d (%.2f per driving hour)\n", totals.Incidents, totals.IncidentsPerHour)

	human, err := s.Totals(store.Filter{ExcludeAI: true})
	if err != nil {
		return err
	}
	fmt.Printf("  passes made     %d\n", human.PassesMade)
	fmt.Printf("  times passed    %d   (human races only; AI excluded)\n", human.TimesPassed)

	rows, err := s.Summary(all, "typecontext")
	if err != nil {
		return err
	}
	fmt.Println("\n  driving hours by session type and context")
	for _, r := range rows {
		fmt.Printf("    %-34s %7.1f h  %4d sessions\n", r.Key, r.DrivingHours, r.Sessions)
	}

	daily, err := s.Daily(all)
	if err != nil {
		return err
	}
	if len(daily) > 0 {
		fmt.Printf("\n  active days     %d, from %s to %s\n",
			len(daily), daily[0].Day, daily[len(daily)-1].Day)
	}

	facets, err := s.Facets()
	if err != nil {
		return err
	}
	fmt.Printf("  distinct tracks %d\n", len(facets.Tracks))
	fmt.Printf("  distinct cars   %d\n", len(facets.Cars))
	fmt.Printf("  leagues         %d\n", len(facets.Leagues))
	return nil
}
