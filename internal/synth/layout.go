// Package synth generates synthetic iRacing capture files.
//
// The output is byte-identical in form to what the application records from the
// simulator's shared memory: gzipped .lpd captures holding a variable-layout
// header, session-info YAML blobs that evolve over the session, and one raw
// variable row per poll. Feeding these through the replay Source therefore
// exercises the entire ingestion path — binary decode, classification, time
// accounting, lap detection and position attribution — rather than skipping it
// the way a pre-built database would.
package synth

import "github.com/blezek/lapdog/internal/irsdk"

// MaxCars is the size of the per-car arrays the simulator publishes.
const MaxCars = 64

// TickRate is the rate the real simulator writes at. Captures are recorded at
// the poll rate, not the tick rate, but the header reports it faithfully.
const TickRate = 60

// varSpec declares one variable in the emulated layout.
type varSpec struct {
	name  string
	typ   irsdk.VarType
	count int32
	unit  string
	desc  string
}

// layoutSpec is the variable set the generator publishes.
//
// It contains every variable in collector.RequiredCoreVars and
// collector.RequiredRaceVars, plus PlayerCarMyIncidentCount — which postdates
// the 2015 documentation and is therefore optional in the collector — and a
// handful of extra channels a real session would carry, so the fixtures are not
// suspiciously minimal.
var layoutSpec = []varSpec{
	{"SessionNum", irsdk.VarInt, 1, "", "Session number"},
	{"SessionState", irsdk.VarInt, 1, "irsdk_SessionState", "Session state"},
	{"SessionTime", irsdk.VarDouble, 1, "s", "Seconds since session start"},
	{"SessionTimeRemain", irsdk.VarDouble, 1, "s", "Seconds left till session ends"},
	{"SessionLapsRemain", irsdk.VarInt, 1, "", "Laps left till session ends"},
	{"SessionFlags", irsdk.VarBitField, 1, "irsdk_Flags", "Session flags"},
	{"SessionUniqueID", irsdk.VarInt, 1, "", "Session ID"},

	{"IsOnTrack", irsdk.VarBool, 1, "", "Car on track physics running with player in car"},
	{"IsOnTrackCar", irsdk.VarBool, 1, "", "Car on track physics running"},
	{"IsInGarage", irsdk.VarBool, 1, "", "Car in garage physics running"},
	{"IsReplayPlaying", irsdk.VarBool, 1, "", "Replay is playing"},
	{"OnPitRoad", irsdk.VarBool, 1, "", "Player car on pit road between the cones"},

	{"Lap", irsdk.VarInt, 1, "", "Lap count"},
	{"LapCompleted", irsdk.VarInt, 1, "", "Laps completed"},
	{"LapCurrentLapTime", irsdk.VarFloat, 1, "s", "Estimate of players current lap time"},
	{"LapLastLapTime", irsdk.VarFloat, 1, "s", "Players last lap time"},
	{"LapBestLapTime", irsdk.VarFloat, 1, "s", "Players best lap time"},
	{"LapBestLap", irsdk.VarInt, 1, "", "Players best lap number"},
	{"LapDist", irsdk.VarFloat, 1, "m", "Meters traveled from S/F this lap"},
	{"LapDistPct", irsdk.VarFloat, 1, "%", "Percentage distance around lap"},
	{"LapDeltaToSessionBestLap", irsdk.VarFloat, 1, "s", "Delta time for session best lap"},

	{"FuelLevel", irsdk.VarFloat, 1, "l", "Liters of fuel remaining"},
	{"FuelLevelPct", irsdk.VarFloat, 1, "%", "Percent fuel remaining"},
	{"FuelUsePerHour", irsdk.VarFloat, 1, "kg/h", "Engine fuel used instantaneous"},

	{"Speed", irsdk.VarFloat, 1, "m/s", "GPS vehicle speed"},
	{"RPM", irsdk.VarFloat, 1, "revs/min", "Engine rpm"},
	{"Gear", irsdk.VarInt, 1, "", "Current gear"},
	{"Throttle", irsdk.VarFloat, 1, "%", "Throttle position"},
	{"Brake", irsdk.VarFloat, 1, "%", "Brake position"},
	{"SteeringWheelAngle", irsdk.VarFloat, 1, "rad", "Steering wheel angle"},

	{"PlayerCarPosition", irsdk.VarInt, 1, "", "Players position in race"},
	{"PlayerCarClassPosition", irsdk.VarInt, 1, "", "Players class position in race"},
	{"PlayerCarIdx", irsdk.VarInt, 1, "", "Players car array index"},
	{"PlayerCarMyIncidentCount", irsdk.VarInt, 1, "", "Players own incident count for this session"},

	{"AirTemp", irsdk.VarFloat, 1, "C", "Temperature of air at start/finish line"},
	{"TrackTempCrew", irsdk.VarFloat, 1, "C", "Temperature of track measured by crew around track"},

	{"CarIdxLap", irsdk.VarInt, MaxCars, "", "Lap count by car index"},
	{"CarIdxLapDistPct", irsdk.VarFloat, MaxCars, "%", "Percentage distance around lap by car index"},
	{"CarIdxPosition", irsdk.VarInt, MaxCars, "", "Cars position in race by car index"},
	{"CarIdxClassPosition", irsdk.VarInt, MaxCars, "", "Cars class position in race by car index"},
	{"CarIdxOnPitRoad", irsdk.VarBool, MaxCars, "", "On pit road between the cones by car index"},
	{"CarIdxTrackSurface", irsdk.VarInt, MaxCars, "irsdk_TrkLoc", "Track surface type by car index"},
}

// Layout returns the variable headers for the emulated layout, with offsets
// assigned contiguously, along with the resulting row length.
func Layout() ([]irsdk.VarHeader, int32) {
	out := make([]irsdk.VarHeader, 0, len(layoutSpec))
	var off int32
	for _, s := range layoutSpec {
		out = append(out, irsdk.VarHeader{
			Type:   s.typ,
			Offset: off,
			Count:  s.count,
			Name:   s.name,
			Desc:   s.desc,
			Unit:   s.unit,
			// CountAsTime marks per-car arrays in the real SDK.
			CountAsTime: s.count > 1,
		})
		off += int32(s.typ.Size()) * s.count
	}
	return out, off
}
