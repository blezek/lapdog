package collector

import (
	"time"

	"github.com/blezek/lapdog/internal/irsdk"
)

// Clock supplies the current time.
//
// The collector takes one as a dependency rather than calling time.Now directly,
// so a replayed capture can run a ninety-minute race through in milliseconds.
type Clock interface {
	Now() time.Time
}

// RealClock reads the system clock.
type RealClock struct{}

// Now returns the current system time.
func (RealClock) Now() time.Time { return time.Now() }

// FakeClock is a manually advanced clock for tests.
type FakeClock struct{ t time.Time }

// NewFakeClock returns a clock fixed at t.
func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{t: t} }

// Now returns the clock's current value.
func (c *FakeClock) Now() time.Time { return c.t }

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// clampFactor is how many poll intervals a gap may span before it is treated as a
// stall rather than elapsed session time.
//
// Without this, a machine suspend would be recorded as hours of practice.
const clampFactor = 4

// Sample is one poll's accounting-relevant state, extracted from a telemetry row.
//
// There is no Connected field: receiving a sample at all means the sim is running
// and this session is active, which is exactly what connected time measures.
type Sample struct {
	T       float64 // frame timestamp, in seconds
	InCar   bool
	Driving bool
	Replay  bool
}

// Accountant accumulates the three time measures across samples.
//
// Time comes from the sample timestamps, never the wall clock, so replay is
// deterministic and can run faster than real time.
type Accountant struct {
	Connected float64
	InCar     float64
	Driving   float64

	// Clamped counts how many gaps were treated as stalls. A non-zero value in a
	// real session is worth logging.
	Clamped int

	interval float64
	lastT    float64
	haveLast bool
}

// NewAccountant returns an Accountant sized for the given poll interval.
func NewAccountant(interval time.Duration) *Accountant {
	s := interval.Seconds()
	if s <= 0 {
		s = 1
	}
	return &Accountant{interval: s}
}

// Reset zeroes the counters and forgets the baseline, so the next sample
// establishes a new one and credits nothing.
func (a *Accountant) Reset() {
	a.Connected, a.InCar, a.Driving = 0, 0, 0
	a.Clamped = 0
	a.lastT = 0
	a.haveLast = false
}

// Add credits the interval since the previous sample to whichever counters
// qualify.
//
// The first sample after construction or Reset establishes a baseline and credits
// nothing, because there is no prior observation to measure against. A replay
// sample credits nothing but still advances the baseline, so time either side of
// it is unaffected.
func (a *Accountant) Add(s Sample) {
	if !a.haveLast {
		a.lastT = s.T
		a.haveLast = true
		return
	}

	elapsed := s.T - a.lastT
	a.lastT = s.T

	// Time running backwards is nonsense; credit nothing rather than subtracting.
	if elapsed <= 0 {
		return
	}
	if elapsed > a.interval*clampFactor {
		elapsed = a.interval
		a.Clamped++
	}

	// Replay playback is never counted, and there is deliberately no setting for
	// it.
	if s.Replay {
		return
	}

	a.Connected += elapsed
	if s.InCar {
		a.InCar += elapsed
	}
	if s.InCar && s.Driving {
		a.Driving += elapsed
	}
}

// SampleFrom extracts accounting state from a telemetry row.
//
// It reports false if a variable it needs is absent, which the caller treats as
// "do not record this session" rather than guessing.
func SampleFrom(row irsdk.Row, driverCarIdx int) (Sample, bool) {
	inCar, ok := row.Bool("IsOnTrackCar")
	if !ok {
		return Sample{}, false
	}
	surfaces, ok := row.IntArray("CarIdxTrackSurface")
	if !ok {
		return Sample{}, false
	}
	if driverCarIdx < 0 || driverCarIdx >= len(surfaces) {
		return Sample{}, false
	}

	// A missing replay flag is treated as "not replaying" rather than refusing the
	// session: its absence should not stop recording.
	replay, _ := row.Bool("IsReplayPlaying")

	loc := irsdk.TrkLoc(surfaces[driverCarIdx])
	// Driving includes OffTrack, ApproachingPits and OnTrack — the driver is
	// driving in all three. Only being out of the world and sitting stationary in
	// the pit box are excluded.
	driving := loc != irsdk.NotInWorld && loc != irsdk.InPitStall

	return Sample{InCar: inCar, Driving: driving, Replay: replay}, true
}

// NotDrivingReason explains why driving time is not accruing.
//
// It exists because "zero driving seconds" is indistinguishable from a bug
// without it: a real capture recorded 154 seconds in the car and no driving
// time at all, which was correct — the car sat in the pit box — and could only
// be established by reading this file.
type NotDrivingReason string

// NotDrivingReason values. The empty string means driving time is accruing.
const (
	ReasonNone       NotDrivingReason = ""
	ReasonReplay     NotDrivingReason = "watching a replay"
	ReasonNotInCar   NotDrivingReason = "not in the car"
	ReasonPitBox     NotDrivingReason = "in the pit box"
	ReasonNotOnTrack NotDrivingReason = "not on track"
)

// NotDrivingReasonFrom reports why driving time is not accruing for this frame,
// or ReasonNone when it is.
//
// It reads exactly the values SampleFrom reads and follows Accountant.Add's
// precedence, so the explanation cannot contradict the boolean it explains.
// Replay comes first because Add returns before crediting anything at all.
//
// A row this function cannot read otherwise yields ReasonNone rather than a
// guess: the collector refuses such a session, and an invented explanation would
// be worse than none. Replay is the one exception, because it is checked first: a
// present-and-true IsReplayPlaying is reported even when nothing else in the row
// is readable, which is correct, since Add credits nothing during a replay
// regardless of what the rest of the row says.
func NotDrivingReasonFrom(row irsdk.Row, driverCarIdx int) NotDrivingReason {
	if replay, ok := row.Bool("IsReplayPlaying"); ok && replay {
		return ReasonReplay
	}
	inCar, ok := row.Bool("IsOnTrackCar")
	if !ok {
		return ReasonNone
	}
	// The surface array's presence and bounds are checked here, before inCar's
	// value is consulted, because SampleFrom refuses the row on that basis
	// regardless of inCar: an out-of-range index makes the row unreadable, not
	// merely "not in the car".
	surfaces, ok := row.IntArray("CarIdxTrackSurface")
	if !ok || driverCarIdx < 0 || driverCarIdx >= len(surfaces) {
		return ReasonNone
	}
	if !inCar {
		return ReasonNotInCar
	}
	switch irsdk.TrkLoc(surfaces[driverCarIdx]) {
	case irsdk.InPitStall:
		return ReasonPitBox
	case irsdk.NotInWorld:
		return ReasonNotOnTrack
	default:
		return ReasonNone
	}
}
