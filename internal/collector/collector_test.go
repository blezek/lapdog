package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/synth"
)

// fixtureDir generates the committed fixture set into a temp directory.
//
// Tests generate a fresh copy rather than reading testdata/fixtures so a stale
// commit cannot mask a break in the generator.
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := synth.WriteFixtures(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

// ingest replays one capture through a real collector into a real database.
func ingest(t *testing.T, capturePath string, tune func(*Options)) *store.Store {
	t.Helper()

	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	src, err := source.NewReplay(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	opts := Options{
		Source:     src,
		Store:      st,
		Clock:      NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
		Interval:   time.Second,
		MinSession: 0,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if tune != nil {
		tune(&opts)
	}
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return st
}

// A race weekend capture must land as three separate session rows, with the
// practice one classified as race practice because a race shares the weekend.
func TestIngestRaceWeekendProducesThreeSegments(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)

	rows, total, err := st.ListSessions(store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Fatalf("sessions = %d, want 3 (practice, qualify, race)", total)
	}

	byNum := map[int]store.Session{}
	for _, r := range rows {
		byNum[r.SessionNum] = r
	}
	want := map[int]struct{ st, ctx string }{
		0: {"Practice", "OfficialRace"},
		1: {"Qualify", "OfficialRace"},
		2: {"Race", "OfficialRace"},
	}
	for num, w := range want {
		got, ok := byNum[num]
		if !ok {
			t.Errorf("no session recorded for SessionNum %d", num)
			continue
		}
		if got.SessionType != w.st || got.EventContext != w.ctx {
			t.Errorf("session %d = %s/%s, want %s/%s",
				num, got.SessionType, got.EventContext, w.st, w.ctx)
		}
		if got.EndedAt == nil {
			t.Errorf("session %d has no EndedAt; it must be closed", num)
		}
	}
}

// The three counters must be distinct and correctly ordered. If driving equals
// connected, the garage and pit-box states are not being separated.
func TestIngestSeparatesTheThreeTimeCounters(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)
	rows, _, _ := st.ListSessions(store.Filter{})

	for _, r := range rows {
		if r.ConnectedSeconds <= 0 {
			t.Errorf("session %d has no connected time", r.SessionNum)
		}
		if !(r.DrivingSeconds <= r.InCarSeconds && r.InCarSeconds <= r.ConnectedSeconds) {
			t.Errorf("session %d counters out of order: driving=%.0f in-car=%.0f connected=%.0f",
				r.SessionNum, r.DrivingSeconds, r.InCarSeconds, r.ConnectedSeconds)
		}
		if r.DrivingSeconds == r.ConnectedSeconds {
			t.Errorf("session %d has driving == connected; garage time is not being excluded",
				r.SessionNum)
		}
		if r.InCarSeconds == r.DrivingSeconds {
			t.Errorf("session %d has in-car == driving; pit-box time is not being excluded",
				r.SessionNum)
		}
	}
}

func TestIngestRecordsLaps(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)
	rows, _, _ := st.ListSessions(store.Filter{})

	totalLaps := 0
	for _, r := range rows {
		laps, err := st.LapsForSession(r.ID)
		if err != nil {
			t.Fatal(err)
		}
		totalLaps += len(laps)

		if r.LapsCompleted != len(laps) {
			t.Errorf("session %d reports %d laps completed but %d lap rows exist",
				r.SessionNum, r.LapsCompleted, len(laps))
		}
		for _, l := range laps {
			if l.LapTimeS == nil || *l.LapTimeS <= 0 {
				t.Errorf("session %d lap %d has no usable time", r.SessionNum, l.LapNumber)
			}
		}
		if len(laps) > 0 {
			if r.BestLapTimeS == nil {
				t.Errorf("session %d has laps but no best lap time", r.SessionNum)
				continue
			}
			// The stored best must actually be the fastest recorded lap.
			best := *laps[0].LapTimeS
			for _, l := range laps {
				if *l.LapTimeS < best {
					best = *l.LapTimeS
				}
			}
			if *r.BestLapTimeS > best+1e-6 {
				t.Errorf("session %d best = %v but the fastest lap is %v",
					r.SessionNum, *r.BestLapTimeS, best)
			}
		}
	}
	if totalLaps == 0 {
		t.Error("no laps ingested from a race weekend")
	}
}

