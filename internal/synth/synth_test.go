package synth

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/classify"
	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/sessionyaml"
)

// The emulated layout is the contract with the collector. If a required
// variable goes missing the collector refuses the session, so a fixture that
// omits one is silently useless.
func TestLayoutCoversEveryRequiredVariable(t *testing.T) {
	vh, bufLen := Layout()
	row := irsdk.NewRow(vh, make([]byte, bufLen))
	for _, name := range requiredVars {
		if !row.Has(name) {
			t.Errorf("layout is missing required variable %q", name)
		}
	}
	// The optional live incident counter is what makes incident attribution
	// per-lap rather than per-session, so the fixtures should carry it.
	if !row.Has("PlayerCarMyIncidentCount") {
		t.Error("layout omits PlayerCarMyIncidentCount")
	}
}

func TestLayoutOffsetsDoNotOverlap(t *testing.T) {
	vh, bufLen := Layout()
	covered := make([]string, bufLen)
	for _, v := range vh {
		for i := v.Offset; i < v.Extent(); i++ {
			if i < 0 || int(i) >= len(covered) {
				t.Fatalf("variable %q extends past the %d byte row", v.Name, bufLen)
			}
			if prev := covered[i]; prev != "" {
				t.Fatalf("variables %q and %q both claim byte %d", prev, v.Name, i)
			}
			covered[i] = v.Name
		}
	}
	for i, owner := range covered {
		if owner == "" {
			t.Errorf("byte %d is not claimed by any variable; the layout has a hole", i)
		}
	}
}

// Every value written must decode back to what was written, or the fixtures
// would encode nonsense that only shows up much later.
func TestRowBuilderRoundTrip(t *testing.T) {
	vh, bufLen := Layout()
	rb := irsdk.NewRowBuilder(vh, bufLen)

	if err := rb.SetInt("Lap", 37); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetFloat("SessionTime", 1234.5); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetFloat("LapLastLapTime", 102.312); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetBool("IsOnTrackCar", true); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetIntAt("CarIdxTrackSurface", 7, int32(irsdk.InPitStall)); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetBoolAt("CarIdxOnPitRoad", 3, true); err != nil {
		t.Fatal(err)
	}

	row := irsdk.NewRow(vh, rb.Bytes())
	if v, ok := row.Int("Lap"); !ok || v != 37 {
		t.Errorf("Lap = %d, %v; want 37, true", v, ok)
	}
	// SessionTime is a double, so it must survive exactly.
	if v, ok := row.Float("SessionTime"); !ok || v != 1234.5 {
		t.Errorf("SessionTime = %v, %v; want 1234.5, true", v, ok)
	}
	// LapLastLapTime is a float32 in the layout, so allow narrowing loss.
	if v, ok := row.Float("LapLastLapTime"); !ok || v < 102.31 || v > 102.32 {
		t.Errorf("LapLastLapTime = %v, %v; want about 102.312", v, ok)
	}
	if v, ok := row.Bool("IsOnTrackCar"); !ok || !v {
		t.Errorf("IsOnTrackCar = %v, %v", v, ok)
	}
	surfaces, ok := row.IntArray("CarIdxTrackSurface")
	if !ok || irsdk.TrkLoc(surfaces[7]) != irsdk.InPitStall {
		t.Errorf("CarIdxTrackSurface[7] = %v, ok=%v", surfaces[7], ok)
	}
	pit, ok := row.BoolArray("CarIdxOnPitRoad")
	if !ok || !pit[3] || pit[4] {
		t.Errorf("CarIdxOnPitRoad = %v, ok=%v", pit[:5], ok)
	}
}

func TestRowBuilderRejectsUnknownVariable(t *testing.T) {
	vh, bufLen := Layout()
	rb := irsdk.NewRowBuilder(vh, bufLen)
	if err := rb.SetInt("NoSuchVariable", 1); err == nil {
		t.Error("SetInt on an unknown variable returned nil, want an error")
	}
}

// The driver never races on a Sunday. This is a stated property of the dataset,
// so it gets a test rather than a comment.
func TestScheduleNeverProducesSunday(t *testing.T) {
	schedule := BuildSchedule(ScheduleOptions{
		End:  time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
		Seed: 20260804,
	})
	if len(schedule) == 0 {
		t.Fatal("schedule is empty")
	}
	for _, w := range schedule {
		if w.StartedAt.Weekday() == time.Sunday {
			t.Errorf("weekend on a Sunday: %s (%s)", w.StartedAt.Format(time.RFC3339), w.Flavour)
		}
	}
}

