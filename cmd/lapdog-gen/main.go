// Command lapdog-gen writes a synthetic iRacing capture dataset.
//
// The output is the same .lpd capture format the application records from the
// simulator, so replaying it exercises the whole ingestion path rather than
// bypassing it. It is a development tool and is not shipped in releases.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/blezek/lapdog/internal/synth"
	"github.com/blezek/lapdog/internal/version"
)

func main() {
	dir := flag.String("dir", "testdata/dataset", "directory to write captures into")
	seed := flag.Int64("seed", 20260804, "random seed; the same seed reproduces the dataset exactly")
	weeks := flag.Int("weeks", synth.Weeks, "number of weeks of history to generate")
	endStr := flag.String("end", "", "last day of history as YYYY-MM-DD (default: today)")
	quiet := flag.Bool("quiet", false, "suppress per-file progress")
	fixtures := flag.Bool("fixtures", false, "write the small committed fixture set instead of a full dataset")
	validateOnly := flag.Bool("validate", false, "validate an existing dataset instead of generating one")
	skipValidate := flag.Bool("no-validate", false, "generate without replaying the result back")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.String())
		return
	}

	// Validating replays every capture back through the real decode, parse and
	// classify path. A dataset that cannot survive that is worthless as a
	// fixture, so it runs by default rather than on request.
	if *validateOnly {
		if !synth.ValidateDirOrExit(*dir, os.Stdout) {
			os.Exit(1)
		}
		return
	}

	// The fixture set is small, committed, and generated from a fixed date so
	// the files never churn. It is a separate mode rather than a week count
	// because its point is guaranteed coverage, not a span of history.
	if *fixtures {
		written, err := synth.WriteFixtures(*dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lapdog-gen: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Wrote %d fixtures to %s\n", len(written), *dir)
		for i, fc := range synth.FixtureCases {
			fi, err := os.Stat(written[i])
			size := int64(0)
			if err == nil {
				size = fi.Size()
			}
			fmt.Printf("  %-24s %6.1f KB  %s\n", fc.Name, float64(size)/1024, fc.Why)
		}
		if !*skipValidate {
			if !synth.ValidateDirOrExit(*dir, os.Stdout) {
				os.Exit(1)
			}
		}
		return
	}

	end := time.Now().UTC()
	if *endStr != "" {
		parsed, err := time.Parse("2006-01-02", *endStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "lapdog-gen: invalid -end: %v\n", err)
			os.Exit(2)
		}
		end = parsed
	}

	opts := synth.Options{Dir: *dir, End: end, Seed: *seed, Weeks: *weeks}
	if !*quiet {
		opts.Progress = func(done, total int, path string) {
			if done%25 == 0 || done == total {
				fmt.Printf("\r  %d/%d weekends", done, total)
			}
		}
	}

	start := time.Now()
	sum, err := synth.Generate(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nlapdog-gen: %v\n", err)
		os.Exit(1)
	}
	if !*quiet {
		fmt.Println()
	}
	report(sum, *dir, time.Since(start))

	if !*skipValidate {
		if !synth.ValidateDirOrExit(*dir, os.Stdout) {
			os.Exit(1)
		}
	}
}

// report prints a human-readable summary, including the weekly-hours band so a
// reviewer can confirm the dataset matches its stated shape.
func report(s synth.Summary, dir string, elapsed time.Duration) {
	fmt.Printf("\nWrote %d capture files to %s in %s\n", s.Files, dir, elapsed.Round(time.Millisecond))
	fmt.Printf("  span            %s .. %s\n",
		s.First.Format("2006-01-02"), s.Last.Format("2006-01-02"))
	fmt.Printf("  weekends        %d\n", s.Weekends)
	fmt.Printf("  session segments %d\n", s.Sessions)
	fmt.Printf("  laps            %d\n", s.Laps)
	fmt.Printf("  driving         %.1f h\n", s.DrivingHours)
	fmt.Printf("  connected       %.1f h\n", s.ConnectedHours)
	fmt.Printf("  on disk         %.1f MB\n", float64(s.Bytes)/(1<<20))

	// The first and last weeks are partial: the schedule is built from whole
	// weeks but clipped to the requested end date. Reporting them in the band
	// would understate the floor, so they are excluded and called out.
	if weeks := s.WeeklyHours; len(weeks) > 2 {
		interior := append([]float64(nil), weeks[1:len(weeks)-1]...)
		sort.Float64s(interior)
		n := len(interior)
		var total float64
		for _, v := range interior {
			total += v
		}
		fmt.Printf("  weekly driving  min %.1f  median %.1f  max %.1f  mean %.1f  (target 8-15, %d full weeks)\n",
			interior[0], interior[n/2], interior[n-1], total/float64(n), n)
		fmt.Printf("  partial weeks   %.1f at the start, %.1f at the end (clipped by the date range)\n",
			weeks[0], weeks[len(weeks)-1])
	}

	fmt.Println("  event mix")
	flavours := make([]string, 0, len(s.ByFlavour))
	for f := range s.ByFlavour {
		flavours = append(flavours, string(f))
	}
	sort.Strings(flavours)
	for _, f := range flavours {
		fmt.Printf("    %-18s %d\n", f, s.ByFlavour[synth.EventFlavour(f)])
	}
}
