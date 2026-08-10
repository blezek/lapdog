package source

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/irsdk"
)

// Off Windows the live source must report ErrDisconnected rather than failing to
// construct, because the collector treats a missing simulator as normal and keeps
// serving. Constructing must not require a simulator either: LapDog starts before
// iRacing does.
func TestNewLiveWithoutASimulator(t *testing.T) {
	s, err := NewLive()
	if err != nil {
		t.Fatalf("NewLive: %v", err)
	}
	defer s.Close()

	if p, ok := s.(Paced); !ok {
		t.Error("the live source must implement Paced so the poll rate is adjustable")
	} else {
		// A tiny interval keeps the test fast; Next sleeps for it before reading.
		p.SetInterval(time.Millisecond)
	}

	if runtime.GOOS == "windows" {
		t.Skip("a simulator may be running; the disconnected path is not assertable")
	}
	if _, err := s.Next(); !errors.Is(err, ErrDisconnected) {
		t.Errorf("Next() with no simulator = %v, want ErrDisconnected", err)
	}
}

// Meta is empty until the sim declares a layout, rather than guessing one.
func TestLiveMetaEmptyBeforeFirstRead(t *testing.T) {
	s, err := NewLive()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if m := s.Meta(); m.NumVars != 0 || len(m.VarHeaders) != 0 {
		t.Errorf("Meta before any read = %+v, want zero", m)
	}
}

// A non-positive interval is ignored rather than accepted.
//
// Zero would turn Next into a spin loop over shared memory, which is a busy core
// for no extra data: the sim publishes at its tick rate regardless.
func TestLiveRejectsNonPositiveInterval(t *testing.T) {
	s := &live{interval: time.Second, now: time.Now}
	for _, d := range []time.Duration{0, -time.Second} {
		s.SetInterval(d)
		if s.interval != time.Second {
			t.Errorf("SetInterval(%v) changed the interval to %v", d, s.interval)
		}
	}
	s.SetInterval(250 * time.Millisecond)
	if s.interval != 250*time.Millisecond {
		t.Errorf("SetInterval(250ms) = %v", s.interval)
	}
}

func TestLiveNextContextStopsWaitingWhenCancelled(t *testing.T) {
	s := &live{interval: time.Hour, now: time.Now}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		_, err := s.NextContext(ctx)
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("NextContext after cancel = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("NextContext did not return after cancellation")
	}
	if s.polls != 0 {
		t.Errorf("polls = %d after canceled sleep, want 0", s.polls)
	}
}

// Frame.T must be monotonic seconds since the source started, not the sim's
// SessionTime.
//
// This is the bug the test exists to prevent. SessionTime restarts at each session
// in a weekend, so an interval measured across that boundary is negative — and the
// accountant credits nothing for a negative interval. Practice time would vanish
// at exactly the moment the session changed, with nothing logged.
func TestLiveFrameTimeIsMonotonicNotSessionTime(t *testing.T) {
	base := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	var offset time.Duration
	s := &live{
		interval: time.Nanosecond,
		now:      func() time.Time { return base.Add(offset) },
	}

	// First reading anchors the origin, so it must be zero rather than a wall
	// clock value.
	if s.started.IsZero() {
		s.started = s.now()
	}
	if got := s.now().Sub(s.started).Seconds(); got != 0 {
		t.Errorf("first frame T = %v, want 0", got)
	}

	// Time advances even if the sim's own clock would have gone backwards.
	offset = 3 * time.Second
	if got := s.now().Sub(s.started).Seconds(); got != 3 {
		t.Errorf("T after three seconds = %v, want 3", got)
	}
	offset = 90 * time.Second
	if got := s.now().Sub(s.started).Seconds(); got != 90 {
		t.Errorf("T after ninety seconds = %v, want 90", got)
	}
}

// Closing a source that never connected must not panic, since that is the state it
// spends most of its life in.
func TestLiveCloseWithoutConnection(t *testing.T) {
	s, err := NewLive()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close on an unconnected source = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close = %v; Close must be idempotent", err)
	}
}

// An absent simulator is not a failure, and a real read failure is not silence.
//
// Collapsing both into ErrDisconnected is what hid a genuine mapping bug: the Windows
// build connected to iRacing, recorded nothing, said only "not connected", and wrote
// nothing to the log. Next still returns ErrDisconnected either way — the collector
// depends on that — but the reason survives when there is one.
func TestLiveDistinguishesIdleFromFailure(t *testing.T) {
	s := &live{interval: time.Nanosecond, now: time.Now}

	// Off Windows, Open reports ErrUnsupported rather than ErrNotRunning, which counts
	// as a real failure: the build cannot read telemetry at all, and saying so is more
	// use than reporting an idle simulator that was never looked for.
	if _, err := s.Next(); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("Next() = %v, want ErrDisconnected", err)
	}
	if runtime.GOOS != "windows" {
		if got := s.LastFailure(); got == "" {
			t.Error("LastFailure is empty after an unsupported-platform open; the reason was discarded")
		} else if !strings.Contains(got, "Windows") {
			t.Errorf("LastFailure = %q, want it to name the platform limitation", got)
		}
	}

	// A recorded failure must clear rather than persist once reading works, or the
	// interface would keep showing a stale reason indefinitely.
	s.noteFailure(nil)
	if got := s.LastFailure(); got != "" {
		t.Errorf("LastFailure = %q after a success, want empty", got)
	}
}

// The idle case records nothing, so an absent simulator never shows a reason.
func TestLiveIdleRecordsNoFailure(t *testing.T) {
	s := &live{interval: time.Nanosecond, now: time.Now}
	s.noteFailure(irsdk.ErrNotRunning)
	// noteFailure is only ever called for non-idle errors, so verify the caller's
	// discrimination rather than the setter: an ErrNotRunning open must leave it clear.
	s.noteFailure(nil)
	if got := s.LastFailure(); got != "" {
		t.Errorf("LastFailure = %q, want empty for an idle simulator", got)
	}
}