func TestScheduleStaysWithinTheDateRange(t *testing.T) {
	end := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	limit := end.AddDate(0, 0, 1)
	for _, w := range BuildSchedule(ScheduleOptions{End: end, Seed: 7}) {
		if !w.StartedAt.Before(limit) {
			t.Errorf("weekend at %s is past the requested end %s",
				w.StartedAt.Format(time.RFC3339), end.Format("2006-01-02"))
		}
	}
}

// Weekly driving hours must land in the stated band. The first and last weeks
// are partial because the schedule is built from whole weeks and clipped, so
// they are excluded.
func TestScheduleWeeklyHoursInBand(t *testing.T) {
	schedule := BuildSchedule(ScheduleOptions{
		End:  time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
		Seed: 20260804,
	})
	weeks := weeklyDrivingHours(schedule)
	if len(weeks) < 50 {
		t.Fatalf("only %d weeks produced, want roughly %d", len(weeks), Weeks)
	}
	for i, h := range weeks {
		if i == 0 || i == len(weeks)-1 {
			continue // partial by construction
		}
		// A small tolerance is needed because hours are quantised to whole laps
		// and a lap at the Nordschleife is over eight minutes. It is kept tight
		// deliberately: a looser bound would stop catching allocation bugs, which
		// is exactly what let an earlier version undershoot the floor.
		if h < MinWeeklyHours-1.0 || h > MaxWeeklyHours+1.0 {
			t.Errorf("week %d has %.1f driving hours, outside the %.0f-%.0f band",
				i, h, MinWeeklyHours, MaxWeeklyHours)
		}
	}
}

// Every event flavour must appear across two years, or a classifier branch has
// no data behind it.
func TestScheduleCoversEveryFlavour(t *testing.T) {
	seen := map[EventFlavour]int{}
	for _, w := range BuildSchedule(ScheduleOptions{
		End:  time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
		Seed: 20260804,
	}) {
		seen[w.Flavour]++
	}
	for _, f := range []EventFlavour{
		FlavourOfficialRace, FlavourOfficialPractice, FlavourLeague,
		FlavourHosted, FlavourAI, FlavourOfflineTest, FlavourTimeTrial,
	} {
		if seen[f] == 0 {
			t.Errorf("flavour %q never appears in two years of schedule", f)
		}
	}
}

// The same seed must produce byte-identical captures, or golden tests built on
// this dataset would be unstable.
func TestGenerateIsDeterministic(t *testing.T) {
	end := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	runOnce := func() map[string][]byte {
		dir := t.TempDir()
		if _, err := Generate(Options{Dir: dir, End: end, Seed: 424242, Weeks: 2}); err != nil {
			t.Fatal(err)
		}
		files, err := filepath.Glob(filepath.Join(dir, "*"+capture.Ext))
		if err != nil {
			t.Fatal(err)
		}
		out := map[string][]byte{}
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			out[filepath.Base(f)] = b
		}
		return out
	}

	a, b := runOnce(), runOnce()
	if len(a) == 0 {
		t.Fatal("no captures written")
	}
	if len(a) != len(b) {
		t.Fatalf("run one wrote %d files, run two wrote %d", len(a), len(b))
	}
	for name, ba := range a {
		bb, ok := b[name]
		if !ok {
			t.Errorf("%s missing from the second run", name)
			continue
		}
		if len(ba) != len(bb) {
			t.Errorf("%s differs in length: %d vs %d", name, len(ba), len(bb))
			continue
		}
		for i := range ba {
			if ba[i] != bb[i] {
				t.Errorf("%s differs at byte %d", name, i)
				break
			}
		}
	}
}

// fixtureDir generates the fixture set into a temp directory. Tests use a fresh
// copy rather than the committed one so a stale commit cannot mask a break.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := WriteFixtures(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFixturesValidate(t *testing.T) {
	v, err := Validate(fixtureDir(t))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !v.OK() {
		t.Errorf("fixture set does not validate: problems=%v missing=%v", v.Problems, v.MissingVars)
	}
	if v.Files != len(FixtureCases) {
		t.Errorf("validated %d files, want %d", v.Files, len(FixtureCases))
	}
	if v.Frames == 0 || v.YAMLDocs == 0 {
		t.Errorf("frames=%d yamlDocs=%d, both must be non-zero", v.Frames, v.YAMLDocs)
	}
}

