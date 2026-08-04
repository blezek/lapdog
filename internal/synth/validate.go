package synth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/classify"
	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/sessionyaml"
)

// Validation reports what replaying a dataset back through the real decode,
// parse and classify path produced.
//
// This is the point of generating captures rather than a database: the fixtures
// only prove anything if they survive the same code the live simulator feeds.
type Validation struct {
	Files    int
	Frames   int
	YAMLDocs int

	// Segments counts distinct (SubSessionID, SessionNum) pairs seen.
	Segments int

	// Labels counts the UI label each segment classified to, which is the
	// clearest evidence that every event flavour round-trips.
	Labels map[string]int
	// AIDetections counts how AI presence was determined.
	AIDetections map[string]int
	// MissingVars names any required variable absent from a capture's layout.
	MissingVars map[string]int

	Problems []string
}

// requiredVars is the set the collector insists on. Duplicated here rather than
// imported so the validator can run before the collector package exists, and so
// a mismatch between the two surfaces as a validation failure rather than a
// compile error.
var requiredVars = []string{
	"SessionNum", "SessionState", "SessionTime", "SessionTimeRemain",
	"SessionLapsRemain", "IsOnTrack", "IsOnTrackCar", "IsInGarage",
	"IsReplayPlaying", "OnPitRoad", "Lap", "LapCurrentLapTime",
	"LapLastLapTime", "LapBestLapTime", "LapBestLap", "LapDist", "LapDistPct",
	"FuelLevel", "PlayerCarPosition", "PlayerCarClassPosition",
	"CarIdxTrackSurface", "CarIdxPosition", "CarIdxClassPosition",
	"CarIdxOnPitRoad", "CarIdxLap",
}

// Validate replays every capture in dir through the decode and classify path.
func Validate(dir string) (Validation, error) {
	v := Validation{
		Labels:       map[string]int{},
		AIDetections: map[string]int{},
		MissingVars:  map[string]int{},
	}

	paths, err := filepath.Glob(filepath.Join(dir, "*"+capture.Ext))
	if err != nil {
		return v, err
	}
	if len(paths) == 0 {
		return v, fmt.Errorf("synth: no capture files in %s", dir)
	}
	sort.Strings(paths)

	for _, path := range paths {
		if err := validateOne(path, &v); err != nil {
			v.Problems = append(v.Problems, fmt.Sprintf("%s: %v", filepath.Base(path), err))
		}
		v.Files++
	}
	return v, nil
}

// validateOne replays a single capture.
func validateOne(path string, v *Validation) error {
	r, err := capture.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	meta := r.Meta()
	layout := irsdk.NewRow(meta.VarHeaders, make([]byte, meta.BufLen))
	for _, name := range requiredVars {
		if !layout.Has(name) {
			v.MissingVars[name]++
		}
	}

	var info *sessionyaml.Info
	seen := map[string]bool{}

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		switch rec.Kind {
		case capture.KindSession:
			v.YAMLDocs++
			parsed, err := sessionyaml.Parse(rec.YAML)
			if err != nil {
				return fmt.Errorf("session YAML at t=%.1f did not parse: %w", rec.T, err)
			}
			info = parsed

		case capture.KindVars:
			v.Frames++
			if info == nil {
				return fmt.Errorf("variable frame at t=%.1f arrived before any session YAML", rec.T)
			}
			row := irsdk.NewRow(meta.VarHeaders, rec.Vars)
			num, ok := row.Int("SessionNum")
			if !ok {
				return fmt.Errorf("frame at t=%.1f has no SessionNum", rec.T)
			}

			key := fmt.Sprintf("%d/%d", info.WeekendInfo.SubSessionID, num)
			if seen[key] {
				continue
			}
			seen[key] = true
			v.Segments++

			res := classify.Classify(info, int(num))
			v.Labels[classify.Label(res.SessionType, res.EventContext)]++
			v.AIDetections[string(res.AIDetection)]++

			if res.SessionType == classify.TypeUnknown {
				return fmt.Errorf("session %d classified Unknown from raw type %q", num, res.RawSessionType)
			}
			if res.EventContext == classify.ContextUnknown {
				return fmt.Errorf("session %d produced an Unknown event context", num)
			}
		}
	}
	return nil
}

// ReportTo writes a human-readable validation summary.
func (v Validation) ReportTo(w io.Writer) {
	fmt.Fprintf(w, "\nReplayed %d captures through decode, parse and classify\n", v.Files)
	fmt.Fprintf(w, "  variable frames  %d\n", v.Frames)
	fmt.Fprintf(w, "  session YAML docs %d\n", v.YAMLDocs)
	fmt.Fprintf(w, "  session segments %d\n", v.Segments)

	fmt.Fprintln(w, "  classified as")
	for _, k := range sortedKeys(v.Labels) {
		fmt.Fprintf(w, "    %-22s %d\n", k, v.Labels[k])
	}
	fmt.Fprintln(w, "  AI detection path")
	for _, k := range sortedKeys(v.AIDetections) {
		fmt.Fprintf(w, "    %-22s %d\n", k, v.AIDetections[k])
	}

	if len(v.MissingVars) > 0 {
		fmt.Fprintln(w, "  MISSING required variables")
		for _, k := range sortedKeys(v.MissingVars) {
			fmt.Fprintf(w, "    %-22s in %d captures\n", k, v.MissingVars[k])
		}
	}
	if len(v.Problems) > 0 {
		fmt.Fprintf(w, "  PROBLEMS (%d)\n", len(v.Problems))
		for i, p := range v.Problems {
			if i >= 15 {
				fmt.Fprintf(w, "    ... and %d more\n", len(v.Problems)-15)
				break
			}
			fmt.Fprintf(w, "    %s\n", p)
		}
		return
	}
	fmt.Fprintln(w, "  no problems")
}

// OK reports whether validation found nothing wrong.
func (v Validation) OK() bool { return len(v.Problems) == 0 && len(v.MissingVars) == 0 }

// sortedKeys returns a map's keys in sorted order.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidateDirOrExit is a convenience for command-line use.
func ValidateDirOrExit(dir string, w io.Writer) bool {
	v, err := Validate(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "synth: validate: %v\n", err)
		return false
	}
	v.ReportTo(w)
	return v.OK()
}
