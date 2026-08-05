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
)
SELECT sess.oid, sess.oname, alltime.best, inrange.best,
       COALESCE(sess.laps, 0), sess.sessions
FROM sess
LEFT JOIN inrange ON inrange.oid = sess.oid
LEFT JOIN alltime ON alltime.oid = sess.oid
ORDER BY sess.laps DESC, sess.oname`

	// Argument order follows the CTEs: filtered predicate, filtered predicate,
	// all-time predicate — each followed by the entity id.
	qargs := make([]any, 0, len(args)*2+len(pbArgs)+3)
	qargs = append(qargs, args...)
	qargs = append(qargs, id)
	qargs = append(qargs, args...)
	qargs = append(qargs, id)
	qargs = append(qargs, pbArgs...)
	qargs = append(qargs, id)

	rows, err := s.reader.Query(q, qargs...)
	if err != nil {
		return nil, fmt.Errorf("store: entity pace by %s: %w", by, err)
	}
	defer rows.Close()

	out := []PaceRow{}
	for rows.Next() {
		var r PaceRow
		if err := rows.Scan(&r.OtherID, &r.OtherName, &r.PersonalBestS,
			&r.BestInRangeS, &r.Laps, &r.Sessions); err != nil {
			return nil, fmt.Errorf("store: scan pace row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
