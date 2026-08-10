package collector

import (
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
	if live.Frame.Lap == nil {
		t.Error("Lap was not captured")
	}
	if live.Frame.Speed == nil {
		t.Error("Speed was not captured")
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
