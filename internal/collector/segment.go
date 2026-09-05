package collector

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/blezek/lapdog/internal/classify"
	"github.com/blezek/lapdog/internal/sessionyaml"
	"github.com/blezek/lapdog/internal/store"
)

// Segment is the collector's in-memory state for one session segment: one entry
// in SessionInfo.Sessions for one subsession.
//
// It accumulates until a flush writes it to the database, which is why the
// counters live here rather than being read back from SQLite each poll.
type Segment struct {
	Key          string
	SubsessionID int
	SessionNum   int

	StartedAt time.Time
	endedAt   *time.Time

	Acct  *Accountant
	Class classify.Result

	// StoreID is the database primary key once the segment has been flushed at
	// least once. Zero means never written.
	StoreID int64

	leagueID int
	seriesID int
	seasonID int
	official int

	trackID       *int
	trackName     *string
	trackConfig   *string
	trackLengthKm *float64

	carID        *int
	carName      *string
	carClassID   *int
	carClassName *string

	lapsCompleted int
	bestLapTimeS  *float64

	incidents      int
	incidentIsLive bool

	startingPosition     *int
	finishPosition       *int
	finishClassPosition  *int
	qualifyPosition      *int
	qualifyClassPosition *int
	qualifyBestTimeS     *float64
	fieldSize            *int

	captureFile    *string
	identity       sessionyaml.Identity
	ratingCategory *string
	sourceJSON     string
}

// NewSegment starts tracking a session segment.
func NewSegment(info *sessionyaml.Info, sessionNum int, startedAt time.Time, interval time.Duration) *Segment {
	g := &Segment{
		SessionNum: sessionNum,
		StartedAt:  startedAt,
		Acct:       NewAccountant(interval),
	}
	if info != nil {
		g.SubsessionID = info.WeekendInfo.SubSessionID
	}
	g.Key = store.SessionKey(g.SubsessionID, sessionNum, startedAt)
	g.ApplyInfo(info)
	return g
}

// SetCaptureFile records which capture file this segment is being written to.
func (g *Segment) SetCaptureFile(name string) { g.captureFile = &name }

// Resume carries accumulated facts into a later connection to the same online
// session. The new segment's timestamps remain its own so its capture filename
// is unique; UpsertSession keeps the session row's earliest start time.
func (g *Segment) Resume(existing *store.Session) {
	if existing == nil {
		return
	}
	g.Acct.Connected = existing.ConnectedSeconds
	g.Acct.InCar = existing.InCarSeconds
	g.Acct.Driving = existing.DrivingSeconds
	g.lapsCompleted = existing.LapsCompleted
	g.incidents = existing.Incidents
	g.bestLapTimeS = existing.BestLapTimeS
	g.startingPosition = existing.StartingPosition
	g.finishPosition = existing.FinishPosition
	g.finishClassPosition = existing.FinishClassPosition
	g.qualifyPosition = existing.QualifyPosition
	g.qualifyClassPosition = existing.QualifyClassPosition
	g.qualifyBestTimeS = existing.QualifyBestTimeS
	g.fieldSize = existing.FieldSize
}

// ApplyInfo refreshes everything derived from the session YAML.
//
// This runs on every YAML change rather than once at session start, because
// QualifyResultsInfo only populates after qualifying has run and ResultsPositions
// only fills in as the session concludes.
func (g *Segment) ApplyInfo(info *sessionyaml.Info) {
	if info == nil {
		return
	}
	g.Class = classify.Classify(info, g.SessionNum)

	w := info.WeekendInfo
	g.leagueID, g.seriesID, g.seasonID, g.official = w.LeagueID, w.SeriesID, w.SeasonID, w.Official
	g.ratingCategory = strPtrIfSet(w.Category)

	g.trackID = intPtr(w.TrackID)
	g.trackName = strPtrIfSet(w.TrackDisplayName)
	g.trackConfig = strPtrIfSet(w.TrackConfigName)
	if km := info.TrackLengthKm(); km > 0 {
		g.trackLengthKm = &km
	}

	if me, ok := info.Me(); ok {
		g.carID = intPtr(me.CarID)
		g.carName = strPtrIfSet(me.CarScreenName)
		g.carClassID = intPtr(me.CarClassID)
		g.carClassName = strPtrIfSet(me.CarClassShortName)
	}

	if r, ok := info.MyResult(g.SessionNum); ok {
		g.finishPosition = intPtr(r.Position)
		g.finishClassPosition = intPtr(r.ClassPosition)
		// The YAML incident count is only authoritative when no live variable is
		// available; otherwise it would clobber a fresher value.
		if !g.incidentIsLive {
			g.incidents = r.Incidents
		}
	}
	if q, ok := info.MyQualifyResult(); ok {
		g.qualifyPosition = intPtr(q.Position)
		g.qualifyClassPosition = intPtr(q.ClassPosition)
		if q.FastestTime > 0 {
			t := q.FastestTime
			g.qualifyBestTimeS = &t
		}
	}
	if n := info.FieldSize(g.SessionNum); n > 0 {
		g.fieldSize = &n
	}

	// Who is driving, and where their ratings stand as of this document. Both ratings
	// move after almost every official race, so they are recorded per session rather
	// than as a single current value — that is what turns them into a progression.
	g.identity = info.MyIdentity()

	if raw, err := classifySourceJSON(info); err == nil {
		g.sourceJSON = raw
	}
}

// SetIncidentSource records whether the live incident variable is in use.
func (g *Segment) SetIncidentSource(live bool) { g.incidentIsLive = live }

