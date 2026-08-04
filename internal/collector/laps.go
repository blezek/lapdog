package collector

import (
	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/store"
)

// LapDetector turns a stream of telemetry rows into completed-lap records.
//
// A lap is recognised when the Lap counter increments and LapLastLapTime holds a
// usable time. The lap recorded is the one just finished, which is the counter
// value before the increment — not the one now starting.
type LapDetector struct {
	haveBaseline  bool
	prevLap       int32
	prevFuel      float64
	haveFuel      bool
	prevIncidents int
	pitSeen       bool
}

// NewLapDetector returns a detector with no baseline.
func NewLapDetector() *LapDetector { return &LapDetector{} }

// Reset forgets the baseline, so the next observation establishes a new one and
// emits nothing. Called when a session segment changes.
func (d *LapDetector) Reset() { *d = LapDetector{} }

// Observe consumes one row and returns a completed lap when one was crossed.
//
// incidents is the running session incident count, used to attribute incidents to
// the lap they happened on. bestSoFar is the session best before this lap, used
// for the delta; nil means there is no best yet.
func (d *LapDetector) Observe(row irsdk.Row, incidents int, bestSoFar *float64) (*store.Lap, bool) {
	lapNow, ok := row.Int("Lap")
	if !ok {
		return nil, false
	}

	// Pit road at any point during the lap makes it a pit lap.
	if onPit, ok := row.Bool("OnPitRoad"); ok && onPit {
		d.pitSeen = true
	}

	fuelNow, haveFuelNow := row.Float("FuelLevel")

	// The first observation establishes the reference point. There is no previous
	// lap number to compare against, so no lap is emitted.
	if !d.haveBaseline {
		d.haveBaseline = true
		d.advance(lapNow, fuelNow, haveFuelNow, incidents)
		return nil, false
	}

	// A counter going backwards means a reset or a new session rather than a lap;
	// re-baseline rather than emitting nonsense.
	if lapNow < d.prevLap {
		d.advance(lapNow, fuelNow, haveFuelNow, incidents)
		return nil, false
	}
	if lapNow == d.prevLap {
		return nil, false
	}

	// The counter advanced. LapLastLapTime describes the lap immediately before
	// the current one.
	lastTime, haveTime := row.Float("LapLastLapTime")
	completed := lapNow - 1

	if !haveTime || lastTime <= 0 {
		// An increment with no usable time is an out lap. Advance the baseline so
		// the next crossing is measured from here, and clear the per-lap flags.
		d.advance(lapNow, fuelNow, haveFuelNow, incidents)
		return nil, false
	}

	rec := &store.Lap{
		LapNumber:      int(completed),
		LapTimeS:       &lastTime,
		IncidentsOnLap: incidents - d.prevIncidents,
		IsPitLap:       d.pitSeen,
	}
	if rec.IncidentsOnLap < 0 {
		rec.IncidentsOnLap = 0
	}
	if bestSoFar != nil {
		delta := lastTime - *bestSoFar
		rec.DeltaToBestS = &delta
	}
	if haveFuelNow {
		end := fuelNow
		rec.FuelLevelEndL = &end
		// A fuel increase means a refuel happened, which makes the delta
		// meaningless. Leave it nil rather than reporting negative usage.
		if d.haveFuel && fuelNow <= d.prevFuel {
			used := d.prevFuel - fuelNow
			rec.FuelUsedL = &used
		}
	}
	if p, ok := row.Int("PlayerCarPosition"); ok && p > 0 {
		v := int(p)
		rec.Position = &v
	}
	if p, ok := row.Int("PlayerCarClassPosition"); ok && p > 0 {
		v := int(p)
		rec.ClassPosition = &v
	}

	d.advance(lapNow, fuelNow, haveFuelNow, incidents)
	return rec, true
}

// advance moves the reference point forward and clears per-lap state.
func (d *LapDetector) advance(lap int32, fuel float64, haveFuel bool, incidents int) {
	d.prevLap = lap
	if haveFuel {
		d.prevFuel = fuel
		d.haveFuel = true
	}
	d.prevIncidents = incidents
	d.pitSeen = false
}
