// Per-entity queries: the aggregates behind the Cars and Tracks pages.
//
// Every function here takes a dimension rather than existing twice, so the
// arithmetic — which has several traps in it — is written once. The dimension is
// an allowlist key and never interpolated user input; see entityDim.
package store

import "fmt"

// entityDim maps a dimension name onto the columns it selects.
//
// Two columns are needed rather than one: the id identifies the entity and the
// name labels it, and the *other* dimension is what a per-entity breakdown groups
// by. A car page shows rows per track, and vice versa.
type entityDim struct {
	idCol     string // sessions column holding the entity id
	nameExpr  string // display name for the entity
	otherID   string // the opposite dimension's id column
	otherExpr string // the opposite dimension's display name
}

var entityDims = map[string]entityDim{
	"car": {
		idCol:     "s.car_id",
		nameExpr:  "COALESCE(s.car_name, 'Unknown car')",
		otherID:   "s.track_id",
		otherExpr: "COALESCE(s.track_name, 'Unknown track')",
	},
	"track": {
		idCol:     "s.track_id",
		nameExpr:  "COALESCE(s.track_name, 'Unknown track')",
		otherID:   "s.car_id",
		otherExpr: "COALESCE(s.car_name, 'Unknown car')",
	},
}

// consistencyMinLaps is how many laps a session needs, after exclusions, before its
// consistency is measured.
//
// A single lap is trivially 100% consistent, which is a lie, and one false figure
// discredits every honest number beside it. Sessions below this are dropped from
// the average rather than contributing a noisy value.
const consistencyMinLaps = 5

// consistencyOutlierFactor is the multiple of a session's own best lap beyond which
// a lap is treated as an outlier and excluded.
//
// Chosen here rather than derived: Simresults publishes the consistency formula but
// not its outlier rule. Motorsport's 107% rule is the precedent, loosened because a
// lap held up in traffic is normal in a race rather than exceptional.
const consistencyOutlierFactor = 1.10

// EntityDimensions returns the allowlisted dimensions.
func EntityDimensions() []string {
	out := make([]string, 0, len(entityDims))
	for k := range entityDims {
		out = append(out, k)
	}
	return out
}

// dimOrErr resolves a dimension name, refusing anything not allowlisted.
//
// Returning an error rather than defaulting matters: a typo in a query string
// would otherwise silently answer a different question than the one asked.
func dimOrErr(by string) (entityDim, error) {
	d, ok := entityDims[by]
	if !ok {
		return entityDim{}, fmt.Errorf("%w: %q", ErrBadGroupBy, by)
	}
	return d, nil
}

// EntityRow is one entry in the left-hand list of the Cars or Tracks page.
type EntityRow struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	DrivingHours float64 `json:"drivingHours"`
	Sessions     int     `json:"sessions"`
	Laps         int     `json:"laps"`
}