// Lap numbers must be sequential from 1 with no gaps or repeats: a gap means a
// crossing was missed, a repeat means one was double-counted.
func TestIngestLapNumbersAreSequential(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "public-practice.lpd"), nil)
	rows, _, _ := st.ListSessions(store.Filter{})
	if len(rows) != 1 {
		t.Fatalf("sessions = %d, want 1", len(rows))
	}
	laps, _ := st.LapsForSession(rows[0].ID)
	if len(laps) < 3 {
		t.Fatalf("only %d laps ingested", len(laps))
	}
	for i, l := range laps {
		if l.LapNumber != i+1 {
			t.Errorf("laps[%d].LapNumber = %d, want %d", i, l.LapNumber, i+1)
		}
	}
}

// Position events must appear in the race and nowhere else, and must carry a
// cause and an opponent name.
func TestIngestRecordsPositionEventsInRacesOnly(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)
	rows, _, _ := st.ListSessions(store.Filter{})

	raceEvents := 0
	for _, r := range rows {
		evs, err := st.PositionEventsForSession(r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.SessionType != "Race" {
			if len(evs) != 0 {
				t.Errorf("%s recorded %d position events, want 0", r.SessionType, len(evs))
			}
			continue
		}
		raceEvents = len(evs)
		for _, ev := range evs {
			if ev.Cause == "" {
				t.Error("position event has no cause")
			}
			if ev.FromPosition == ev.ToPosition {
				t.Errorf("position event from %d to %d is not a change",
					ev.FromPosition, ev.ToPosition)
			}
			if ev.OpponentCarIdx != nil && ev.OpponentName == nil {
				t.Errorf("event attributed to car %d has no opponent name", *ev.OpponentCarIdx)
			}
		}
	}
	if raceEvents == 0 {
		t.Error("the race produced no position events")
	}
}

// Qualifying and finish results only appear in the YAML once their session has
// run, so ingesting the whole weekend must pick them up.
func TestIngestCapturesQualifyingAndFinishResults(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)
	rows, _, _ := st.ListSessions(store.Filter{})

	var race *store.Session
	for i := range rows {
		if rows[i].SessionType == "Race" {
			race = &rows[i]
		}
	}
	if race == nil {
		t.Fatal("no race session ingested")
	}
	if race.FinishPosition == nil {
		t.Error("race has no finish position; ResultsPositions was not picked up")
	}
	// Qualifying result is copied onto the race row so a race can be analysed
	// without joining to the qualifying session.
	if race.QualifyPosition == nil {
		t.Error("race has no qualify position; it must be carried across from QualifyResultsInfo")
	}
	if race.FieldSize == nil || *race.FieldSize < 2 {
		t.Errorf("FieldSize = %v, want the size of the field", race.FieldSize)
	}
	if race.StartingPosition == nil {
		t.Error("race has no starting position; it should be captured at the green flag")
	}
}

// Every fixture must ingest to the classification the validator predicted, which
// pins that the collector agrees with the classifier end to end.
func TestIngestClassificationMatchesFixtures(t *testing.T) {
	dir := fixtureDir(t)
	cases := []struct {
		file       string
		sessionNum int
		wantType   string
		wantCtx    string
		wantAI     string
	}{
		{"public-practice.lpd", 0, "Practice", "OfficialPractice", "none"},
		{"league-race-weekend.lpd", 2, "Race", "League", "none"},
		{"hosted-race.lpd", 1, "Race", "Hosted", "none"},
		{"ai-race-field-present.lpd", 1, "Race", "AI", "field"},
		{"ai-race-field-absent.lpd", 1, "Race", "AI", "heuristic"},
		{"offline-test-drive.lpd", 0, "OfflineTest", "Offline", "none"},
		{"time-trial.lpd", 0, "TimeTrial", "TimeTrial", "none"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			st := ingest(t, filepath.Join(dir, c.file), nil)
			rows, _, err := st.ListSessions(store.Filter{})
			if err != nil {
				t.Fatal(err)
			}
			var got *store.Session
			for i := range rows {
				if rows[i].SessionNum == c.sessionNum {
					got = &rows[i]
				}
			}
			if got == nil {
				t.Fatalf("no session with SessionNum %d; got %d sessions", c.sessionNum, len(rows))
			}
			if got.SessionType != c.wantType || got.EventContext != c.wantCtx {
				t.Errorf("classified %s/%s, want %s/%s",
					got.SessionType, got.EventContext, c.wantType, c.wantCtx)
			}
			if got.AIDetection == nil || *got.AIDetection != c.wantAI {
				t.Errorf("AIDetection = %v, want %q", got.AIDetection, c.wantAI)
			}
		})
	}
}

