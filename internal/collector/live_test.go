package collector

import (
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
)

// collectorForFixture returns a collector wired to a replay source, without
// running its loop, so individual frames can be handed to handle directly.
func collectorForFixture(t *testing.T, name string) (*Collector, source.Source) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "lapdog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	src, err := source.NewReplay(filepath.Join(fixtureDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(Options{
		Source: src, Store: st,
		Clock:    NewFakeClock(time.Date(2026, 3, 2, 18, 0, 0, 0, time.UTC)),
		Interval: time.Second, MinSession: 0,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, src
}

// A handled frame is retained, so the interface can report what the simulator
// last said rather than only what has been accumulated.
func TestLiveReportsTheLastHandledFrame(t *testing.T) {
	c, src := collectorForFixture(t, "public-practice.lpd")
	defer src.Close()

	if got := c.Live(); got.Frame != nil {
		t.Fatal("a frame was reported before any was handled")
	}

	frame, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handle(frame); err != nil {
		t.Fatal(err)
	}

	live := c.Live()
	if live.Frame == nil {
		t.Fatal("no frame retained after handling one")
	}
	if live.Frame.At.IsZero() {
		t.Error("the frame carries no timestamp, so staleness cannot be computed")
	}
	// InCar, Driving and Replay are passthroughs from the sample recordLiveFrame
	// already received, not read from the row a second time, so the check has to
	// be against an independent recomputation rather than a hardcoded true/false —
	// otherwise it would only pin the fixture's happenstance, not the passthrough.
	// A swapped or dropped assignment in recordLiveFrame would go unnoticed by a
	// literal expectation but not by this one.
	wantSample, ok := SampleFrom(frame.Row, c.info.DriverInfo.DriverCarIdx)
	if !ok {
		t.Fatal("SampleFrom could not read the frame that was just handled")
	}
	if live.Frame.InCar != wantSample.InCar {
		t.Errorf("InCar = %v, want %v (from SampleFrom on the same row)", live.Frame.InCar, wantSample.InCar)
	}
	if live.Frame.Driving != wantSample.Driving {
		t.Errorf("Driving = %v, want %v (from SampleFrom on the same row)", live.Frame.Driving, wantSample.Driving)
	}
	if live.Frame.Replay != wantSample.Replay {
		t.Errorf("Replay = %v, want %v (from SampleFrom on the same row)", live.Frame.Replay, wantSample.Replay)
	}
	// Every telemetry field the fixture can supply is checked by name, not just
	// Lap and Speed: a silently dropped field (Gear, FuelLevel, Incidents, Reason,
	// or a Lap*Time) would otherwise pass unnoticed and render as "—" on the page,
	// which looks like staleness rather than the bug it is.
	if live.Frame.Lap == nil {
		t.Error("Lap was not captured")
	}
	if live.Frame.LapDistPct == nil {
		t.Error("LapDistPct was not captured")
	}
	if live.Frame.LapCurrentTimeS == nil {
		t.Error("LapCurrentTimeS was not captured")
	}
	if live.Frame.LapLastTimeS == nil {
		t.Error("LapLastTimeS was not captured")
	}
	if live.Frame.LapBestTimeS == nil {
		t.Error("LapBestTimeS was not captured")
	}
	if live.Frame.Speed == nil {
		t.Error("Speed was not captured")
	}
	if live.Frame.Gear == nil {
		t.Error("Gear was not captured")
	}
	if live.Frame.FuelLevel == nil {
		t.Error("FuelLevel was not captured")
	}
	if live.Frame.Incidents == nil {
		t.Error("Incidents was not captured")
	}
	// Reason has no nil state to check — it is a value type — but it must be one
	// of the declared constants, which catches a typo in the derivation call.
	switch live.Frame.Reason {
	case ReasonNone, ReasonReplay, ReasonNotInCar, ReasonPitBox, ReasonNotOnTrack:
	default:
		t.Errorf("Reason has an unrecognised value: %q", live.Frame.Reason)
	}
}

