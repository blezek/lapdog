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

	q := `
SELECT ` + d.idCol + `,
       ` + d.nameExpr + `,
       SUM(s.driving_seconds) / 3600.0,
       COUNT(*),
       SUM(s.laps_completed)
FROM sessions s
WHERE ` + pred + ` AND ` + d.idCol + ` IS NOT NULL
GROUP BY ` + d.idCol + `
ORDER BY SUM(s.driving_seconds) DESC, ` + d.nameExpr

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