// The fixture set exists to give every classifier branch a real capture. This
// pins that, so deleting or breaking a fixture is a test failure rather than a
// silent loss of coverage.
func TestFixturesCoverEveryLabel(t *testing.T) {
	v, err := Validate(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Public Practice", "Race Practice", "Qualifying", "Race",
		"League Practice", "League Qualifying", "League Race",
		"Hosted Race", "AI Race", "Offline Testing", "Time Trial",
	} {
		if v.Labels[want] == 0 {
			t.Errorf("no fixture produces the label %q; got %v", want, v.Labels)
		}
	}
}

// Both AI detection paths must have a capture behind them, since the field name
// is unverified and the heuristic is the fallback that has to keep working.
func TestFixturesCoverBothAIDetectionPaths(t *testing.T) {
	v, err := Validate(fixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if v.AIDetections["field"] == 0 {
		t.Error("no fixture exercises AI detection via the CarIsAI field")
	}
	if v.AIDetections["heuristic"] == 0 {
		t.Error("no fixture exercises the AI detection heuristic fallback")
	}
}

// A race weekend must advance SessionNum within one capture, publish qualifying
// results only after qualifying, and publish finish results only after the race.
// This is the behaviour an .ibt file structurally cannot represent, and the whole
// reason captures carry the YAML repeatedly.
func TestRaceWeekendFixtureEvolvesResults(t *testing.T) {
	path := filepath.Join(fixtureDir(t), "official-race-weekend.lpd")
	r, err := capture.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var (
		sessionNums     = map[int32]bool{}
		firstQualDoc    = -1
		firstResultsDoc = -1
		docIndex        = 0
	)
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch rec.Kind {
		case capture.KindSession:
			info, err := sessionyaml.Parse(rec.YAML)
			if err != nil {
				t.Fatalf("document %d did not parse: %v", docIndex, err)
			}
			if firstQualDoc < 0 && len(info.QualifyResultsInfo.Results) > 0 {
				firstQualDoc = docIndex
			}
			if firstResultsDoc < 0 {
				if s, ok := info.SessionByNum(2); ok && len(s.ResultsPositions) > 0 {
					firstResultsDoc = docIndex
				}
			}
			docIndex++
		case capture.KindVars:
			row := irsdk.NewRow(r.Meta().VarHeaders, rec.Vars)
			if n, ok := row.Int("SessionNum"); ok {
				sessionNums[n] = true
			}
		}
	}

	if len(sessionNums) != 3 {
		t.Errorf("capture spans %d session numbers, want 3 (practice, qualify, race)", len(sessionNums))
	}
	if docIndex < 4 {
		t.Errorf("only %d session documents; the YAML must be republished as results appear", docIndex)
	}
	if firstQualDoc <= 0 {
		t.Error("qualifying results are present in the opening document; they must appear only after qualifying runs")
	}
	if firstResultsDoc <= firstQualDoc {
		t.Errorf("race results appeared at document %d, not after qualifying results at %d",
			firstResultsDoc, firstQualDoc)
	}
}

// The generator must produce real car and track display names, since the app
// reads them straight from the YAML rather than a lookup table.
func TestFixtureCarriesDisplayNames(t *testing.T) {
	path := filepath.Join(fixtureDir(t), "official-race-weekend.lpd")
	r, err := capture.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			t.Fatal("no session document found")
		}
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind != capture.KindSession {
			continue
		}
		info, err := sessionyaml.Parse(rec.YAML)
		if err != nil {
			t.Fatal(err)
		}
		me, ok := info.Me()
		if !ok {
			t.Fatal("the local driver is not in the driver list")
		}
		if me.CarScreenName == "" || me.CarClassShortName == "" || me.CarID == 0 {
			t.Errorf("car identity incomplete: %+v", me)
		}
		if info.WeekendInfo.TrackDisplayName == "" || info.WeekendInfo.TrackID == 0 {
			t.Errorf("track identity incomplete: %+v", info.WeekendInfo)
		}
		if km := info.TrackLengthKm(); km <= 0 {
			t.Errorf("TrackLengthKm = %v, want a positive length parsed from %q",
				km, info.WeekendInfo.TrackLength)
		}
		return
	}
}

// Replay frames must be present in the dataset, because "never count replay
// time" is only meaningfully tested if some fixture actually contains some.
func TestDatasetContainsReplayFrames(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generate(Options{
		Dir:   dir,
		End:   time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC),
		Seed:  20260804,
		Weeks: 3,
	}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"+capture.Ext))

	replayFrames := 0
	for _, f := range files {
		r, err := capture.OpenReader(f)
		if err != nil {
			t.Fatal(err)
		}
		for {
			rec, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				r.Close()
				t.Fatal(err)
			}
			if rec.Kind != capture.KindVars {
				continue
			}
			row := irsdk.NewRow(r.Meta().VarHeaders, rec.Vars)
			if v, ok := row.Bool("IsReplayPlaying"); ok && v {
				replayFrames++
			}
		}
		r.Close()
	}
	if replayFrames == 0 {
		t.Error("no replay frames in the dataset; the exclusion rule would go untested")
	}
}