// Closing the session clears the frame. A finished session must not leave
// instantaneous values behind for the interface to present as current — the
// same rule that made clearActiveStatus necessary.
func TestLiveFrameIsClearedWhenTheSessionCloses(t *testing.T) {
	c, src := collectorForFixture(t, "public-practice.lpd")
	defer src.Close()

	frame, err := src.Next()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.handle(frame); err != nil {
		t.Fatal(err)
	}
	if c.Live().Frame == nil {
		t.Fatal("no frame retained after handling one")
	}

	c.closeSegment()

	if got := c.Live(); got.Frame != nil {
		t.Errorf("a frame survived the session closing: %+v", got.Frame)
	}
}

// The pit-box case, end to end from replayed frames rather than constructed rows.
//
// The fixture is synthetic — internal/synth wrote it — so this is a full frame
// stream through handle, not a real simulator's data. What it buys over a
// hand-built row is that every value comes from the same source the collector
// reads in production, through the same decoding path.
//
// This is the observation the Live page exists to explain: time in the car, no
// driving time, and previously no way to see why without reading the accounting
// code. The offline-test fixture drives, so the reason is empty; what is pinned
// here is that a reason is always present exactly when driving time is not
// accruing.
func TestLiveReasonMatchesAccountingOnReplayedFrames(t *testing.T) {
	c, src := collectorForFixture(t, "official-race-weekend.lpd")
	defer src.Close()

	// Counted, and required to be non-zero at the end. Without this the test had
	// two ways to pass while asserting nothing: an immediate read error, and a
	// stream of frames that never produced a retained frame. Deleting
	// recordLiveFrame left it green.
	asserted := 0
	for i := 0; i < 200; i++ {
		frame, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("frame %d: reading the fixture failed: %v", i, err)
		}
		if err := c.handle(frame); err != nil {
			t.Fatal(err)
		}
		live := c.Live()
		if live.Frame == nil {
			t.Fatalf("frame %d was handled but no frame was retained", i)
		}
		credited := live.Frame.InCar && live.Frame.Driving && !live.Frame.Replay
		if credited && live.Frame.Reason != ReasonNone {
			t.Fatalf("frame %d credits driving time but gives reason %q", i, live.Frame.Reason)
		}
		if !credited && live.Frame.Reason == ReasonNone {
			t.Fatalf("frame %d credits no driving time and gives no reason", i)
		}
		asserted++
	}
	if asserted == 0 {
		t.Fatal("no frames were checked, so this test proved nothing")
	}
}

// The three accounted totals are published together, because driving seconds
// alone cannot be read: zero driving is either a bug or a parked car, and only
// the connected and in-car figures beside it say which.
func TestLiveStatusReportsAllThreeTotals(t *testing.T) {
	c, src := collectorForFixture(t, "official-race-weekend.lpd")
	defer src.Close()

	// Two hundred frames, because the accountant credits the gap between samples —
	// the first frame accrues nothing at all — and because this fixture's driver
	// spends the opening minute connected but out of the car, which is exactly the
	// distinction being asserted below.
	for i := 0; i < 200; i++ {
		frame, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("frame %d: reading the fixture failed: %v", i, err)
		}
		if err := c.handle(frame); err != nil {
			t.Fatal(err)
		}
	}

	s := c.Live().Status
	if s.ConnectedSeconds <= 0 {
		t.Fatalf("ConnectedSeconds = %v, want time accrued after 200 frames", s.ConnectedSeconds)
	}
	if s.InCarSeconds <= 0 {
		t.Errorf("InCarSeconds = %v, want time accrued while in the car", s.InCarSeconds)
	}
	// The same ordering the accountant guarantees. It is what makes the band
	// interpretable, and a swapped assignment in refreshStatus would break it.
	if !(s.DrivingSeconds <= s.InCarSeconds && s.InCarSeconds <= s.ConnectedSeconds) {
		t.Errorf("driving=%v in-car=%v connected=%v, want driving <= in-car <= connected",
			s.DrivingSeconds, s.InCarSeconds, s.ConnectedSeconds)
	}
	// Published from the accountant, not recomputed, so they must equal it exactly.
	if s.ConnectedSeconds != c.seg.Acct.Connected || s.InCarSeconds != c.seg.Acct.InCar {
		t.Errorf("status totals (%v, %v) disagree with the accountant (%v, %v)",
			s.ConnectedSeconds, s.InCarSeconds, c.seg.Acct.Connected, c.seg.Acct.InCar)
	}
}