// NoteIncidents records an incident count read from the live variable.
//
// The count is treated as monotonic within a session: the sim's own counter only
// ever rises, so a lower reading means the variable was momentarily unavailable —
// which happens on frames where the car is not under physics — rather than that
// incidents were forgiven. Taking the maximum keeps a garage visit from wiping
// the session's incident total.
func (g *Segment) NoteIncidents(n int) {
	if n > g.incidents {
		g.incidents = n
	}
}

// Incidents returns the running incident count, which the lap detector needs to
// attribute incidents to the lap they happened on.
func (g *Segment) Incidents() int { return g.incidents }

// NoteLap records a completed lap, tracking the count and the session best.
func (g *Segment) NoteLap(lapNumber int, lapTimeS float64) {
	g.lapsCompleted++
	// A zero or negative time is not a usable lap time, but the lap still happened
	// and still counts toward the total.
	if lapTimeS <= 0 {
		return
	}
	if g.bestLapTimeS == nil || lapTimeS < *g.bestLapTimeS {
		t := lapTimeS
		g.bestLapTimeS = &t
	}
}

// SetLapsCompleted reconciles the in-memory counter with the store after an
// idempotent lap insert. A reconnect can observe a lap already in SQLite, so
// blindly incrementing would make the session total exceed its lap rows.
func (g *Segment) SetLapsCompleted(n int) { g.lapsCompleted = n }

// BestLapTimeS returns the session best so far, for lap delta computation.
func (g *Segment) BestLapTimeS() (float64, bool) {
	if g.bestLapTimeS == nil {
		return 0, false
	}
	return *g.bestLapTimeS, true
}

// NoteStartingPosition records the grid position at the green flag.
//
// Only the first call takes effect, since the grid slot is set once. It is stored
// separately from the qualifying position because the two diverge after a pit-lane
// start or a grid penalty.
func (g *Segment) NoteStartingPosition(p int) {
	if g.startingPosition == nil && p > 0 {
		g.startingPosition = intPtr(p)
	}
}

// End marks the segment finished.
func (g *Segment) End(at time.Time) {
	t := at
	g.endedAt = &t
}

// IsRace reports whether position events should be recorded.
func (g *Segment) IsRace() bool { return g.Class.SessionType == classify.TypeRace }

// TooShort reports whether the segment is below the minimum recordable length,
// which drops accidental joins.
func (g *Segment) TooShort(min time.Duration) bool {
	return g.Acct.Connected < min.Seconds()
}

// Label renders the segment's classification the way the interface shows it.
func (g *Segment) Label() string {
	return classify.Label(g.Class.SessionType, g.Class.EventContext)
}

// ToStore renders the segment as a database row.
func (g *Segment) ToStore() *store.Session {
	rec := &store.Session{
		ID:           g.StoreID,
		SessionKey:   g.Key,
		SubsessionID: g.SubsessionID,
		SessionNum:   g.SessionNum,
		SessionType:  string(g.Class.SessionType),
		EventContext: string(g.Class.EventContext),

		LeagueID: g.leagueID,
		SeriesID: g.seriesID,
		SeasonID: g.seasonID,
		Official: g.official,

		TrackID:       g.trackID,
		TrackName:     g.trackName,
		TrackConfig:   g.trackConfig,
		TrackLengthKm: g.trackLengthKm,

		CarID:        g.carID,
		CarName:      g.carName,
		CarClassID:   g.carClassID,
		CarClassName: g.carClassName,

		StartedAt: store.FormatTime(g.StartedAt),

		ConnectedSeconds: g.Acct.Connected,
		InCarSeconds:     g.Acct.InCar,
		DrivingSeconds:   g.Acct.Driving,

		LapsCompleted: g.lapsCompleted,
		Incidents:     g.incidents,
		BestLapTimeS:  g.bestLapTimeS,

		StartingPosition:     g.startingPosition,
		FinishPosition:       g.finishPosition,
		FinishClassPosition:  g.finishClassPosition,
		QualifyPosition:      g.qualifyPosition,
		QualifyClassPosition: g.qualifyClassPosition,
		QualifyBestTimeS:     g.qualifyBestTimeS,
		FieldSize:            g.fieldSize,

		AIOpponentCount: g.Class.AIOpponentCount,

		DriverUserID:         g.identity.UserID,
		DriverIRating:        g.identity.IRating,
		DriverLicString:      g.identity.LicString,
		DriverLicLevel:       g.identity.LicLevel,
		DriverLicSubLevel:    g.identity.LicSubLevel,
		DriverSafetyRating:   g.identity.SafetyRating,
		DriverRatingCategory: g.ratingCategory,

		ClassifySourceJSON: g.sourceJSON,
		CaptureFile:        g.captureFile,
	}

	detection := string(g.Class.AIDetection)
	rec.AIDetection = &detection

	if g.incidentIsLive {
		rec.IncidentSource = "live"
	} else {
		rec.IncidentSource = "yaml"
	}
	if g.endedAt != nil {
		s := store.FormatTime(*g.endedAt)
		rec.EndedAt = &s
	}
	return rec
}

// classifySourceJSON serialises the YAML subset the classification was derived
// from.
//
// The full Drivers array is included, not just the local driver, so AI detection can
// be re-derived from it. Storing only the player's entry would make re-classification
// impossible for exactly the case the provenance exists to fix.
func classifySourceJSON(info *sessionyaml.Info) (string, error) {
	if info == nil {
		return "{}", nil
	}
	b, err := json.Marshal(info)
	if err != nil {
		return "{}", fmt.Errorf("collector: marshal classification source: %w", err)
	}
	return string(b), nil
}

// intPtr returns a pointer to v, or nil when v is zero, so "absent" and "zero"
// stay distinguishable in the database.
func intPtr(v int) *int {
	if v == 0 {
		return nil
	}
	return &v
}

// strPtrIfSet returns a pointer to v, or nil when v is empty.
func strPtrIfSet(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
