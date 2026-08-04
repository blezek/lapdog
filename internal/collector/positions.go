package collector

import (
	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/sessionyaml"
	"github.com/blezek/lapdog/internal/store"
)

// PositionDetector turns changes in the local driver's race position into
// attributed position events.
//
// The attribution is the point. A position change is not an overtake: if two cars
// ahead pit, the driver gains two places without passing anyone. So when the
// position changes, the car now occupying the driver's former position is
// identified and its state at that moment decides the cause. Only OnTrack causes
// count toward a pass/passed ratio.
//
// Known limitation at the default 1 Hz poll rate: two changes within the same
// second collapse into one event and the intermediate position is lost, and a
// simultaneous multi-car shuffle may attribute the swap to the wrong opponent.
// This is inherent to polling and is documented rather than solved.
type PositionDetector struct {
	havePrev bool
	prevPos  int32
}

// NewPositionDetector returns a detector with no baseline.
func NewPositionDetector() *PositionDetector { return &PositionDetector{} }

// Reset forgets the baseline. Called when a session segment changes.
func (d *PositionDetector) Reset() { *d = PositionDetector{} }

// Observe consumes one row and returns a position event when the local driver's
// position changed.
//
// Callers must only invoke this for races; position in practice is an artefact of
// who happens to be on track.
func (d *PositionDetector) Observe(
	row irsdk.Row, driverCarIdx int, sessionTimeS float64, info *sessionyaml.Info,
) (*store.PositionEvent, bool) {
	pos, ok := row.Int("PlayerCarPosition")
	if !ok {
		return nil, false
	}
	carPositions, ok := row.IntArray("CarIdxPosition")
	if !ok {
		return nil, false
	}

	// Position 0 means not yet classified, which happens before the green flag. It
	// is not first place, and transitions in and out of it are not position
	// changes.
	if pos <= 0 {
		d.havePrev = false
		return nil, false
	}
	if !d.havePrev {
		d.havePrev = true
		d.prevPos = pos
		return nil, false
	}
	if pos == d.prevPos {
		return nil, false
	}

	from := d.prevPos
	d.prevPos = pos

	ev := &store.PositionEvent{
		SessionTimeS: sessionTimeS,
		FromPosition: int(from),
		ToPosition:   int(pos),
		Cause:        store.CauseUnknown,
	}
	if lap, ok := row.Int("Lap"); ok {
		ev.LapNumber = int(lap)
	}

	// Find the car that now holds the position the driver just vacated.
	opponent := -1
	for idx, p := range carPositions {
		if idx == driverCarIdx {
			continue
		}
		if p == from {
			opponent = idx
			break
		}
	}
	if opponent < 0 {
		// Unattributable, but the change still happened and is still recorded.
		return ev, true
	}

	ev.OpponentCarIdx = &opponent
	ev.Cause = attributeCause(row, opponent)
	if info != nil {
		for _, dr := range info.DriverInfo.Drivers {
			if dr.CarIdx == opponent && dr.UserName != "" {
				name := dr.UserName
				ev.OpponentName = &name
				break
			}
		}
	}
	return ev, true
}

// attributeCause decides why a swap with the given car happened, based on that
// car's state at this moment.
//
// Pit road is checked before track surface because a car in its own pit box is
// both on pit road and InPitStall, and "they pitted" is the more informative
// answer than "they were in a pit stall".
func attributeCause(row irsdk.Row, opponent int) store.Cause {
	if onPit, ok := row.BoolArray("CarIdxOnPitRoad"); ok &&
		opponent < len(onPit) && onPit[opponent] {
		return store.CauseOpponentPit
	}
	if surfaces, ok := row.IntArray("CarIdxTrackSurface"); ok && opponent < len(surfaces) {
		if irsdk.TrkLoc(surfaces[opponent]) == irsdk.NotInWorld {
			return store.CauseOpponentOffWorld
		}
		return store.CauseOnTrack
	}
	return store.CauseUnknown
}