// EntityList returns every car or track in the filtered range, most-driven first.
//
// Laps come from sessions.laps_completed rather than from counting lap rows. That
// is deliberate: joining laps here would multiply driving_seconds by the lap count,
// and the session's own counter is the authoritative figure anyway.
func (s *Store) EntityList(f Filter, by string) ([]EntityRow, error) {
	d, err := dimOrErr(by)
	if err != nil {
		return nil, err
	}
	pred, args := f.where()

	// The name is aggregated with MAX rather than added to GROUP BY. iRacing
	// renames cars and track configurations between seasons, so two sessions can
	// share an id while carrying different names; grouping by the name as well
	// would split that one entity into two rows, each with half its hours —
	// a wrong total, not just a cosmetic one. MAX picks a single name
	// deterministically (the same one on every run against the same data), and
	// which of the two names wins is arbitrary but consistent — that is
	// preferred to a row split with divided totals.
	q := `
SELECT ` + d.idCol + `,
       MAX(` + d.nameExpr + `),
       SUM(s.driving_seconds) / 3600.0,
       COUNT(*),
       SUM(s.laps_completed)
FROM sessions s
WHERE ` + pred + ` AND ` + d.idCol + ` IS NOT NULL
GROUP BY ` + d.idCol + `
ORDER BY SUM(s.driving_seconds) DESC, MAX(` + d.nameExpr + `)`

	rows, err := s.reader.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: entity list by %s: %w", by, err)
	}
	defer rows.Close()

	out := []EntityRow{}
	for rows.Next() {
		var r EntityRow
		if err := rows.Scan(&r.ID, &r.Name, &r.DrivingHours, &r.Sessions, &r.Laps); err != nil {
			return nil, fmt.Errorf("store: scan entity row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EntityStats is the headline panel for one car or track.
//
// Only quantities comparable across the opposite dimension appear here. Lap times
// do not: a car's best lap at one track and at another are not the same quantity,
// so pace lives in EntityPace, per row.
//
// The rate fields are pointers because they have no meaning at zero exposure.
// Rendering "0.00 points per 100 km" for a car that has never been driven claims
// something the data does not support; an em dash does not.
type EntityStats struct {
	ID   int    `json:"id"`
	Name string `json:"name"`

	DrivingHours   float64 `json:"drivingHours"`
	InCarHours     float64 `json:"inCarHours"`
	ConnectedHours float64 `json:"connectedHours"`

	Sessions   int     `json:"sessions"`
	Laps       int     `json:"laps"`
	DistanceKm float64 `json:"distanceKm"`

	IncidentPoints         int      `json:"incidentPoints"`
	IncidentPointsPer100Km *float64 `json:"incidentPointsPer100Km"`
	CleanLapPct            *float64 `json:"cleanLapPct"`

	// CleanLaps and TimedLaps are the numerator and denominator behind
	// CleanLapPct: laps with no incident, over timed non-pit laps. They are
	// surfaced so the interface can show the fraction beside the percentage rather
	// than a bare figure, and so the two cannot disagree. Both are zero, and
	// CleanLapPct nil, when there are no timed laps.
	CleanLaps int `json:"cleanLaps"`
	TimedLaps int `json:"timedLaps"`

	// Races counts race sessions with a recorded finish position, not every
	// session with SessionType "Race". That keeps one denominator across all
	// four result metrics: Wins, Podiums, and AvgPositionsGained are each
	// counted or averaged over the same set of races, so a wins-per-race ratio
	// means what it says. The cost is that a race with no recorded finish (a
	// DNF the sim never logged a position for) is excluded from Races too,
	// rather than inflating the denominator with a race that contributes to
	// none of the other three counts.
	Races              int      `json:"races"`
	Wins               int      `json:"wins"`
	Podiums            int      `json:"podiums"`
	AvgPositionsGained *float64 `json:"avgPositionsGained"`
}

// EntityStats returns the headline figures for one entity.
//
// The session-level and lap-level aggregates are two separate subqueries joined on
// the entity id. Combining them into one aggregate would multiply every session sum
// by that session's lap count: measured on the development database, that reported
// 28,895 driving hours against a true 1,242.6, which looks like a real number.
func (s *Store) EntityStats(f Filter, by string, id int) (EntityStats, error) {
	d, err := dimOrErr(by)
	if err != nil {
		return EntityStats{}, err
	}
	pred, args := f.where()

	// Session-level aggregate. Distance uses the session's own lap counter and the
	// track length, so it needs no lap rows.
	//
	// Every aggregate is wrapped in COALESCE, and the name in MAX, because this query
	// always returns exactly one row: for an id matching nothing, every SUM is NULL
	// and the name would fail to scan into a string before the zero check below could
	// report the id as absent.
	sessQ := `
SELECT COALESCE(MAX(` + d.nameExpr + `), ''),
       COALESCE(SUM(s.driving_seconds), 0) / 3600.0,
       COALESCE(SUM(s.in_car_seconds), 0) / 3600.0,
       COALESCE(SUM(s.connected_seconds), 0) / 3600.0,
       COUNT(*),
       COALESCE(SUM(s.laps_completed), 0),
       COALESCE(SUM(s.laps_completed * COALESCE(s.track_length_km, 0)), 0),
       COALESCE(SUM(s.incidents), 0),
       COALESCE(SUM(CASE WHEN s.session_type = 'Race' AND s.finish_position > 0 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN s.session_type = 'Race' AND s.finish_position = 1 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN s.session_type = 'Race' AND s.finish_position BETWEEN 1 AND 3 THEN 1 ELSE 0 END), 0),
       AVG(CASE WHEN s.session_type = 'Race' AND s.starting_position > 0 AND s.finish_position > 0
                THEN s.starting_position - s.finish_position END)
FROM sessions s
WHERE ` + pred + ` AND ` + d.idCol + ` = ?`

	var out EntityStats
	out.ID = id
	var gained *float64
	err = s.reader.QueryRow(sessQ, append(args, id)...).Scan(
		&out.Name, &out.DrivingHours, &out.InCarHours, &out.ConnectedHours,
		&out.Sessions, &out.Laps, &out.DistanceKm, &out.IncidentPoints,
		&out.Races, &out.Wins, &out.Podiums, &gained,
	)
	if err != nil {
		return EntityStats{}, fmt.Errorf("store: entity stats by %s: %w", by, err)
	}
	if out.Sessions == 0 {
		return EntityStats{}, fmt.Errorf("%w: %s id %d", ErrNotFound, by, id)
	}
	out.AvgPositionsGained = gained

	// Lap-level aggregate, separately. Pit laps are excluded because an in-lap is
	// not an attempt at a clean flying lap.
	lapQ := `
SELECT COALESCE(COUNT(*), 0),
       COALESCE(SUM(CASE WHEN l.incidents_on_lap = 0 THEN 1 ELSE 0 END), 0)
FROM laps l
JOIN sessions s ON s.id = l.session_id
WHERE ` + pred + ` AND ` + d.idCol + ` = ?
  AND l.lap_time_s > 0 AND l.is_pit_lap = 0`

	var timed, clean int
	if err := s.reader.QueryRow(lapQ, append(args, id)...).Scan(&timed, &clean); err != nil {
		return EntityStats{}, fmt.Errorf("store: entity lap stats by %s: %w", by, err)
	}
	out.TimedLaps = timed
	out.CleanLaps = clean
	if timed > 0 {
		pct := 100.0 * float64(clean) / float64(timed)
		out.CleanLapPct = &pct
	}
	if out.DistanceKm > 0 {
		rate := 100.0 * float64(out.IncidentPoints) / out.DistanceKm
		out.IncidentPointsPer100Km = &rate
	}
	return out, nil
}

// PaceRow is one row of the pace table: a track on a car's page, or a car on a
// track's page.
//
// Every pace field is a pointer. A track where the driver has time but no completed
// timed lap is a real state, and zero is not the same as absent.
type PaceRow struct {
	OtherID   int    `json:"otherId"`
	OtherName string `json:"otherName"`

	PersonalBestS *float64 `json:"personalBestS"`
	BestInRangeS  *float64 `json:"bestInRangeS"`

	Laps     int `json:"laps"`
	Sessions int `json:"sessions"`

	ConsistencyPct    *float64 `json:"consistencyPct"`
	ConsistencyDeltaS *float64 `json:"consistencyDeltaS"`
}

// EntityPace returns one row per value of the opposite dimension.
//
// Lap times are only comparable within a car and track pair, which is why the car
// page has no single headline lap time and this table exists instead.
//
// The personal best deliberately ignores the filter's date range while everything
// else respects it. A personal best is a fact about the driver's whole history, and
// the comparison against the in-range best is what shows current form.
func (s *Store) EntityPace(f Filter, by string, id int) ([]PaceRow, error) {
	d, err := dimOrErr(by)
	if err != nil {
		return nil, err
	}
	pred, args := f.where()

	// The all-time filter keeps every predicate except the date bounds, so that
	// session-type and context filters still apply to the personal best.
	allTime := f
	allTime.From = ""
	allTime.To = ""
	pbPred, pbArgs := allTime.where()

	q := `
WITH sess AS (
  SELECT ` + d.otherID + ` AS oid,
         MAX(` + d.otherExpr + `) AS oname,
         COUNT(*)                    AS sessions,
         SUM(s.laps_completed)       AS laps
  FROM sessions s
  WHERE ` + pred + ` AND ` + d.idCol + ` = ? AND ` + d.otherID + ` IS NOT NULL
  GROUP BY ` + d.otherID + `
),
inrange AS (
  SELECT ` + d.otherID + ` AS oid, MIN(l.lap_time_s) AS best
  FROM laps l JOIN sessions s ON s.id = l.session_id
  WHERE ` + pred + ` AND ` + d.idCol + ` = ?
    AND l.lap_time_s > 0 AND l.is_pit_lap = 0
  GROUP BY ` + d.otherID + `
),
alltime AS (
  SELECT ` + d.otherID + ` AS oid, MIN(l.lap_time_s) AS best
  FROM laps l JOIN sessions s ON s.id = l.session_id
  WHERE ` + pbPred + ` AND ` + d.idCol + ` = ?
    AND l.lap_time_s > 0 AND l.is_pit_lap = 0
  GROUP BY ` + d.otherID + `
),
-- Consistency is computed within each session and only then averaged.
--
-- Pooling every lap a car has ever done at a track measures something else: it
-- conflates repeatability with improvement over time and with varying conditions.
-- Measured on the development database the two forms differ by about 2.5
-- percentage points, enough to move a driver across the 98% threshold.
lap AS (
  SELECT s.id AS sid,
         ` + d.otherID + ` AS oid,
         l.lap_time_s AS t,
         ROW_NUMBER() OVER (PARTITION BY s.id ORDER BY l.lap_number) AS rn
  FROM laps l JOIN sessions s ON s.id = l.session_id
  WHERE ` + pred + ` AND ` + d.idCol + ` = ?
    AND l.lap_time_s > 0 AND l.is_pit_lap = 0
),
-- The first timed lap of a session starts from a standing or rolling start and is
-- not an attempt at a representative lap.
flying AS (SELECT * FROM lap WHERE rn > 1),
sbest AS (SELECT sid, MIN(t) AS best FROM flying GROUP BY sid),
kept AS (
  SELECT fl.sid, fl.oid, fl.t, sb.best
  FROM flying fl JOIN sbest sb ON sb.sid = fl.sid
  WHERE fl.t <= sb.best * ?
),
persession AS (
  SELECT sid, oid, MIN(best) AS best,
         AVG(CASE WHEN t > best THEN t END) AS mean_others,
         COUNT(*) AS n
  FROM kept
  GROUP BY sid, oid
  HAVING COUNT(*) >= ? AND AVG(CASE WHEN t > best THEN t END) IS NOT NULL
),
cons AS (
  SELECT oid,
         AVG(100.0 * best / mean_others) AS pct,
         AVG(mean_others - best)         AS delta
  FROM persession GROUP BY oid
)
SELECT sess.oid, sess.oname, alltime.best, inrange.best,
       COALESCE(sess.laps, 0), sess.sessions,
       cons.pct, cons.delta
FROM sess
LEFT JOIN inrange ON inrange.oid = sess.oid
LEFT JOIN alltime ON alltime.oid = sess.oid
LEFT JOIN cons    ON cons.oid    = sess.oid
ORDER BY sess.laps DESC, sess.oname`

	// Argument order follows the CTEs: filtered predicate, filtered predicate,
	// all-time predicate, filtered predicate (for the consistency lap CTE) — each
	// followed by the entity id — then the outlier factor and the lap minimum.
	qargs := make([]any, 0, len(args)*3+len(pbArgs)+6)
	qargs = append(qargs, args...)
	qargs = append(qargs, id)
	qargs = append(qargs, args...)
	qargs = append(qargs, id)
	qargs = append(qargs, pbArgs...)
	qargs = append(qargs, id)
	qargs = append(qargs, args...)
	qargs = append(qargs, id, consistencyOutlierFactor, consistencyMinLaps)

	rows, err := s.reader.Query(q, qargs...)
	if err != nil {
		return nil, fmt.Errorf("store: entity pace by %s: %w", by, err)
	}
	defer rows.Close()

	out := []PaceRow{}
	for rows.Next() {
		var r PaceRow
		if err := rows.Scan(&r.OtherID, &r.OtherName, &r.PersonalBestS,
			&r.BestInRangeS, &r.Laps, &r.Sessions,
			&r.ConsistencyPct, &r.ConsistencyDeltaS); err != nil {
			return nil, fmt.Errorf("store: scan pace row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProgressionRow is one month's best lap, for the improvement chart.
type ProgressionRow struct {
	Month    string  `json:"month"`
	BestLapS float64 `json:"bestLapS"`
	Laps     int     `json:"laps"`
}

// EntityProgression returns the best lap per calendar month for one entity at one
// value of the opposite dimension.
//
// Both ids are required because a line mixing tracks would be meaningless: it would
// rise and fall with which circuit was driven rather than with the driver's pace.
// This is the closest LapDog gets to the reference-lap comparison that coaching
// tools are built on, the reference being the driver's own earlier self.
func (s *Store) EntityProgression(f Filter, by string, id, otherID int) ([]ProgressionRow, error) {
	d, err := dimOrErr(by)
	if err != nil {
		return nil, err
	}
	pred, args := f.where()

	q := `
SELECT substr(s.started_at, 1, 7) AS month,
       MIN(l.lap_time_s),
       COUNT(*)
FROM laps l JOIN sessions s ON s.id = l.session_id
WHERE ` + pred + ` AND ` + d.idCol + ` = ? AND ` + d.otherID + ` = ?
  AND l.lap_time_s > 0 AND l.is_pit_lap = 0
GROUP BY month
ORDER BY month`

	rows, err := s.reader.Query(q, append(append(args, id), otherID)...)
	if err != nil {
		return nil, fmt.Errorf("store: entity progression by %s: %w", by, err)
	}
	defer rows.Close()

	out := []ProgressionRow{}
	for rows.Next() {
		var r ProgressionRow
		if err := rows.Scan(&r.Month, &r.BestLapS, &r.Laps); err != nil {
			return nil, fmt.Errorf("store: scan progression row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RivalRow is a head-to-head record against one named opponent.
type RivalRow struct {
	Name       string `json:"name"`
	PassedThem int    `json:"passedThem"`
	LostTo     int    `json:"lostTo"`
	Net        int    `json:"net"`
}

// Rivals returns per-opponent pass records, most-contested first.
//
// Only CauseOnTrack counts. Inheriting a place because the other car pitted or left
// the world is not overtaking, and counting it would flatter the record.
//
// Opponent names are returned as recorded. Other drivers are never anonymised: a
// race is not a race without who was in it.
//
// Grouping is by opponent_name, not by an opponent id, because position_events
// has no stable identifier for the other car — see EntityList for the same
// tradeoff on the entity side. That means a driver who changes their display
// name mid-season splits into two rows here, each with half the real record, and
// two different drivers who happen to share a display name merge into one row
// that overstates how contested either rivalry actually is.
func (s *Store) Rivals(f Filter) ([]RivalRow, error) {
	pred, args := f.where()

	q := `
SELECT pe.opponent_name,
       SUM(CASE WHEN pe.to_position < pe.from_position THEN 1 ELSE 0 END) AS passed,
       SUM(CASE WHEN pe.to_position > pe.from_position THEN 1 ELSE 0 END) AS lost
FROM position_events pe
JOIN sessions s ON s.id = pe.session_id
WHERE ` + pred + `
  AND pe.cause = ?
  AND pe.opponent_name IS NOT NULL AND pe.opponent_name <> ''
GROUP BY pe.opponent_name
ORDER BY (passed + lost) DESC, pe.opponent_name`

	rows, err := s.reader.Query(q, append(args, string(CauseOnTrack))...)
	if err != nil {
		return nil, fmt.Errorf("store: rivals: %w", err)
	}
	defer rows.Close()

	out := []RivalRow{}
	for rows.Next() {
		var r RivalRow
		if err := rows.Scan(&r.Name, &r.PassedThem, &r.LostTo); err != nil {
			return nil, fmt.Errorf("store: scan rival row: %w", err)
		}
		r.Net = r.PassedThem - r.LostTo
		out = append(out, r)
	}
	return out, rows.Err()
}

// Racecraft is the overtaking and results record for one entity.
//
// The position figures are pointers because they have no meaning with no races in
// the range: rendering "grid 0.0 → finish 0.0" would claim a driver started and
// finished every race on the same non-existent position, which is a much stronger
// statement than "no races here".
//
// Only Races is a count of sessions; PassesMade and TimesPassed count events. The
// two are deliberately not divided by one another here — a passes-per-race ratio
// is the interface's business, and it can only be honest while both numerators
// share the Races denominator, which they do not: a race with no recorded finish
// still contributes its on-track passes.
type Racecraft struct {
	PassesMade  int `json:"passesMade"`
	TimesPassed int `json:"timesPassed"`

	// Races counts race sessions with both a grid and a finish position, which is
	// the set the two averages below are taken over. A race the sim never logged
	// positions for cannot contribute to either average, so counting it here would
	// make the denominator disagree with the numbers beside it.
	Races             int      `json:"races"`
	AvgStartPosition  *float64 `json:"avgStartPosition"`
	AvgFinishPosition *float64 `json:"avgFinishPosition"`
}

// Racecraft returns the pass record and the average grid-to-finish for one entity.
//
// Two separate queries, deliberately. position_events is per event and sessions is
// per session, and joining them into one aggregate would multiply each session's
// positions by its number of position changes — the same fan-out that reported
// 28,895 driving hours against a true 1,242.6 when laps were joined into
// EntityStats. See the note on EntityStats.
//
// Both halves are restricted to race sessions. A practice pass is not racecraft,
// and the averages are only defined for a race in the first place.
//
// AI races are *not* excluded here. The caller decides, via Filter.ExcludeAI, the
// way the dashboard's grid-to-finish panel does: exclude_ai is a user-visible
// checkbox that starts off, so hard-coding the exclusion inside the query would
// make the parameter a lie. The spec calls these figures human-only, and the
// frontend panel therefore passes ExcludeAI explicitly.
func (s *Store) Racecraft(f Filter, by string, id int) (Racecraft, error) {
	d, err := dimOrErr(by)
	if err != nil {
		return Racecraft{}, err
	}
	pred, args := f.where()

	var out Racecraft

	// Event-level: only CauseOnTrack counts. Inheriting a place because the other
	// car pitted or left the world is not overtaking, and counting it would
	// flatter the record. This matches Totals, which the dashboard uses.
	evQ := `
SELECT COALESCE(SUM(CASE WHEN pe.to_position < pe.from_position THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN pe.to_position > pe.from_position THEN 1 ELSE 0 END), 0)
FROM position_events pe
JOIN sessions s ON s.id = pe.session_id
WHERE ` + pred + ` AND ` + d.idCol + ` = ?
  AND s.session_type = 'Race'
  AND pe.cause = ?`

	evArgs := append(append([]any{}, args...), id, string(CauseOnTrack))
	if err := s.reader.QueryRow(evQ, evArgs...).Scan(&out.PassesMade, &out.TimesPassed); err != nil {
		return Racecraft{}, fmt.Errorf("store: racecraft passes by %s: %w", by, err)
	}

	// Session-level, separately. AVG over no rows is NULL, which is what leaves the
	// two averages nil rather than zero.
	sessQ := `
SELECT COUNT(*),
       AVG(s.starting_position),
       AVG(s.finish_position)
FROM sessions s
WHERE ` + pred + ` AND ` + d.idCol + ` = ?
  AND s.session_type = 'Race'
  AND s.starting_position > 0 AND s.finish_position > 0`

	sessArgs := append(append([]any{}, args...), id)
	if err := s.reader.QueryRow(sessQ, sessArgs...).Scan(
		&out.Races, &out.AvgStartPosition, &out.AvgFinishPosition,
	); err != nil {
		return Racecraft{}, fmt.Errorf("store: racecraft positions by %s: %w", by, err)
	}
	return out, nil
}

// QualiPace is how much slower race pace is than qualifying pace.
//
// AvgDeltaS is nil rather than zero when no weekend has both sessions. Zero would
// claim the two are identical, which is a much stronger statement than "unknown".
type QualiPace struct {
	Pairs     int      `json:"pairs"`
	AvgDeltaS *float64 `json:"avgDeltaS"`
}

// QualifyingVsRace returns the mean of (race best lap - qualifying best lap).
//
// The two sessions are paired on subsession_id, which is what keeps a qualifying
// session at one track from being compared against a race at another: a subsession
// is one event at one track in one car.
//
// A positive delta is the normal direction. Qualifying is run on low fuel with a
// clear track, so race pace is expected to be slower; a negative value is worth
// noticing rather than assuming to be a bug.
func (s *Store) QualifyingVsRace(f Filter, by string, id int) (QualiPace, error) {
	d, err := dimOrErr(by)
	if err != nil {
		return QualiPace{}, err
	}
	pred, args := f.where()

	q := `
WITH q AS (
  SELECT s.subsession_id AS ss, MIN(s.qualify_best_time_s) AS qt
  FROM sessions s
  WHERE ` + pred + ` AND ` + d.idCol + ` = ?
    AND s.session_type = 'Qualify' AND s.qualify_best_time_s > 0
    AND s.subsession_id <> 0
  GROUP BY s.subsession_id
),
r AS (
  SELECT s.subsession_id AS ss, MIN(s.best_lap_time_s) AS rt
  FROM sessions s
  WHERE ` + pred + ` AND ` + d.idCol + ` = ?
    AND s.session_type = 'Race' AND s.best_lap_time_s > 0
    AND s.subsession_id <> 0
  GROUP BY s.subsession_id
)
SELECT COUNT(*), AVG(r.rt - q.qt)
FROM q JOIN r ON r.ss = q.ss`

	qargs := make([]any, 0, len(args)*2+2)
	qargs = append(qargs, args...)
	qargs = append(qargs, id)
	qargs = append(qargs, args...)
	qargs = append(qargs, id)

	var out QualiPace
	if err := s.reader.QueryRow(q, qargs...).Scan(&out.Pairs, &out.AvgDeltaS); err != nil {
		return QualiPace{}, fmt.Errorf("store: qualifying vs race by %s: %w", by, err)
	}
	return out, nil
}

// defaultComboLimit is how many car-and-track pairings the heatmap shows.
const defaultComboLimit = 10

// ComboCell is one cell of the car-and-track heatmap: a pairing, a session
// category, and the hours spent.
//
// ComboHours repeats the pairing's total on every one of its cells. That is
// redundant by design: it lets the frontend order rows without re-summing, and it
// means the ordering the store chose cannot disagree with the ordering the
// interface draws.
type ComboCell struct {
	Combo      string  `json:"combo"`
	Category   string  `json:"category"`
	Hours      float64 `json:"hours"`
	ComboHours float64 `json:"comboHours"`
}

// TopCombos returns the busiest car-and-track pairings, split by session category.
//
// The filter applies to both the ranking and the accumulated time, so narrowing the
// range answers "where did my hours go lately" rather than always reporting
// all-time totals. A category filter therefore also narrows the columns, which is
// consistent with every other panel on the dashboard.
//
// Sessions with no car or no track are excluded: a pairing is the unit here, and
// half of one is not a row.
func (s *Store) TopCombos(f Filter, limit int) ([]ComboCell, error) {
	if limit <= 0 {
		limit = defaultComboLimit
	}
	pred, args := f.where()

	// The label is wrapped in MAX rather than added to GROUP BY, for the same
	// reason as EntityList: iRacing renames cars and track configurations between
	// seasons, so two sessions can share a (car_id, track_id) pair while carrying
	// different names. Grouping by the label as well would split one pairing into
	// two rows, each with half its hours. MAX picks a single label
	// deterministically; which of the two labels wins is arbitrary but consistent.
	q := `
WITH combo AS (
  SELECT s.car_id AS ci, s.track_id AS ti,
         MAX(COALESCE(s.car_name, 'Unknown car') || ' / ' ||
             COALESCE(s.track_name, 'Unknown track')) AS label,
         SUM(s.driving_seconds) AS tot
  FROM sessions s
  WHERE ` + pred + ` AND s.car_id IS NOT NULL AND s.track_id IS NOT NULL
  GROUP BY s.car_id, s.track_id
  ORDER BY tot DESC
  LIMIT ?
)
SELECT c.label,
       s.session_type || '/' || s.event_context AS category,
       SUM(s.driving_seconds) / 3600.0,
       c.tot / 3600.0
FROM sessions s
JOIN combo c ON c.ci = s.car_id AND c.ti = s.track_id
WHERE ` + pred + `
GROUP BY c.label, category, c.tot
ORDER BY c.tot DESC, category`

	qargs := make([]any, 0, len(args)*2+1)
	qargs = append(qargs, args...)
	qargs = append(qargs, limit)
	qargs = append(qargs, args...)

	rows, err := s.reader.Query(q, qargs...)
	if err != nil {
		return nil, fmt.Errorf("store: top combos: %w", err)
	}
	defer rows.Close()

	out := []ComboCell{}
	for rows.Next() {
		var c ComboCell
		if err := rows.Scan(&c.Combo, &c.Category, &c.Hours, &c.ComboHours); err != nil {
			return nil, fmt.Errorf("store: scan combo cell: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