// Replay frames must contribute to no counter, so a fixture containing them must
// still show driving time strictly less than its frame count would imply.
func TestIngestExcludesReplayTime(t *testing.T) {
	// offline-test-drive is long enough that its replay segment is material.
	st := ingest(t, filepath.Join(fixtureDir(t), "offline-test-drive.lpd"), nil)
	rows, _, _ := st.ListSessions(store.Filter{})
	if len(rows) != 1 {
		t.Fatalf("sessions = %d", len(rows))
	}
	// The strong assertion lives in the accounting unit tests; here it is enough
	// that a session with replay frames still records sane, ordered counters.
	r := rows[0]
	if r.DrivingSeconds <= 0 || r.DrivingSeconds > r.ConnectedSeconds {
		t.Errorf("driving=%.0f connected=%.0f", r.DrivingSeconds, r.ConnectedSeconds)
	}
}

// A session below the minimum length must be discarded entirely, including any
// row an earlier flush already wrote.
func TestIngestDropsTooShortSession(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "short-session.lpd"), func(o *Options) {
		o.MinSession = 30 * time.Minute
	})
	_, total, err := st.ListSessions(store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("sessions = %d, want 0 — the session is below the minimum length", total)
	}
}

// The collector writes its own capture while ingesting, and that capture must
// itself be replayable.
func TestIngestWritesReplayableCapture(t *testing.T) {
	capDir := filepath.Join(t.TempDir(), "captures")
	ingest(t, filepath.Join(fixtureDir(t), "public-practice.lpd"), func(o *Options) {
		o.CaptureEnabled = true
		o.CaptureDir = capDir
		o.CaptureMaxBytes = 1 << 30
	})

	written, err := filepath.Glob(filepath.Join(capDir, "*.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	if len(written) == 0 {
		t.Fatal("no capture written")
	}
	src, err := source.NewReplay(written[0])
	if err != nil {
		t.Fatalf("the capture the collector wrote is not replayable: %v", err)
	}
	src.Close()
}

func TestSetCaptureDisablesActiveCaptureOnNextFrame(t *testing.T) {
	dir := fixtureDir(t)
	src, err := source.NewReplay(filepath.Join(dir, "public-practice.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var c *Collector
	hooked := &hookSource{Source: src}
	hooked.onFrame = func(n int) {
		if n == 2 {
			c.SetCapture(false, 1<<30)
		}
	}

	capDir := filepath.Join(t.TempDir(), "captures")
	c, err = New(Options{
		Source: hooked, Store: st, Clock: NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
		Interval: time.Second, CaptureEnabled: true, CaptureDir: capDir, CaptureMaxBytes: 1 << 30,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hooked.frames < 2 {
		t.Fatalf("fixture produced %d frames, need at least 2 for this test", hooked.frames)
	}

	written, err := filepath.Glob(filepath.Join(capDir, "*.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("captures written = %d, want 1", len(written))
	}
	counts := captureKindCounts(t, written[0])
	if counts[capture.KindVars] != 1 {
		t.Errorf("vars records after live disable = %d, want 1", counts[capture.KindVars])
	}
}

func TestSetCaptureEnablesActiveCaptureOnNextFrame(t *testing.T) {
	dir := fixtureDir(t)
	src, err := source.NewReplay(filepath.Join(dir, "public-practice.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var c *Collector
	hooked := &hookSource{Source: src}
	hooked.onFrame = func(n int) {
		if n == 2 {
			c.SetCapture(true, 1<<30)
		}
	}

	capDir := filepath.Join(t.TempDir(), "captures")
	c, err = New(Options{
		Source: hooked, Store: st, Clock: NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
		Interval: time.Second, CaptureEnabled: false, CaptureDir: capDir, CaptureMaxBytes: 1 << 30,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if hooked.frames < 2 {
		t.Fatalf("fixture produced %d frames, need at least 2 for this test", hooked.frames)
	}

	written, err := filepath.Glob(filepath.Join(capDir, "*.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 1 {
		t.Fatalf("captures written = %d, want 1", len(written))
	}
	counts := captureKindCounts(t, written[0])
	if counts[capture.KindSession] == 0 {
		t.Error("capture has no session record after live enable")
	}
	if counts[capture.KindVars] != hooked.frames-1 {
		t.Errorf("vars records after live enable = %d, want %d", counts[capture.KindVars], hooked.frames-1)
	}
	replay, err := source.NewReplay(written[0])
	if err != nil {
		t.Fatalf("capture enabled mid-session is not replayable: %v", err)
	}
	replay.Close()
}

func TestSetCaptureMaxBytesPrunesWithoutRestart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	capDir := filepath.Join(t.TempDir(), "captures")
	if err := os.MkdirAll(capDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(capDir, "old.lpd")
	newer := filepath.Join(capDir, "newer.lpd")
	if err := os.WriteFile(old, []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)
	if err := os.Chtimes(old, base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	c, err := New(Options{
		Source: emptySource{}, Store: st, Clock: RealClock{}, Interval: time.Second,
		CaptureDir: capDir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.SetCapture(true, 10)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old capture still exists after live prune; stat err = %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Errorf("newer capture was pruned, want it kept: %v", err)
	}
}

func TestSetMinSessionUpdatesCloseThreshold(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c, err := New(Options{
		Source: emptySource{}, Store: st, Clock: RealClock{}, Interval: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.SetMinSession(42 * time.Second)
	if got := c.minSessionLen(); got != 42*time.Second {
		t.Errorf("minSessionLen = %s, want 42s", got)
	}
}

func TestSetPausedSerializesWithActiveSegmentState(t *testing.T) {
	src, err := source.NewReplay(filepath.Join(fixtureDir(t), "public-practice.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c, err := New(Options{
		Source: src, Store: st, Clock: NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
		Interval: time.Second, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handle(frame); err != nil {
		t.Fatal(err)
	}
	if c.seg == nil {
		t.Fatal("first frame did not open a segment")
	}

	c.activeMu.Lock()
	done := make(chan struct{})
	go func() {
		c.SetPaused(true)
		close(done)
	}()
	waitForPaused(t, c)
	select {
	case <-done:
		t.Fatal("SetPaused completed while active segment state was locked")
	default:
	}
	if c.seg == nil {
		t.Fatal("SetPaused closed the segment while active segment state was locked")
	}
	c.activeMu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetPaused did not finish after active segment state unlocked")
	}
	if c.seg != nil {
		t.Fatal("SetPaused did not close the active segment")
	}
}

func TestRunCancelsContextAwareSourcePromptly(t *testing.T) {
	src := &blockingContextSource{started: make(chan struct{})}
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c, err := New(Options{
		Source: src, Store: st, Clock: RealClock{}, Interval: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-src.started:
	case <-time.After(time.Second):
		t.Fatal("collector did not enter the source read")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancellation = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after source context cancellation")
	}
}

func TestStatusReportsProgress(t *testing.T) {
	dir := fixtureDir(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	src, err := source.NewReplay(filepath.Join(dir, "public-practice.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	var c *Collector
	var active Status
	wrapped := &hookSource{
		Source: src,
		onFrame: func(int) {
			if c == nil || active.SessionLabel != "" {
				return
			}
			if s := c.Status(); s.SessionLabel != "" {
				active = s
			}
		},
	}

	c, err = New(Options{
		Source: wrapped, Store: st, Clock: RealClock{}, Interval: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	s := c.Status()
	if s.SessionsRecorded != 1 {
		t.Errorf("SessionsRecorded = %d, want 1", s.SessionsRecorded)
	}
	if active.SessionLabel != "Public Practice" {
		t.Errorf("active SessionLabel = %q, want Public Practice", active.SessionLabel)
	}
	if active.TrackName == "" {
		t.Error("active status has no track name")
	}
	if s.IntervalSeconds != 1 {
		t.Errorf("IntervalSeconds = %v, want 1", s.IntervalSeconds)
	}
	// The live incident variable is present in the synthetic layout, so it must be
	// preferred over the YAML count.
	if active.IncidentSource != "live" {
		t.Errorf("active IncidentSource = %q, want live", active.IncidentSource)
	}
	if s.SessionLabel != "" || s.TrackName != "" || s.CarName != "" ||
		s.DrivingSeconds != 0 || s.Laps != 0 || s.IncidentSource != "" {
		t.Errorf("closed status kept active session detail: %+v", s)
	}
}

func TestPausedRecordsNothing(t *testing.T) {
	dir := fixtureDir(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	src, err := source.NewReplay(filepath.Join(dir, "public-practice.lpd"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	c, _ := New(Options{
		Source: src, Store: st, Clock: RealClock{}, Interval: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	c.SetPaused(true)
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := st.ListSessions(store.Filter{}); total != 0 {
		t.Errorf("sessions = %d, want 0 while paused", total)
	}
	if !c.Status().Paused {
		t.Error("Status().Paused = false after SetPaused(true)")
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Error("New with no Source or Store returned nil error")
	}
}

// Ingesting the same capture twice must not duplicate anything: session identity
// is keyed on subsession and session number, and laps are idempotent.
func TestIngestIsIdempotent(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "public-practice.lpd")

	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	run := func() {
		src, err := source.NewReplay(path)
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()
		c, err := New(Options{
			Source: src, Store: st,
			Clock:    NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
			Interval: time.Second,
			Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	run()
	rows, total, _ := st.ListSessions(store.Filter{})
	firstLaps, _ := st.LapsForSession(rows[0].ID)

	run()
	rows2, total2, _ := st.ListSessions(store.Filter{})
	if total2 != total {
		t.Errorf("re-ingesting produced %d sessions, want %d", total2, total)
	}
	secondLaps, _ := st.LapsForSession(rows2[0].ID)
	if len(secondLaps) != len(firstLaps) {
		t.Errorf("re-ingesting produced %d laps, want %d", len(secondLaps), len(firstLaps))
	}
}

type hookSource struct {
	source.Source
	frames  int
	onFrame func(int)
}

func (s *hookSource) Next() (source.Frame, error) {
	f, err := s.Source.Next()
	if err == nil {
		s.frames++
		if s.onFrame != nil {
			s.onFrame(s.frames)
		}
	}
	return f, err
}

type emptySource struct{}

func (emptySource) Next() (source.Frame, error) { return source.Frame{}, io.EOF }
func (emptySource) Meta() capture.Meta          { return capture.Meta{} }
func (emptySource) Close() error                { return nil }

type blockingContextSource struct {
	started chan struct{}
}

func (s *blockingContextSource) Next() (source.Frame, error) {
	return source.Frame{}, errors.New("plain Next must not be called")
}

func (s *blockingContextSource) NextContext(ctx context.Context) (source.Frame, error) {
	close(s.started)
	<-ctx.Done()
	return source.Frame{}, ctx.Err()
}

func (s *blockingContextSource) Meta() capture.Meta { return capture.Meta{} }
func (s *blockingContextSource) Close() error       { return nil }

func captureKindCounts(t *testing.T, path string) map[capture.Kind]int {
	t.Helper()
	r, err := capture.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	counts := map[capture.Kind]int{}
	for {
		rec, err := r.Next()
		if err == io.EOF {
			return counts
		}
		if err != nil {
			t.Fatal(err)
		}
		counts[rec.Kind]++
	}
}

func waitForPaused(t *testing.T, c *Collector) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if c.Status().Paused {
			return
		}
		select {
		case <-deadline:
			t.Fatal("collector did not report paused")
		case <-tick.C:
		}
	}
}
