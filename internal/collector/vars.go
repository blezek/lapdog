package collector

import "github.com/blezek/lapdog/internal/irsdk"

// RequiredCoreVars are the telemetry variables the collector needs in every
// session. All are confirmed present in
// documentation/telemetry_11_23_15.pdf, Appendix A.
//
// If any is absent the session is not recorded and the omission is logged and
// surfaced in the interface. Recording wrong data is worse than recording none.
//
// CarIdxTrackSurface is here rather than in RequiredRaceVars because
// driving_seconds depends on it in every session type, not just races. Outside
// races only the local driver's index is read.
var RequiredCoreVars = []string{
	"SessionNum",
	"SessionState",
	"SessionTime",
	"SessionTimeRemain",
	"SessionLapsRemain",
	"IsOnTrack",
	"IsOnTrackCar",
	"IsInGarage",
	"IsReplayPlaying",
	"OnPitRoad",
	"Lap",
	"LapCurrentLapTime",
	"LapLastLapTime",
	"LapBestLapTime",
	"LapBestLap",
	"LapDist",
	"LapDistPct",
	"FuelLevel",
	"PlayerCarPosition",
	"PlayerCarClassPosition",
	"PlayerCarIdx",
	"CarIdxTrackSurface",
}

// RequiredRaceVars are additionally needed to attribute position changes, and are
// read only when the session is a race. Position in practice is an artefact of
// who happens to be on track.
var RequiredRaceVars = []string{
	"CarIdxPosition",
	"CarIdxClassPosition",
	"CarIdxOnPitRoad",
	"CarIdxLap",
}

// OptionalIncidentVar is preferred for incident counting when present, because it
// updates live rather than only when the session YAML does. It postdates the 2015
// documentation, so its absence is not an error.
const OptionalIncidentVar = "PlayerCarMyIncidentCount"

// MissingVars returns which of names are absent from the row's layout.
func MissingVars(row irsdk.Row, names []string) []string {
	var missing []string
	for _, n := range names {
		if !row.Has(n) {
			missing = append(missing, n)
		}
	}
	return missing
}
