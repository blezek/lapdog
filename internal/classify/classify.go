// Package classify decides what kind of session the sim is running.
//
// Classify is a pure function with no I/O and no state, which is deliberate: it
// is the highest-risk logic in the application, and purity makes it exhaustively
// table-testable.
package classify

import (
	"strings"

	"github.com/blezek/lapdog/internal/sessionyaml"
)

// SessionType is what the driver is doing: practising, qualifying, racing.
type SessionType string

// SessionType values.
const (
	TypePractice    SessionType = "Practice"
	TypeQualify     SessionType = "Qualify"
	TypeRace        SessionType = "Race"
	TypeWarmup      SessionType = "Warmup"
	TypeTimeTrial   SessionType = "TimeTrial"
	TypeOfflineTest SessionType = "OfflineTest"
	TypeUnknown     SessionType = "Unknown"
)

// EventContext is what kind of event the session belongs to. It is orthogonal to
// SessionType, which is what lets "race practice" and "public practice" be
// derived rather than stored as labels.
type EventContext string

// EventContext values.
const (
	ContextOfficialRace     EventContext = "OfficialRace"
	ContextOfficialPractice EventContext = "OfficialPractice"
	ContextLeague           EventContext = "League"
	ContextHosted           EventContext = "Hosted"
	ContextOffline          EventContext = "Offline"
	ContextTimeTrial        EventContext = "TimeTrial"
	ContextAI               EventContext = "AI"
	ContextUnknown          EventContext = "Unknown"
)

// AIDetection records how AI opponents were identified, so that
// heuristically-classified sessions can be found and re-classified once the real
// field is confirmed. See the design spec, section 6.5.
type AIDetection string

// AIDetection values.
const (
	AIDetectField     AIDetection = "field"
	AIDetectHeuristic AIDetection = "heuristic"
	AIDetectNone      AIDetection = "none"
)

// Result is the outcome of classifying one session.
type Result struct {
	SessionType     SessionType
	EventContext    EventContext
	AIOpponentCount int
	AIDetection     AIDetection

	// RawSessionType is the unnormalised YAML string, retained so an Unknown
	// classification can be diagnosed.
	RawSessionType string
}

// NormaliseSessionType maps a raw SessionType string from the session YAML onto
// a SessionType, returning TypeUnknown for anything unrecognised rather than
// guessing.
func NormaliseSessionType(raw string) SessionType {
	switch strings.ToLower(strings.Join(strings.Fields(raw), " ")) {
	case "practice", "open practice", "lone practice":
		return TypePractice
	case "qualify", "open qualify", "lone qualify":
		return TypeQualify
	case "race", "heat", "consolation":
		return TypeRace
	case "warmup":
		return TypeWarmup
	case "offline testing", "testing":
		return TypeOfflineTest
	case "time trial":
		return TypeTimeTrial
	default:
		return TypeUnknown
	}
}

// detectAI reports the AI opponent count and how it was determined.
func detectAI(info *sessionyaml.Info, st SessionType) (int, AIDetection) {
	if count, present := info.AIOpponentCount(); present {
		return count, AIDetectField
	}
	// Heuristic fallback, used only while the CarIsAI field is unverified. It
	// cannot tell an AI race from an offline hosted race with no AI, and it
	// deliberately misses AI practice sessions. Both errors are corrected by
	// reclassify once the field is known.
	w := info.WeekendInfo
	if st == TypeRace &&
		w.SubSessionID == 0 &&
		w.Official == 0 &&
		w.LeagueID == 0 &&
		len(info.DriverInfo.Drivers) > 1 {
		return len(info.DriverInfo.Drivers) - 1, AIDetectHeuristic
	}
	return 0, AIDetectNone
}

// Classify determines the session type and event context for the session with
// the given SessionNum.
func Classify(info *sessionyaml.Info, sessionNum int) Result {
	if info == nil {
		return Result{SessionType: TypeUnknown, EventContext: ContextUnknown, AIDetection: AIDetectNone}
	}

	raw := ""
	if s, ok := info.SessionByNum(sessionNum); ok {
		raw = s.SessionType
	}
	st := NormaliseSessionType(raw)

	aiCount, aiHow := detectAI(info, st)

	res := Result{
		SessionType:     st,
		AIOpponentCount: aiCount,
		AIDetection:     aiHow,
		RawSessionType:  raw,
	}
	res.EventContext = context(info, st, aiCount)
	return res
}

// context applies the ordered event-context rules. First match wins.
func context(info *sessionyaml.Info, st SessionType, aiCount int) EventContext {
	w := info.WeekendInfo

	// A league session never contains AI, so League winning is harmless and
	// keeps league accounting intact.
	if w.LeagueID != 0 {
		return ContextLeague
	}
	// AI is checked before Offline because an AI event is always offline, and
	// "raced against AI" is the more specific fact.
	if aiCount > 0 {
		return ContextAI
	}
	if (w.SimMode != "" && w.SimMode != "full") || st == TypeOfflineTest {
		return ContextOffline
	}
	if st == TypeTimeTrial {
		return ContextTimeTrial
	}
	if w.Official == 1 {
		if info.HasRaceSession() {
			return ContextOfficialRace
		}
		return ContextOfficialPractice
	}
	return ContextHosted
}

// Label renders the pair as the string the UI shows. Labels are computed, never
// stored, so the rules can change without a data migration.
func Label(t SessionType, c EventContext) string {
	if c == ContextOffline {
		return "Offline Testing"
	}
	if t == TypeTimeTrial {
		return "Time Trial"
	}

	var base string
	switch t {
	case TypePractice:
		base = "Practice"
	case TypeQualify:
		base = "Qualifying"
	case TypeRace:
		base = "Race"
	case TypeWarmup:
		base = "Warmup"
	default:
		base = "Unknown"
	}

	switch c {
	case ContextLeague:
		return "League " + base
	case ContextAI:
		return "AI " + base
	case ContextHosted:
		return "Hosted " + base
	case ContextOfficialPractice:
		if t == TypePractice {
			return "Public Practice"
		}
		return base
	case ContextOfficialRace:
		if t == TypePractice {
			return "Race Practice"
		}
		return base
	default:
		return base
	}
}
