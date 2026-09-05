package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// RatingPoint is one observation of the driver's ratings.
//
// Both ratings are nullable because a session recorded before the identity
// migration has neither, and an offline session may carry an iRating with no
// licence string. A point is only emitted when at least one is present.
type RatingPoint struct {
	StartedAt    string   `json:"startedAt"`
	SessionType  string   `json:"sessionType"`
	EventContext string   `json:"eventContext"`
	IRating      *int     `json:"iRating"`
	SafetyRating *float64 `json:"safetyRating"`
	LicString    *string  `json:"licString"`
	Discipline   *string  `json:"discipline"`
}

// Ratings is the driver's identity plus how their ratings moved over the
// filtered range.
//
// Latest and Delta are computed here rather than in the interface because both
// depend on which points the filter admits: a range that starts mid-season has a
// different first point, and therefore a different delta, than the whole history.
type Ratings struct {
	// UserID is the local driver's iRacing customer ID, absent if no session in
	// range recorded one.
	UserID *int `json:"userId"`

	// LicString, IRating and SafetyRating are the most recent values in range.
	LicString    *string  `json:"licString"`
	IRating      *int     `json:"iRating"`
	SafetyRating *float64 `json:"safetyRating"`

	// IRatingDelta and SafetyRatingDelta are last minus first within range, absent
	// when fewer than two observations carry the value — a delta of zero and "only
	// one reading" are different statements.
	IRatingDelta      *int     `json:"iRatingDelta"`
	SafetyRatingDelta *float64 `json:"safetyRatingDelta"`

	// PeakIRating is the highest value observed in range.
	PeakIRating *int `json:"peakIRating"`

	Points []RatingPoint `json:"points"`
}

// Ratings returns the identity and rating progression for the filtered sessions,
// oldest first.
//
// Ordered oldest-first because the series is read left to right as a progression;
// the summary fields are taken from the ends of that ordering rather than by a
// separate query, so they cannot disagree with the chart beside them.
func (s *Store) Ratings(f Filter) (Ratings, error) {
	pred, args := f.where()

	out := Ratings{Points: []RatingPoint{}}

	// The identity is read across every filtered session, not only the rated ones.
	//
	// These are two different questions. "Whose database is this" is answered by any
	// session; "how did the rating move" only by sessions that observed one. Taking the
	// id from the rating rows conflated them, so a database holding only offline
	// sessions — which carry the account but no ratings — reported no owner at all, and
	// the settings screen said "not yet recorded" for a known account.
	//
	// The newest session that names an id wins, so a database that predates the identity
	// columns still reports whoever its recent sessions belong to.
	idQ := fmt.Sprintf(`
SELECT s.driver_user_id
FROM sessions s
WHERE (%s) AND s.driver_user_id IS NOT NULL
ORDER BY s.started_at DESC
LIMIT 1`, pred)
	var userID *int
	switch err := s.reader.QueryRow(idQ, args...).Scan(&userID); {
	case errors.Is(err, sql.ErrNoRows):
		// No session names an owner. Legitimate on a fresh install.
	case err != nil:
		return Ratings{}, fmt.Errorf("store: ratings identity: %w", err)
	default:
		out.UserID = userID
	}

	// Only sessions that observed a rating take part in the progression. A session with
	// neither value contributes nothing but would flatten the line if emitted as a gap.
	q := fmt.Sprintf(`
SELECT s.started_at, s.session_type, s.event_context,
       s.driver_irating, s.driver_safety_rating, s.driver_lic_string,
       s.driver_rating_category
FROM sessions s
WHERE (%s)
  AND (s.driver_irating IS NOT NULL OR s.driver_safety_rating IS NOT NULL)
ORDER BY s.started_at ASC`, pred)

	rows, err := s.reader.Query(q, args...)
	if err != nil {
		return Ratings{}, fmt.Errorf("store: ratings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var p RatingPoint
		if err := rows.Scan(&p.StartedAt, &p.SessionType, &p.EventContext,
			&p.IRating, &p.SafetyRating, &p.LicString, &p.Discipline); err != nil {
			return Ratings{}, fmt.Errorf("store: ratings scan: %w", err)
		}
		p.Discipline = ratingDiscipline(p.Discipline)
		out.Points = append(out.Points, p)
	}
	if err := rows.Err(); err != nil {
		return Ratings{}, fmt.Errorf("store: ratings rows: %w", err)
	}
	out.summarise()
	return out, nil
}

// ratingDiscipline maps the simulator's category names onto the five licence
// disciplines. Road is the pre-split name; SportsCar is its current equivalent.
// Unknown values remain absent so a future SDK change cannot mislabel a chart.
func ratingDiscipline(category *string) *string {
	if category == nil {
		return nil
	}
	var discipline string
	switch *category {
	case "Road", "SportsCar":
		discipline = "Road"
	case "FormulaCar":
		discipline = "Formula"
	case "Oval":
		discipline = "Oval"
	case "DirtRoad":
		discipline = "Dirt Road"
	case "DirtOval":
		discipline = "Dirt Oval"
	default:
		return nil
	}
	return &discipline
}

// summarise fills the headline fields from the collected points.
//
// Done in one pass after the scan rather than incrementally inside it: a delta
// needs both ends of the range, and computing it as rows arrive means carrying a
// baseline that is only correct once the first point bearing the value is known.
func (r *Ratings) summarise() {
	var firstIR, lastIR *int
	var firstSR, lastSR *float64
	// Counted rather than inferred from the pointers: two readings of the same
	// value are two observations, and pointer identity would call them one.
	var nIR, nSR int

	for i := range r.Points {
		p := r.Points[i]
		if p.IRating != nil {
			if firstIR == nil {
				firstIR = p.IRating
			}
			lastIR = p.IRating
			nIR++
			if r.PeakIRating == nil || *p.IRating > *r.PeakIRating {
				v := *p.IRating
				r.PeakIRating = &v
			}
		}
		if p.SafetyRating != nil {
			if firstSR == nil {
				firstSR = p.SafetyRating
			}
			lastSR = p.SafetyRating
			nSR++
			r.LicString = p.LicString
		}
	}

	r.IRating, r.SafetyRating = lastIR, lastSR

	// A delta needs two distinct observations. With one, last equals first and a
	// reported zero would read as "no movement" rather than "nothing to compare".
	if nIR > 1 {
		d := *lastIR - *firstIR
		r.IRatingDelta = &d
	}
	if nSR > 1 {
		d := *lastSR - *firstSR
		r.SafetyRatingDelta = &d
	}
}
