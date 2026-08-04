package synth

import (
	"fmt"
	"time"
)

// EventFlavour is the kind of event a weekend represents. It determines the
// identity fields the classifier keys on, and therefore which event_context the
// ingestion path should derive.
type EventFlavour string

// Event flavours the generator produces. Between them these cover every
// event_context branch in the classifier.
const (
	FlavourOfficialRace     EventFlavour = "official-race"
	FlavourOfficialPractice EventFlavour = "official-practice"
	FlavourLeague           EventFlavour = "league"
	FlavourHosted           EventFlavour = "hosted"
	FlavourAI               EventFlavour = "ai"
	FlavourOfflineTest      EventFlavour = "offline-test"
	FlavourTimeTrial        EventFlavour = "time-trial"
)

// SessionResult is one car's classified result in a session.
type SessionResult struct {
	CarIdx        int
	Position      int
	ClassPosition int
	LapsComplete  int
	LapsLed       int
	Incidents     int
	FastestLap    int
	FastestTimeS  float64
	TotalTimeS    float64
	ReasonOutID   int
}

// QualifyResult is one car's qualifying result.
type QualifyResult struct {
	CarIdx        int
	Position      int
	ClassPosition int
	FastestLap    int
	FastestTimeS  float64
}

// Session is one segment of a weekend: one entry in SessionInfo.Sessions.
type Session struct {
	Num int
	// RawType is the SessionType string as the sim would emit it, so the
	// generator exercises the classifier's normalisation rather than bypassing
	// it with pre-normalised values.
	RawType string

	// GarageSeconds is time connected with the car not under physics: menus,
	// setup screens, sitting in the garage. It accrues connected time only.
	GarageSeconds float64
	// PitSeconds is time in the car but stationary in the pit box. It accrues
	// in-car time but not driving time.
	PitSeconds float64
	// Laps is how many timed laps the driver completes.
	Laps int

	// LapLimit and TimeLimitS describe the session's scheduled length, or zero
	// when unlimited.
	LapLimit   int
	TimeLimitS float64

	// ReplaySeconds is time spent watching a replay, which must contribute to
	// no counter at all.
	ReplaySeconds float64

	Results      []SessionResult
	AverageLapS  float64
	LapsComplete int
	CautionFlags int
	LeadChanges  int
}

// LapsField renders SessionLaps the way the sim does, including the
// "unlimited" sentinel that forces the field to be parsed as a string.
func (s *Session) LapsField() string {
	if s.LapLimit <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", s.LapLimit)
}

// TimeField renders SessionTime the way the sim does.
func (s *Session) TimeField() string {
	if s.TimeLimitS <= 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%0.4f sec", s.TimeLimitS)
}

// Weekend is one subsession's worth of activity: a set of sessions sharing a
// track, car and field.
type Weekend struct {
	Flavour   EventFlavour
	StartedAt time.Time

	SubSessionID int
	SessionID    int
	SeriesID     int
	SeasonID     int
	LeagueID     int
	Official     int
	RaceWeek     int
	EventType    string
	SimMode      string

	Track Track
	Car   Car

	// BaseLapS is this car and track combination's reference lap, before the
	// driver's own pace factor is applied.
	BaseLapS float64
	// PaceFactor is the driver's pace this weekend. Above 1 is slower than the
	// reference; it trends down over the two years to model improvement.
	PaceFactor float64
	// IncidentRatePerHour drives how often incidents accrue.
	IncidentRatePerHour float64

	DriverCarIdx  int
	DriverIRating int
	Opponents     []Opponent

	// EmitCarIsAI controls whether the driver entries carry the CarIsAI field.
	// AI weekends mostly set it, but a deliberate minority do not, so the
	// classifier's heuristic fallback is exercised by real fixture data.
	EmitCarIsAI bool

	AirTempC   float64
	TrackTempC float64

	Sessions       []Session
	QualifyResults []QualifyResult
}

// qualifyIndex returns the index of the qualifying session, or -1.
func (w *Weekend) qualifyIndex() int {
	for i := range w.Sessions {
		if isQualifyType(w.Sessions[i].RawType) {
			return i
		}
	}
	return -1
}

// raceIndex returns the index of the race session, or -1.
func (w *Weekend) raceIndex() int {
	for i := range w.Sessions {
		if isRaceType(w.Sessions[i].RawType) {
			return i
		}
	}
	return -1
}

// LapSeconds returns the driver's expected lap time this weekend.
func (w *Weekend) LapSeconds() float64 {
	return w.BaseLapS * w.PaceFactor
}

// FieldSize is the number of cars including the local driver.
func (w *Weekend) FieldSize() int { return len(w.Opponents) + 1 }

// carIdxFor maps an opponent index to its car index, skipping the local
// driver's slot.
func (w *Weekend) carIdxFor(opponent int) int {
	if opponent >= w.DriverCarIdx {
		return opponent + 1
	}
	return opponent
}

// isRaceType reports whether a raw SessionType denotes a race.
func isRaceType(raw string) bool {
	switch raw {
	case "Race", "Heat", "Consolation":
		return true
	}
	return false
}

// isQualifyType reports whether a raw SessionType denotes qualifying.
func isQualifyType(raw string) bool {
	switch raw {
	case "Qualify", "Open Qualify", "Lone Qualify":
		return true
	}
	return false
}

// isPracticeType reports whether a raw SessionType denotes practice.
func isPracticeType(raw string) bool {
	switch raw {
	case "Practice", "Open Practice", "Lone Practice", "Warmup":
		return true
	}
	return false
}