// Pit-stall frames must be present, because that is the only thing separating
// in-car time from driving time.
func TestDatasetContainsPitStallFrames(t *testing.T) {
	dir := fixtureDir(t)
	files, _ := filepath.Glob(filepath.Join(dir, "*"+capture.Ext))

	pitStall := 0
	for _, f := range files {
		r, err := capture.OpenReader(f)
		if err != nil {
			t.Fatal(err)
		}
		for {
			rec, err := r.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				r.Close()
				t.Fatal(err)
			}
			if rec.Kind != capture.KindVars {
				continue
			}
			row := irsdk.NewRow(r.Meta().VarHeaders, rec.Vars)
			surfaces, ok := row.IntArray("CarIdxTrackSurface")
			if !ok {
				continue
			}
			inCar, _ := row.Bool("IsOnTrackCar")
			if inCar && irsdk.TrkLoc(surfaces[0]) == irsdk.InPitStall {
				pitStall++
			}
		}
		r.Close()
	}
	if pitStall == 0 {
		t.Error("no in-car pit-stall frames; in-car time and driving time would be indistinguishable")
	}
}

// The short-session fixture exists so the minimum-length rule can be tested.
func TestShortSessionFixtureIsActuallyShort(t *testing.T) {
	path := filepath.Join(fixtureDir(t), "short-session.lpd")
	r, err := capture.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	frames := 0
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == capture.KindVars {
			frames++
		}
	}
	// At one frame per second, a session the collector should discard has to be
	// well under the 30 second default minimum of driving.
	if frames > 400 {
		t.Errorf("short-session has %d frames, which is not short enough to exercise the minimum-length rule", frames)
	}
}

// Classification must be stable for a given fixture, so a change in the rules
// shows up as a test failure rather than silently reshaping the dataset.
func TestFixtureClassificationIsStable(t *testing.T) {
	cases := []struct {
		file       string
		sessionNum int
		wantLabel  string
		wantAI     classify.AIDetection
	}{
		{"official-race-weekend.lpd", 0, "Race Practice", classify.AIDetectNone},
		{"official-race-weekend.lpd", 1, "Qualifying", classify.AIDetectNone},
		{"official-race-weekend.lpd", 2, "Race", classify.AIDetectNone},
		{"public-practice.lpd", 0, "Public Practice", classify.AIDetectNone},
		{"league-race-weekend.lpd", 2, "League Race", classify.AIDetectNone},
		{"hosted-race.lpd", 1, "Hosted Race", classify.AIDetectNone},
		{"ai-race-field-present.lpd", 1, "AI Race", classify.AIDetectField},
		{"ai-race-field-absent.lpd", 1, "AI Race", classify.AIDetectHeuristic},
		{"offline-test-drive.lpd", 0, "Offline Testing", classify.AIDetectNone},
		{"time-trial.lpd", 0, "Time Trial", classify.AIDetectNone},
	}

	dir := fixtureDir(t)
	for _, c := range cases {
		t.Run(c.file+"/"+itoa(c.sessionNum), func(t *testing.T) {
			info := lastYAML(t, filepath.Join(dir, c.file))
			res := classify.Classify(info, c.sessionNum)
			if got := classify.Label(res.SessionType, res.EventContext); got != c.wantLabel {
				t.Errorf("label = %q, want %q (type=%q context=%q raw=%q)",
					got, c.wantLabel, res.SessionType, res.EventContext, res.RawSessionType)
			}
			if res.AIDetection != c.wantAI {
				t.Errorf("AIDetection = %q, want %q", res.AIDetection, c.wantAI)
			}
		})
	}
}

// lastYAML returns the final session document in a capture, which is the one
// carrying complete results.
func lastYAML(t *testing.T, path string) *sessionyaml.Info {
	t.Helper()
	r, err := capture.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	var last *sessionyaml.Info
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind != capture.KindSession {
			continue
		}
		info, err := sessionyaml.Parse(rec.YAML)
		if err != nil {
			t.Fatal(err)
		}
		last = info
	}
	if last == nil {
		t.Fatalf("%s contains no session documents", path)
	}
	return last
}

// itoa avoids importing strconv for a single test-name conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
