package store

import (
	"errors"
	"fmt"
	"strings"
)

// ErrBadGroupBy indicates an unrecognised Summary grouping.
var ErrBadGroupBy = errors.New("store: unknown group_by")

// Filter selects a subset of sessions.
//
// Every list and aggregate query takes the same filter, which is what lets an
// export honour exactly what the UI is displaying.
type Filter struct {
	From string // inclusive RFC3339 lower bound on started_at
	To   string // inclusive RFC3339 upper bound on started_at

	SessionType  []string
	EventContext []string

	TrackID  *int
	CarID    *int
	LeagueID *int

	// ExcludeAI drops event_context = 'AI'. AI results are not comparable to
	// human ones, so pace and pass metrics default to excluding them.
	ExcludeAI bool

	Limit  int
	Offset int
}

// where renders the filter as a SQL predicate over a table aliased s, plus its
// bound arguments. Every value is bound, never interpolated.
func (f Filter) where() (string, []any) {
	var conds []string
	var args []any

	if f.From != "" {
		conds = append(conds, "s.started_at >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		conds = append(conds, "s.started_at <= ?")
		args = append(args, f.To)
	}
	if len(f.SessionType) > 0 {
		conds = append(conds, "s.session_type IN ("+placeholders(len(f.SessionType))+")")
		for _, v := range f.SessionType {
			args = append(args, v)
		}
	}
	if len(f.EventContext) > 0 {
		conds = append(conds, "s.event_context IN ("+placeholders(len(f.EventContext))+")")
		for _, v := range f.EventContext {
			args = append(args, v)
		}
	}
	if f.TrackID != nil {
		conds = append(conds, "s.track_id = ?")
		args = append(args, *f.TrackID)
	}
	if f.CarID != nil {
		conds = append(conds, "s.car_id = ?")
		args = append(args, *f.CarID)
	}
	if f.LeagueID != nil {
		conds = append(conds, "s.league_id = ?")
		args = append(args, *f.LeagueID)
	}
	if f.ExcludeAI {
		conds = append(conds, "s.event_context <> 'AI'")
	}
	if len(conds) == 0 {
		return "1=1", nil
	}
	return strings.Join(conds, " AND "), args
}

// FilterPredicate returns the SQL predicate and bound arguments for f, over a
// sessions table aliased s.
//
// Exported so the API's streaming export can reuse the exact predicate the list
// queries use. That shared path is what guarantees an export contains precisely
// the rows the UI is displaying.
func FilterPredicate(f Filter) (string, []any) { return f.where() }

// placeholders returns "?, ?, ?" for n bound values.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}

// limitClause renders LIMIT/OFFSET, or the empty string when unpaginated.
func (f Filter) limitClause() (string, []any) {
	if f.Limit <= 0 {
		return "", nil
	}
	if f.Offset > 0 {
		return " LIMIT ? OFFSET ?", []any{f.Limit, f.Offset}
	}
	return " LIMIT ?", []any{f.Limit}
}

// ListSessions returns matching sessions newest-first, plus the total number of
// matches ignoring Limit and Offset so the UI can paginate.
func (s *Store) ListSessions(f Filter) ([]Session, int, error) {
	pred, args := f.where()

	var total int
	if err := s.reader.QueryRow(
		`SELECT COUNT(*) FROM sessions s WHERE `+pred, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count sessions: %w", err)
	}

	lim, limArgs := f.limitClause()
	q := `SELECT ` + sessionColumnsAliased +
		` FROM sessions s WHERE ` + pred + ` ORDER BY s.started_at DESC, s.id DESC` + lim

	rows, err := s.reader.Query(q, append(args, limArgs...)...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: query sessions: %w", err)
	}
	defer rows.Close()

	out := []Session{}
	for rows.Next() {
		rec, err := scanSession(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("store: scan session: %w", err)
		}
		out = append(out, *rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: iterate sessions: %w", err)
	}
	return out, total, nil
}

// SummaryRow is one group of aggregated session time.
type SummaryRow struct {
	Key            string  `json:"key"`
	ConnectedHours float64 `json:"connectedHours"`
	InCarHours     float64 `json:"inCarHours"`
	DrivingHours   float64 `json:"drivingHours"`
	Sessions       int     `json:"sessions"`
	Laps           int     `json:"laps"`
	Incidents      int     `json:"incidents"`
}

// groupByExpr maps an allowlisted grouping name to its SQL expression.
//
// This is an allowlist rather than interpolation on purpose: group_by arrives
// from an HTTP query parameter, and interpolating it would be SQL injection.
var groupByExpr = map[string]string{
	"type":        "s.session_type",
	"context":     "s.event_context",
	"typecontext": "s.session_type || '/' || s.event_context",
	"track":       "COALESCE(s.track_name, 'Unknown')",
	"car":         "COALESCE(s.car_name, 'Unknown')",
	"week":        "strftime('%Y-W%W', s.started_at)",
	"month":       "strftime('%Y-%m', s.started_at)",
}

// GroupByNames returns the allowlisted grouping names, for error messages and
// for the API to advertise.
func GroupByNames() []string {
	out := make([]string, 0, len(groupByExpr))
	for k := range groupByExpr {
		out = append(out, k)
	}
	return out
}

// Summary aggregates session time grouped by one allowlisted dimension.
func (s *Store) Summary(f Filter, groupBy string) ([]SummaryRow, error) {
	expr, ok := groupByExpr[groupBy]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrBadGroupBy, groupBy)
	}
	pred, args := f.where()

	q := `
SELECT ` + expr + ` AS k,
       SUM(s.connected_seconds) / 3600.0,
       SUM(s.in_car_seconds) / 3600.0,
       SUM(s.driving_seconds) / 3600.0,
       COUNT(*),
       SUM(s.laps_completed),
       SUM(s.incidents)
FROM sessions s
WHERE ` + pred + `
GROUP BY k
ORDER BY k`

	rows, err := s.reader.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: summary by %s: %w", groupBy, err)
	}
	defer rows.Close()

	out := []SummaryRow{}
	for rows.Next() {
		var r SummaryRow
		if err := rows.Scan(&r.Key, &r.ConnectedHours, &r.InCarHours, &r.DrivingHours,
			&r.Sessions, &r.Laps, &r.Incidents); err != nil {
			return nil, fmt.Errorf("store: scan summary row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DailyRow is one day's driving time, for the calendar heatmap.
type DailyRow struct {
	Day          string  `json:"day"`
	DrivingHours float64 `json:"drivingHours"`
}

// Daily returns driving hours per calendar day.
func (s *Store) Daily(f Filter) ([]DailyRow, error) {
	pred, args := f.where()
	q := `
SELECT date(s.started_at) AS day, SUM(s.driving_seconds) / 3600.0
FROM sessions s
WHERE ` + pred + `
GROUP BY day
ORDER BY day`

	rows, err := s.reader.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: daily: %w", err)
	}
	defer rows.Close()

	out := []DailyRow{}
	for rows.Next() {
		var r DailyRow
		if err := rows.Scan(&r.Day, &r.DrivingHours); err != nil {
			return nil, fmt.Errorf("store: scan daily row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Totals is the headline figures for the dashboard KPI row.
type Totals struct {
	ConnectedHours   float64 `json:"connectedHours"`
	InCarHours       float64 `json:"inCarHours"`
	DrivingHours     float64 `json:"drivingHours"`
	Utilisation      float64 `json:"utilisation"`
	IncidentsPerHour float64 `json:"incidentsPerHour"`
	Sessions         int     `json:"sessions"`
	Laps             int     `json:"laps"`
	Incidents        int     `json:"incidents"`
	PassesMade       int     `json:"passesMade"`
	TimesPassed      int     `json:"timesPassed"`
}

// Totals computes the dashboard headline figures.
func (s *Store) Totals(f Filter) (Totals, error) {
	pred, args := f.where()

	var t Totals
	// COALESCE guards the empty set: SUM over no rows is NULL, not 0.
	err := s.reader.QueryRow(`
SELECT COALESCE(SUM(s.connected_seconds), 0) / 3600.0,
       COALESCE(SUM(s.in_car_seconds), 0) / 3600.0,
       COALESCE(SUM(s.driving_seconds), 0) / 3600.0,
       COUNT(*),
       COALESCE(SUM(s.laps_completed), 0),
       COALESCE(SUM(s.incidents), 0)
FROM sessions s WHERE `+pred, args...,
	).Scan(&t.ConnectedHours, &t.InCarHours, &t.DrivingHours, &t.Sessions, &t.Laps, &t.Incidents)
	if err != nil {
		return Totals{}, fmt.Errorf("store: totals: %w", err)
	}
	if t.ConnectedHours > 0 {
		t.Utilisation = t.DrivingHours / t.ConnectedHours
	}
	if t.DrivingHours > 0 {
		t.IncidentsPerHour = float64(t.Incidents) / t.DrivingHours
	}

	// Only OnTrack causes count: a position gained because someone else pitted
	// is not a pass.
	err = s.reader.QueryRow(`
SELECT COALESCE(SUM(CASE WHEN pe.to_position < pe.from_position THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN pe.to_position > pe.from_position THEN 1 ELSE 0 END), 0)
FROM position_events pe JOIN sessions s ON s.id = pe.session_id
WHERE pe.cause = 'OnTrack' AND `+pred, args...,
	).Scan(&t.PassesMade, &t.TimesPassed)
	if err != nil {
		return Totals{}, fmt.Errorf("store: pass totals: %w", err)
	}
	return t, nil
}

// LapRow is a lap joined to the context of the session it belongs to, so the flat
// lap table can be read without a second query.
type LapRow struct {
	Lap
	StartedAt    string `json:"startedAt"`
	TrackName    string `json:"trackName"`
	CarName      string `json:"carName"`
	SessionType  string `json:"sessionType"`
	EventContext string `json:"eventContext"`
}

// ListLaps returns laps across sessions matching the filter, newest first, plus
// the total match count.
func (s *Store) ListLaps(f Filter) ([]LapRow, int, error) {
	pred, args := f.where()

	var total int
	if err := s.reader.QueryRow(
		`SELECT COUNT(*) FROM laps l JOIN sessions s ON s.id = l.session_id WHERE `+pred, args...,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: count laps: %w", err)
	}

	lim, limArgs := f.limitClause()
	q := `
SELECT l.id, l.uuid, l.session_id, l.lap_number,
       l.lap_time_s, l.delta_to_best_s, l.fuel_used_l, l.fuel_level_end_l,
       l.incidents_on_lap, l.is_pit_lap, l.position, l.class_position,
       l.recorded_at, l.uploaded_at,
       s.started_at, COALESCE(s.track_name, ''), COALESCE(s.car_name, ''),
       s.session_type, s.event_context
FROM laps l JOIN sessions s ON s.id = l.session_id
WHERE ` + pred + `
ORDER BY s.started_at DESC, l.lap_number DESC` + lim

	rows, err := s.reader.Query(q, append(args, limArgs...)...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: query laps: %w", err)
	}
	defer rows.Close()

	out := []LapRow{}
	for rows.Next() {
		var r LapRow
		if err := rows.Scan(
			&r.ID, &r.UUID, &r.SessionID, &r.LapNumber,
			&r.LapTimeS, &r.DeltaToBestS, &r.FuelUsedL, &r.FuelLevelEndL,
			&r.IncidentsOnLap, &r.IsPitLap, &r.Position, &r.ClassPosition,
			&r.RecordedAt, &r.UploadedAt,
			&r.StartedAt, &r.TrackName, &r.CarName, &r.SessionType, &r.EventContext,
		); err != nil {
			return nil, 0, fmt.Errorf("store: scan lap row: %w", err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// Facet is one filter option with the number of sessions it matches.
type Facet struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Sessions int    `json:"sessions"`
}

// Facets is the set of filter options the UI offers.
type Facets struct {
	Tracks        []Facet  `json:"tracks"`
	Cars          []Facet  `json:"cars"`
	Leagues       []Facet  `json:"leagues"`
	SessionTypes  []string `json:"sessionTypes"`
	EventContexts []string `json:"eventContexts"`
}

// Facets returns the distinct filterable values present in the database.
func (s *Store) Facets() (Facets, error) {
	var f Facets

	idNameCount := func(idCol, nameCol string, skipZero bool) ([]Facet, error) {
		q := `SELECT ` + idCol + `, COALESCE(` + nameCol + `, 'Unknown'), COUNT(*)
		      FROM sessions WHERE ` + idCol + ` IS NOT NULL`
		if skipZero {
			q += ` AND ` + idCol + ` <> 0`
		}
		q += ` GROUP BY ` + idCol + ` ORDER BY 2`
		rows, err := s.reader.Query(q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []Facet{}
		for rows.Next() {
			var x Facet
			if err := rows.Scan(&x.ID, &x.Name, &x.Sessions); err != nil {
				return nil, err
			}
			out = append(out, x)
		}
		return out, rows.Err()
	}

	distinct := func(col string) ([]string, error) {
		rows, err := s.reader.Query(`SELECT DISTINCT ` + col + ` FROM sessions ORDER BY 1`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}

	var err error
	if f.Tracks, err = idNameCount("track_id", "track_name", false); err != nil {
		return Facets{}, fmt.Errorf("store: track facets: %w", err)
	}
	if f.Cars, err = idNameCount("car_id", "car_name", false); err != nil {
		return Facets{}, fmt.Errorf("store: car facets: %w", err)
	}
	// League 0 means "not a league session", so it is not a filter option.
	if f.Leagues, err = idNameCount("league_id", "CAST(league_id AS TEXT)", true); err != nil {
		return Facets{}, fmt.Errorf("store: league facets: %w", err)
	}
	if f.SessionTypes, err = distinct("session_type"); err != nil {
		return Facets{}, fmt.Errorf("store: session type facets: %w", err)
	}
	if f.EventContexts, err = distinct("event_context"); err != nil {
		return Facets{}, fmt.Errorf("store: event context facets: %w", err)
	}
	return f, nil
}
