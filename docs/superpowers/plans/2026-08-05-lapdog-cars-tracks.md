# Cars and Tracks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `Cars` and `Tracks` pages, each a per-entity review answering how the driver performs with one car or at one track and whether they are improving.

**Architecture:** Five new store queries parameterised by a dimension (`car` or `track`), following the allowlist pattern `Breakdown(f, by)` already uses; five thin API handlers over them; one React page component rendered twice with a different dimension prop. No collector, capture-format or schema changes.

**Tech Stack:** Go 1.26 with `modernc.org/sqlite`, `net/http` `ServeMux` with method patterns, React 19 + TypeScript + ECharts, TanStack Query and Table, vitest.

**Spec:** `docs/superpowers/specs/2026-08-05-lapdog-cars-tracks-design.md`

## Global Constraints

- `CGO_ENABLED=0` is load-bearing. Never add a dependency requiring cgo.
- No schema migration. `CurrentSchemaVersion` stays as it is; every metric comes from existing columns.
- Session-level and lap-level aggregates are computed in **separate subqueries then joined**. A single aggregate joining `laps` to `sessions` multiplies session sums by lap count.
- Pace and pass metrics default to `ExcludeAI` on. AI results are not comparable to human ones.
- The word for `sessions.incidents` in the interface is **"incident points"**, never "incidents". The sim's counter is severity-weighted 1×/2×/4×.
- Consistency is computed **per session and then averaged**, never across pooled laps.
- Dates and numbers format through `./locale`; never pass a fixed locale string.
- Colour never carries meaning alone: every coloured value keeps its number visible.
- Every chart has a table view, via the existing `Card` `table=` prop.
- Never anonymise other drivers' names.
- Run `make ui` after any change under `web/`, or the Go binary serves a stale bundle.

## File Structure

| File | Responsibility |
|---|---|
| `internal/store/entities.go` (create) | The five per-entity queries and their row types. New file rather than growing `queries.go`, which is already ~500 lines. |
| `internal/store/entities_test.go` (create) | Query tests plus `seedPace`, a fixture with enough laps to exercise consistency. |
| `internal/api/handlers.go` (modify) | Five handlers, each translating query string to store call. |
| `internal/api/server.go` (modify) | Route registration. |
| `internal/api/api_test.go` (modify) | Handler tests: validation, 404, filter passthrough. |
| `internal/ui/icons/mdi/road-variant.svg` (create) | The Tracks nav icon. |
| `internal/ui/icons/icons.go` (modify) | `RoadVariant` constant and `All` entry. |
| `web/src/api.ts` (modify) | Types mirroring the Go rows, and client methods. |
| `web/src/entity.ts` (create) | Pure helpers: consistency banding, dimension labels. Separate so it is unit-testable without a DOM. |
| `web/src/entity.test.ts` (create) | Tests for those helpers. |
| `web/src/pages/Entity.tsx` (create) | The shared page: left list, headline stats, and the panels. |
| `web/src/components/Filters.tsx` (modify) | A `hide` prop so a page can suppress its own dimension's dropdown. |
| `web/src/App.tsx` (modify) | Nav entries and routes. |

---

### Task 1: Store scaffolding and the entity list

**Files:**
- Create: `internal/store/entities.go`
- Create: `internal/store/entities_test.go`

**Interfaces:**
- Consumes: `Filter`, `Filter.where()`, `ErrBadGroupBy`, `ErrNotFound` from `internal/store/queries.go` and `sessions.go`.
- Produces:
  - `func EntityDimensions() []string`
  - `type EntityRow struct { ID int; Name string; DrivingHours float64; Sessions int; Laps int }`
  - `func (s *Store) EntityList(f Filter, by string) ([]EntityRow, error)`

- [ ] **Step 1: Write the failing test**

Create `internal/store/entities_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func TestEntityListByCar(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	rows, err := s.EntityList(Filter{}, "car")
	if err != nil {
		t.Fatalf("EntityList: %v", err)
	}
	// The seed has two cars: the Porsche across five sessions and the MX-5 in the
	// single AI race.
	if len(rows) != 2 {
		t.Fatalf("got %d cars, want 2: %+v", len(rows), rows)
	}
	// Ordered by driving time descending, so the Porsche leads.
	if rows[0].Name != "Porsche 911 GT3 R" {
		t.Errorf("rows[0].Name = %q, want the most-driven car first", rows[0].Name)
	}
	if rows[0].ID != 173 {
		t.Errorf("rows[0].ID = %d, want 173", rows[0].ID)
	}
	if rows[0].Sessions != 5 {
		t.Errorf("Porsche sessions = %d, want 5", rows[0].Sessions)
	}
	if rows[1].DrivingHours > rows[0].DrivingHours {
		t.Error("rows are not ordered by driving time descending")
	}
}

func TestEntityListByTrack(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	rows, err := s.EntityList(Filter{}, "track")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d tracks, want 2 (Watkins Glen and Spa): %+v", len(rows), rows)
	}
	names := map[string]bool{}
	for _, r := range rows {
		names[r.Name] = true
	}
	for _, want := range []string{"Watkins Glen", "Spa"} {
		if !names[want] {
			t.Errorf("track %q missing from %+v", want, rows)
		}
	}
}

// Driving hours must sum to the same total Totals reports. This is the fan-out
// guard: joining laps into a session-level aggregate multiplies every sum by that
// session's lap count, which produces a plausible number rather than an error.
func TestEntityListHoursSumToTotal(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	totals, err := s.Totals(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.EntityList(Filter{}, "car")
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, r := range rows {
		sum += r.DrivingHours
	}
	if diff := sum - totals.DrivingHours; diff > 0.001 || diff < -0.001 {
		t.Errorf("per-car hours sum to %.4f, Totals reports %.4f", sum, totals.DrivingHours)
	}
}

func TestEntityListRejectsUnknownDimension(t *testing.T) {
	s := openTemp(t)
	if _, err := s.EntityList(Filter{}, "driver"); !errors.Is(err, ErrBadGroupBy) {
		t.Errorf("EntityList with an unknown dimension = %v, want ErrBadGroupBy", err)
	}
}

func TestEntityDimensions(t *testing.T) {
	got := EntityDimensions()
	if len(got) != 2 {
		t.Fatalf("EntityDimensions() = %v, want exactly car and track", got)
	}
	seen := map[string]bool{got[0]: true, got[1]: true}
	if !seen["car"] || !seen["track"] {
		t.Errorf("EntityDimensions() = %v, want car and track", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run Entity -v`
Expected: FAIL — `undefined: EntityDimensions`, `undefined: EntityRow`, `s.EntityList undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/store/entities.go`:

```go
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
	idCol    string // sessions column holding the entity id
	nameExpr string // display name for the entity
	otherID  string // the opposite dimension's id column
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run Entity -v`
Expected: PASS, five tests.

- [ ] **Step 5: Check formatting and vet**

Run: `gofmt -l internal/store/ && CGO_ENABLED=0 go vet ./internal/store/`
Expected: no output from either.

- [ ] **Step 6: Commit**

```bash
git add internal/store/entities.go internal/store/entities_test.go
git commit -m "Add per-entity dimension allowlist and entity list query"
```

---

### Task 2: Headline stats

**Files:**
- Modify: `internal/store/entities.go`
- Modify: `internal/store/entities_test.go`

**Interfaces:**
- Consumes: `entityDim`, `dimOrErr` from Task 1.
- Produces:
  - `type EntityStats struct { ID int; Name string; DrivingHours, InCarHours, ConnectedHours float64; Sessions, Laps int; DistanceKm float64; IncidentPoints int; CleanLapPct *float64; IncidentPointsPer100Km *float64; Races int; Wins int; Podiums int; AvgPositionsGained *float64 }`
  - `func (s *Store) EntityStats(f Filter, by string, id int) (EntityStats, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/entities_test.go`:

```go
func TestEntityStatsForCar(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	st, err := s.EntityStats(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityStats: %v", err)
	}
	if st.Name != "Porsche 911 GT3 R" {
		t.Errorf("Name = %q", st.Name)
	}
	if st.Sessions != 5 {
		t.Errorf("Sessions = %d, want 5", st.Sessions)
	}
	// The seed's Porsche rows carry 2+1+0+6+4 = 13 incident points.
	if st.IncidentPoints != 13 {
		t.Errorf("IncidentPoints = %d, want 13", st.IncidentPoints)
	}
	// Laps come from laps_completed: 20+15+3+25+30 = 93.
	if st.Laps != 93 {
		t.Errorf("Laps = %d, want 93", st.Laps)
	}
	// Three of the seed's Porsche sessions are races (two OfficialRace, one League).
	if st.Races != 3 {
		t.Errorf("Races = %d, want 3", st.Races)
	}
}

// An entity with no rows in range is a 404 case, not a zeroed struct: a page
// showing every stat as zero is indistinguishable from a car never driven.
func TestEntityStatsUnknownIDIsNotFound(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	if _, err := s.EntityStats(Filter{}, "car", 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("EntityStats for an unknown car = %v, want ErrNotFound", err)
	}
}

func TestEntityStatsRejectsUnknownDimension(t *testing.T) {
	s := openTemp(t)
	if _, err := s.EntityStats(Filter{}, "driver", 1); !errors.Is(err, ErrBadGroupBy) {
		t.Errorf("= %v, want ErrBadGroupBy", err)
	}
}

// Driving hours for one entity must match what EntityList reports for it, or the
// two views of the same page disagree.
func TestEntityStatsAgreesWithEntityList(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	list, err := s.EntityList(Filter{}, "car")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range list {
		st, err := s.EntityStats(Filter{}, "car", row.ID)
		if err != nil {
			t.Fatalf("EntityStats(%d): %v", row.ID, err)
		}
		if diff := st.DrivingHours - row.DrivingHours; diff > 0.001 || diff < -0.001 {
			t.Errorf("car %d: stats %.4f h, list %.4f h", row.ID, st.DrivingHours, row.DrivingHours)
		}
		if st.Laps != row.Laps {
			t.Errorf("car %d: stats %d laps, list %d laps", row.ID, st.Laps, row.Laps)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run EntityStats -v`
Expected: FAIL — `s.EntityStats undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/store/entities.go`:

```go
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
	sessQ := `
SELECT ` + d.nameExpr + `,
       SUM(s.driving_seconds) / 3600.0,
       SUM(s.in_car_seconds) / 3600.0,
       SUM(s.connected_seconds) / 3600.0,
       COUNT(*),
       SUM(s.laps_completed),
       SUM(s.laps_completed * COALESCE(s.track_length_km, 0)),
       SUM(s.incidents),
       SUM(CASE WHEN s.session_type = 'Race' AND s.finish_position > 0 THEN 1 ELSE 0 END),
       SUM(CASE WHEN s.session_type = 'Race' AND s.finish_position = 1 THEN 1 ELSE 0 END),
       SUM(CASE WHEN s.session_type = 'Race' AND s.finish_position BETWEEN 1 AND 3 THEN 1 ELSE 0 END),
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
SELECT COUNT(*),
       SUM(CASE WHEN l.incidents_on_lap = 0 THEN 1 ELSE 0 END)
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
```

Note on `Scan` into `&out.Name` when no rows match: `SUM` over an empty set yields `NULL`, and the aggregate query always returns exactly one row. `COUNT(*)` is 0 in that case, which is why the zero check follows the scan rather than replacing it. `out.Name` would scan `NULL` into a string and error first, so the query uses `COALESCE` in `nameExpr` and the `Sessions == 0` check catches the genuinely-absent case for ids that match nothing.

Correction to apply while writing: because a scan of `NULL` into `string` fails before the count check is reached, wrap the name in `COALESCE(MAX(` … `), '')`. Use this exact expression for the first selected column instead of `d.nameExpr`:

```go
	nameSel := `COALESCE(MAX(` + d.nameExpr + `), '')`
```

and select `nameSel` first, keeping everything else unchanged. Sum columns are scanned into numeric types, which accept `NULL` as zero only for `COUNT`; so also wrap each `SUM(...)` in `COALESCE(SUM(...), 0)`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run Entity -v`
Expected: PASS, nine tests.

- [ ] **Step 5: Verify the fan-out guard actually guards**

Temporarily change the session-level query to join laps — add `JOIN laps l ON l.session_id = s.id` before `WHERE` — and run the tests.
Expected: `TestEntityListHoursSumToTotal` or `TestEntityStatsAgreesWithEntityList` FAILS with inflated hours. Revert the change and confirm PASS again. This proves the guard is real rather than decorative.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/store/
git add internal/store/entities.go internal/store/entities_test.go
git commit -m "Add per-entity headline stats"
```

---

### Task 3: Pace rows without consistency

**Files:**
- Modify: `internal/store/entities.go`
- Modify: `internal/store/entities_test.go`

**Interfaces:**
- Consumes: `entityDim`, `dimOrErr`.
- Produces:
  - `type PaceRow struct { OtherID int; OtherName string; PersonalBestS *float64; BestInRangeS *float64; Laps int; Sessions int; ConsistencyPct *float64; ConsistencyDeltaS *float64 }`
  - `func (s *Store) EntityPace(f Filter, by string, id int) ([]PaceRow, error)`

The consistency fields are declared now and left nil; Task 4 fills them. Declaring the final shape here means the API and frontend types never change.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/entities_test.go`:

```go
func TestEntityPaceByCarGroupsByTrack(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	rows, err := s.EntityPace(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityPace: %v", err)
	}
	// The Porsche was driven at Watkins Glen and Spa.
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(rows), rows)
	}
	// Ordered by laps descending: Watkins Glen has four sessions to Spa's one.
	if rows[0].OtherName != "Watkins Glen" {
		t.Errorf("rows[0] = %q, want the most-lapped track first", rows[0].OtherName)
	}
	if rows[0].Sessions != 4 {
		t.Errorf("Watkins Glen sessions = %d, want 4", rows[0].Sessions)
	}
	if rows[0].PersonalBestS == nil {
		t.Fatal("PersonalBestS is nil; the seed has timed laps")
	}
	// seed() inserts laps at best+0.5 and best+1.0 for each session; the fastest
	// Porsche lap at Watkins Glen therefore comes from the 101.8 qualifying row.
	if got := *rows[0].PersonalBestS; got < 102.29 || got > 102.31 {
		t.Errorf("PersonalBestS = %.3f, want 102.30", got)
	}
}

func TestEntityPaceByTrackGroupsByCar(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	rows, err := s.EntityPace(Filter{}, "track", 341)
	if err != nil {
		t.Fatal(err)
	}
	// Spa hosted the Porsche league race and the MX-5 AI race, but AI is excluded
	// from pace by default only when the caller asks; Filter{} does not.
	names := map[string]bool{}
	for _, r := range rows {
		names[r.OtherName] = true
	}
	if !names["Porsche 911 GT3 R"] {
		t.Errorf("Porsche missing from Spa pace rows: %+v", rows)
	}
}

// The personal best ignores the date range; the in-range best respects it. That
// pair is what tells a driver they are off form, so they must differ when the
// range excludes the fastest lap.
func TestEntityPacePersonalBestIgnoresDateRange(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	// The Porsche's fastest Watkins Glen lap is in the 8 July session. Restrict the
	// range to 1 July only, which excludes it.
	f := Filter{From: "2026-07-01T00:00:00Z", To: "2026-07-02T00:00:00Z"}
	rows, err := s.EntityPace(f, "car", 173)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want only Watkins Glen: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.PersonalBestS == nil || r.BestInRangeS == nil {
		t.Fatal("both bests should be present")
	}
	if *r.PersonalBestS >= *r.BestInRangeS {
		t.Errorf("PersonalBestS %.3f should be faster than BestInRangeS %.3f",
			*r.PersonalBestS, *r.BestInRangeS)
	}
}

// A session with no timed laps must still appear, with pace as nil rather than the
// row vanishing — otherwise time spent shows in the headline but nowhere else.
func TestEntityPaceIncludesEntityWithNoTimedLaps(t *testing.T) {
	s := openTemp(t)
	id, err := s.UpsertSession(&Session{
		SessionKey: "9001/0", SubsessionID: 9001, SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt: "2026-07-02T10:00:00Z", DrivingSeconds: 600,
		TrackID: intp(77), TrackName: strp("Okayama"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = id

	rows, err := s.EntityPace(Filter{}, "car", 173)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].PersonalBestS != nil {
		t.Errorf("PersonalBestS = %v, want nil with no timed laps", *rows[0].PersonalBestS)
	}
	if rows[0].Laps != 0 {
		t.Errorf("Laps = %d, want 0", rows[0].Laps)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run EntityPace -v`
Expected: FAIL — `s.EntityPace undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/store/entities.go`:

```go
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
         ` + d.otherExpr + ` AS oname,
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run EntityPace -v`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/store/ && CGO_ENABLED=0 go vet ./internal/store/
git add internal/store/entities.go internal/store/entities_test.go
git commit -m "Add per-entity pace rows with all-time and in-range bests"
```

---

### Task 4: Consistency

The subtlest part of the feature. It gets its own task because the wrong definition returns a plausible number rather than an error.

**Files:**
- Modify: `internal/store/entities.go`
- Modify: `internal/store/entities_test.go`

**Interfaces:**
- Consumes: `PaceRow`, `EntityPace` from Task 3.
- Produces: no new exported names; `EntityPace` now populates `ConsistencyPct` and `ConsistencyDeltaS`. Adds unexported `consistencyMinLaps` and `consistencyOutlierFactor`.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/entities_test.go`. `seedPace` is new: the shared `seed` helper gives each session only two laps, so after dropping the first there is one left — below the minimum, which would suppress consistency in every assertion.

```go
// seedPace creates one car at one track with sessions long enough to measure
// consistency. The shared seed helper gives two laps per session, which cannot
// exercise a metric that needs five after exclusions.
//
// lapSet is the timed laps in order; the first is dropped as a standing start.
func seedPace(t *testing.T, s *Store, key string, started string, lapSet []float64, pitLast bool) int64 {
	t.Helper()
	id, err := s.UpsertSession(&Session{
		SessionKey: key, SubsessionID: 1, SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt: started, DrivingSeconds: 1800, LapsCompleted: len(lapSet),
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range lapSet {
		lap := &Lap{SessionID: id, LapNumber: i + 1, LapTimeS: f64p(v)}
		if pitLast && i == len(lapSet)-1 {
			lap.IsPitLap = true
		}
		if _, err := s.InsertLap(lap); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// consistencyOf finds the Watkins Glen row and returns its consistency percentage.
func consistencyOf(t *testing.T, s *Store, f Filter) *float64 {
	t.Helper()
	rows, err := s.EntityPace(f, "car", 173)
	if err != nil {
		t.Fatalf("EntityPace: %v", err)
	}
	for _, r := range rows {
		if r.OtherName == "Watkins Glen" {
			return r.ConsistencyPct
		}
	}
	t.Fatal("no Watkins Glen row")
	return nil
}

func TestConsistencyPerSessionThenAveraged(t *testing.T) {
	s := openTemp(t)
	// Two sessions, each internally tight, but a full second apart from each other.
	// Per-session-then-averaged this is highly consistent. Pooling the laps would
	// report a far worse figure, and both forms return a plausible percentage — so
	// this test is the only thing that distinguishes them.
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2, 100.1, 100.0, 100.2}, false)
	seedPace(t, s, "p2/0", "2026-07-02T10:00:00Z",
		[]float64{106.0, 101.0, 101.1, 101.2, 101.1, 101.0, 101.2}, false)

	got := consistencyOf(t, s, Filter{})
	if got == nil {
		t.Fatal("consistency is nil; both sessions have six laps after the first")
	}
	// Each session's best is 100.0 (or 101.0) and its other laps average about
	// 0.12 s slower, so consistency is near 99.9%. Pooling gives roughly 99.4%.
	if *got < 99.7 {
		t.Errorf("consistency = %.2f%%, want >= 99.7 — laps are likely being pooled "+
			"across sessions rather than measured within each", *got)
	}
}

func TestConsistencySuppressedBelowMinimumLaps(t *testing.T) {
	s := openTemp(t)
	// Four timed laps: three survive dropping the first, below the five needed.
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2}, false)

	if got := consistencyOf(t, s, Filter{}); got != nil {
		t.Errorf("consistency = %.2f, want nil below the lap minimum — one lap is "+
			"trivially 100%% consistent", *got)
	}
}

func TestConsistencyExcludesPitLaps(t *testing.T) {
	s := openTemp(t)
	// A slow final lap marked as a pit lap must not drag consistency down.
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2, 100.1, 100.0, 100.1, 140.0}, true)

	got := consistencyOf(t, s, Filter{})
	if got == nil {
		t.Fatal("consistency is nil")
	}
	if *got < 99.7 {
		t.Errorf("consistency = %.2f%%, want >= 99.7 — the 140 s pit lap is being "+
			"counted", *got)
	}
}

func TestConsistencyExcludesOutliers(t *testing.T) {
	s := openTemp(t)
	// 130 s is well beyond 110%% of the 100 s best, so it is an outlier and dropped.
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2, 100.1, 100.0, 100.1, 130.0}, false)

	got := consistencyOf(t, s, Filter{})
	if got == nil {
		t.Fatal("consistency is nil")
	}
	if *got < 99.7 {
		t.Errorf("consistency = %.2f%%, want >= 99.7 — the 130 s outlier is being "+
			"counted", *got)
	}
}

// A lap just inside the threshold must still count, or the exclusion is silently
// discarding real laps.
func TestConsistencyKeepsLapsInsideOutlierThreshold(t *testing.T) {
	s := openTemp(t)
	// 108 s is inside 110 s, so it counts and pulls consistency down measurably.
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{115.0, 100.0, 100.1, 100.2, 100.1, 100.0, 108.0}, false)

	got := consistencyOf(t, s, Filter{})
	if got == nil {
		t.Fatal("consistency is nil")
	}
	if *got > 99.0 {
		t.Errorf("consistency = %.2f%%, want < 99.0 — the 108 s lap is inside the "+
			"110%% threshold and should count", *got)
	}
}

func TestConsistencyDeltaAccompaniesPercentage(t *testing.T) {
	s := openTemp(t)
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.5, 100.5, 100.5, 100.5, 100.5}, false)

	rows, err := s.EntityPace(Filter{}, "car", 173)
	if err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	if r.ConsistencyPct == nil || r.ConsistencyDeltaS == nil {
		t.Fatal("both consistency fields should be set together")
	}
	// Best 100.0, the other five laps all 100.5, so the delta is 0.5 s.
	if *r.ConsistencyDeltaS < 0.49 || *r.ConsistencyDeltaS > 0.51 {
		t.Errorf("ConsistencyDeltaS = %.3f, want 0.50", *r.ConsistencyDeltaS)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run Consistency -v`
Expected: FAIL — every test reports nil consistency, because Task 3 left the fields unset.

- [ ] **Step 3: Write the implementation**

In `internal/store/entities.go`, add the constants near the top, after `entityDims`:

```go
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
```

Then add the consistency CTEs to `EntityPace`. Replace the query's `alltime` CTE closing paren and the final `SELECT` with:

```go
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
```

Extend the argument list, after the all-time id:

```go
	qargs = append(qargs, args...)
	qargs = append(qargs, id, consistencyOutlierFactor, consistencyMinLaps)
```

And extend the scan:

```go
		if err := rows.Scan(&r.OtherID, &r.OtherName, &r.PersonalBestS,
			&r.BestInRangeS, &r.Laps, &r.Sessions,
			&r.ConsistencyPct, &r.ConsistencyDeltaS); err != nil {
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run 'Consistency|EntityPace' -v`
Expected: PASS, eleven tests.

- [ ] **Step 5: Prove the pooling test discriminates**

Temporarily change the `persession` CTE to group by `oid` alone instead of `sid, oid` — the pooled definition.
Run: `CGO_ENABLED=0 go test ./internal/store/ -run TestConsistencyPerSessionThenAveraged -v`
Expected: FAIL, reporting roughly 99.4% against the 99.7% floor. Revert and confirm PASS. Without this check the test could pass under either definition and prove nothing.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/store/ && CGO_ENABLED=0 go test ./internal/store/
git add internal/store/entities.go internal/store/entities_test.go
git commit -m "Add per-session consistency to entity pace rows"
```

---

### Task 5: Progression, rivals and qualifying pace

**Files:**
- Modify: `internal/store/entities.go`
- Modify: `internal/store/entities_test.go`

**Interfaces:**
- Consumes: `entityDim`, `dimOrErr`.
- Produces:
  - `type ProgressionRow struct { Month string; BestLapS float64; Laps int }`
  - `func (s *Store) EntityProgression(f Filter, by string, id, otherID int) ([]ProgressionRow, error)`
  - `type RivalRow struct { Name string; PassedThem int; LostTo int; Net int }`
  - `func (s *Store) Rivals(f Filter) ([]RivalRow, error)`
  - `type QualiPace struct { Pairs int; AvgDeltaS *float64 }`
  - `func (s *Store) QualifyingVsRace(f Filter, by string, id int) (QualiPace, error)`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/entities_test.go`:

```go
func TestEntityProgressionByMonth(t *testing.T) {
	s := openTemp(t)
	seedPace(t, s, "p1/0", "2026-06-10T10:00:00Z",
		[]float64{105.0, 103.0, 103.2, 103.1, 103.0, 103.4}, false)
	seedPace(t, s, "p2/0", "2026-07-10T10:00:00Z",
		[]float64{104.0, 101.0, 101.2, 101.1, 101.0, 101.4}, false)

	rows, err := s.EntityProgression(Filter{}, "car", 173, 18)
	if err != nil {
		t.Fatalf("EntityProgression: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d months, want 2: %+v", len(rows), rows)
	}
	if rows[0].Month != "2026-06" || rows[1].Month != "2026-07" {
		t.Errorf("months = %q, %q; want 2026-06 then 2026-07 in ascending order",
			rows[0].Month, rows[1].Month)
	}
	// June's best is 103.0, July's 101.0: the driver improved.
	if rows[0].BestLapS <= rows[1].BestLapS {
		t.Errorf("June %.3f should be slower than July %.3f", rows[0].BestLapS, rows[1].BestLapS)
	}
	if rows[1].Laps != 6 {
		t.Errorf("July laps = %d, want 6", rows[1].Laps)
	}
}

// Pit laps are not attempts at a fast lap, so they cannot set a monthly best.
func TestEntityProgressionExcludesPitLaps(t *testing.T) {
	s := openTemp(t)
	seedPace(t, s, "p1/0", "2026-07-10T10:00:00Z",
		[]float64{105.0, 103.0, 103.2, 103.1, 103.0, 40.0}, true)

	rows, err := s.EntityProgression(Filter{}, "car", 173, 18)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	if rows[0].BestLapS < 100 {
		t.Errorf("BestLapS = %.3f, want ~103 — the 40 s pit lap set the best", rows[0].BestLapS)
	}
}

func TestRivalsCountsOnlyOnTrackPasses(t *testing.T) {
	s := openTemp(t)
	id, err := s.UpsertSession(&Session{
		SessionKey: "r1/2", SubsessionID: 5001, SessionNum: 2,
		SessionType: "Race", EventContext: "OfficialRace",
		StartedAt: "2026-07-10T18:00:00Z", DrivingSeconds: 1800, LapsCompleted: 10,
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []PositionEvent{
		// Two on-track passes of Larson, one loss to them.
		{LapNumber: 2, SessionTimeS: 10, FromPosition: 6, ToPosition: 5,
			OpponentName: strp("K. Larson"), Cause: CauseOnTrack},
		{LapNumber: 3, SessionTimeS: 20, FromPosition: 6, ToPosition: 5,
			OpponentName: strp("K. Larson"), Cause: CauseOnTrack},
		{LapNumber: 4, SessionTimeS: 30, FromPosition: 5, ToPosition: 6,
			OpponentName: strp("K. Larson"), Cause: CauseOnTrack},
		// Inheriting a place because they pitted is not overtaking.
		{LapNumber: 5, SessionTimeS: 40, FromPosition: 6, ToPosition: 5,
			OpponentName: strp("M. Andretti"), Cause: CauseOpponentPit},
	} {
		ev.SessionID = id
		if _, err := s.InsertPositionEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.Rivals(Filter{})
	if err != nil {
		t.Fatalf("Rivals: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rivals, want only Larson: %+v", len(rows), rows)
	}
	r := rows[0]
	if r.Name != "K. Larson" {
		t.Errorf("Name = %q", r.Name)
	}
	if r.PassedThem != 2 || r.LostTo != 1 || r.Net != 1 {
		t.Errorf("got passed=%d lost=%d net=%d, want 2/1/1", r.PassedThem, r.LostTo, r.Net)
	}
}

func TestRivalsExcludesAIByDefaultWhenAsked(t *testing.T) {
	s := openTemp(t)
	id, err := s.UpsertSession(&Session{
		SessionKey: "ai/2", SubsessionID: 6001, SessionNum: 2,
		SessionType: "Race", EventContext: "AI",
		StartedAt: "2026-07-11T18:00:00Z", DrivingSeconds: 900, LapsCompleted: 5,
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := PositionEvent{SessionID: id, LapNumber: 2, SessionTimeS: 10,
		FromPosition: 4, ToPosition: 3, OpponentName: strp("AI Driver"),
		Cause: CauseOnTrack}
	if _, err := s.InsertPositionEvent(&ev); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Rivals(Filter{ExcludeAI: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Errorf("got %+v, want no rivals when AI is excluded", rows)
	}
}
```

Also append the qualifying-pace test:

```go
// Race pace against qualifying pace, paired within one race weekend.
//
// The pair must come from the same subsession, or a qualifying session at one track
// would be compared against a race at another.
func TestQualifyingVsRacePairsWithinASubsession(t *testing.T) {
	s := openTemp(t)
	mk := func(key string, num int, st string, best float64, quali *float64) {
		t.Helper()
		if _, err := s.UpsertSession(&Session{
			SessionKey: key, SubsessionID: 7001, SessionNum: num,
			SessionType: st, EventContext: "OfficialRace",
			StartedAt: "2026-07-12T18:00:00Z", DrivingSeconds: 600, LapsCompleted: 5,
			TrackID: intp(18), TrackName: strp("Watkins Glen"),
			CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
			BestLapTimeS: f64p(best), QualifyBestTimeS: quali,
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Qualifying best 100.0, race best 101.5: race pace is 1.5 s slower, which is
	// the normal direction — lower fuel and a clear track in qualifying.
	mk("7001/1", 1, "Qualify", 100.0, f64p(100.0))
	mk("7001/2", 2, "Race", 101.5, nil)

	got, err := s.QualifyingVsRace(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("QualifyingVsRace: %v", err)
	}
	if got.Pairs != 1 {
		t.Fatalf("Pairs = %d, want 1", got.Pairs)
	}
	if got.AvgDeltaS == nil {
		t.Fatal("AvgDeltaS is nil")
	}
	if *got.AvgDeltaS < 1.49 || *got.AvgDeltaS > 1.51 {
		t.Errorf("AvgDeltaS = %.3f, want 1.50 (race minus qualifying)", *got.AvgDeltaS)
	}
}

// A weekend with no qualifying session contributes nothing rather than zero.
func TestQualifyingVsRaceWithNoQualifying(t *testing.T) {
	s := openTemp(t)
	if _, err := s.UpsertSession(&Session{
		SessionKey: "8001/0", SubsessionID: 8001, SessionNum: 0,
		SessionType: "Race", EventContext: "OfficialRace",
		StartedAt: "2026-07-13T18:00:00Z", DrivingSeconds: 600, LapsCompleted: 5,
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		BestLapTimeS: f64p(101.0),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.QualifyingVsRace(Filter{}, "car", 173)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pairs != 0 || got.AvgDeltaS != nil {
		t.Errorf("got %+v, want no pairs and a nil delta", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run 'Progression|Rivals|Qualifying' -v`
Expected: FAIL — `s.EntityProgression undefined`, `s.Rivals undefined`, `s.QualifyingVsRace undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/store/entities.go`:

```go
// ProgressionRow is one month's best lap, for the improvement chart.
type ProgressionRow struct {
	Month   string  `json:"month"`
	BestLapS float64 `json:"bestLapS"`
	Laps    int     `json:"laps"`
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
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/store/ -v`
Expected: PASS, the whole store package.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/store/ && CGO_ENABLED=0 go vet ./internal/store/
git add internal/store/entities.go internal/store/entities_test.go
git commit -m "Add entity progression and per-opponent rivals queries"
```

---

### Task 6: API endpoints

**Files:**
- Modify: `internal/api/server.go` (route registration, after the `/api/breakdown` line)
- Modify: `internal/api/handlers.go`
- Modify: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `EntityList`, `EntityStats`, `EntityPace`, `EntityProgression`, `Rivals` from Tasks 1–5; `s.filterOrFail`, `s.fail`, `s.writeJSON`, `s.notFoundOr500`, `ErrBadRequest` from `internal/api/server.go`.
- Produces: `GET /api/entities`, `GET /api/entity`, `GET /api/pace`, `GET /api/progression`, `GET /api/rivals`.

- [ ] **Step 1: Write the failing test**

Append to `internal/api/api_test.go`:

```go
func TestEntitiesEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entities?by=car", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []store.EntityRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestEntitiesRejectsUnknownDimension(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entities?by=driver", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown dimension", rec.Code)
	}
}

func TestEntityEndpointRequiresID(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entity?by=car", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when id is absent", rec.Code)
	}
}

func TestEntityEndpointUnknownIDIs404(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/entity?by=car&id=999999", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPaceEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/pace?by=car&id=173", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []store.PaceRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestProgressionEndpointRequiresBothIDs(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/progression?by=car&id=173", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 without the other id — a line mixing tracks "+
			"would be meaningless", rec.Code)
	}
}

func TestQualiPaceEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/quali-pace?by=car&id=173&range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got store.QualiPace
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestRivalsEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/rivals?range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var rows []store.RivalRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/api/ -run 'Entit|Pace|Progression|Rivals|Quali' -v`
Expected: FAIL — the routes are unregistered, so each returns 404 from the `/api/` catch-all.

- [ ] **Step 3: Register the routes**

In `internal/api/server.go`, immediately after the `mux.HandleFunc("GET /api/breakdown", s.handleBreakdown)` line:

```go
	mux.HandleFunc("GET /api/entities", s.handleEntities)
	mux.HandleFunc("GET /api/entity", s.handleEntity)
	mux.HandleFunc("GET /api/pace", s.handlePace)
	mux.HandleFunc("GET /api/progression", s.handleProgression)
	mux.HandleFunc("GET /api/rivals", s.handleRivals)
	mux.HandleFunc("GET /api/quali-pace", s.handleQualiPace)
```

- [ ] **Step 4: Write the handlers**

Append to `internal/api/handlers.go`:

```go
// requiredInt reads a required integer query parameter.
//
// Absent and unparseable are both client mistakes, and both must be 400 rather than
// a zero that would silently query entity 0.
func (s *Server) requiredInt(w http.ResponseWriter, q url.Values, key string) (int, bool) {
	raw := q.Get(key)
	if raw == "" {
		s.fail(w, http.StatusBadRequest, fmt.Errorf("%w: %s is required", ErrBadRequest, key))
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		s.fail(w, http.StatusBadRequest,
			fmt.Errorf("%w: %s must be an integer", ErrBadRequest, key))
		return 0, false
	}
	return v, true
}

// dimension reads the by parameter, defaulting to car as /api/breakdown does.
func dimension(q url.Values) string {
	if by := q.Get("by"); by != "" {
		return by
	}
	return "car"
}

func (s *Server) handleEntities(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, err := s.st.EntityList(f, dimension(r.URL.Query()))
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleEntity(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	st, err := s.st.EntityStats(f, dimension(q), id)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}
	s.writeJSON(w, st)
}

func (s *Server) handlePace(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	rows, err := s.st.EntityPace(f, dimension(q), id)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleProgression(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	other, ok := s.requiredInt(w, q, "other")
	if !ok {
		return
	}
	rows, err := s.st.EntityProgression(f, dimension(q), id, other)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleRivals(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	rows, err := s.st.Rivals(f)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, rows)
}

func (s *Server) handleQualiPace(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	id, ok := s.requiredInt(w, q, "id")
	if !ok {
		return
	}
	got, err := s.st.QualifyingVsRace(f, dimension(q), id)
	if errors.Is(err, store.ErrBadGroupBy) {
		s.fail(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, got)
}
```

Add `"net/url"` and `"strconv"` to the imports in `internal/api/handlers.go` if not already present.

- [ ] **Step 5: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/api/ -v`
Expected: PASS, the whole api package.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/api/ && CGO_ENABLED=0 go vet ./internal/api/
git add internal/api/
git commit -m "Add per-entity API endpoints"
```

---

### Task 7: Icon, navigation and empty pages

Ends with both pages reachable and rendering their own list. Nothing on them yet, which is the point: the route and nav wiring is reviewable on its own.

**Files:**
- Create: `internal/ui/icons/mdi/road-variant.svg`
- Modify: `internal/ui/icons/icons.go`
- Create: `web/src/entity.ts`
- Create: `web/src/entity.test.ts`
- Modify: `web/src/api.ts`
- Create: `web/src/pages/Entity.tsx`
- Modify: `web/src/App.tsx`

**Interfaces:**
- Consumes: the API endpoints from Task 6.
- Produces:
  - `icons.RoadVariant = "road-variant"`
  - `web/src/entity.ts`: `type Dimension = 'car' | 'track'`, `dimensionLabel(d)`, `otherLabel(d)`, `consistencyBand(pct)`
  - `web/src/api.ts`: `EntityRow`, `EntityStats`, `PaceRow`, `ProgressionRow`, `RivalRow`, and `api.entities`, `api.entity`, `api.pace`, `api.progression`, `api.rivals`
  - `web/src/pages/Entity.tsx`: `EntityPage({ dimension }: { dimension: Dimension })`

- [ ] **Step 1: Add the icon**

Create `internal/ui/icons/mdi/road-variant.svg` with the Material Design Icons `road-variant` path:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" class="mdi-road-variant"><path d="M18.1 4.8C18 4.3 17.6 4 17.2 4H13.5L13.8 9H10.2L10.5 4H6.8C6.4 4 6 4.3 5.9 4.8L3.1 18.8C3 19.4 3.5 20 4.1 20H10L10.2 15H13.8L14 20H19.9C20.5 20 21 19.4 20.9 18.8L18.1 4.8M13.9 11L14.1 13H9.9L10.1 11H13.9Z" /></svg>
```

- [ ] **Step 2: Register the icon**

In `internal/ui/icons/icons.go`, add the constant beside `Tire`:

```go
	// RoadVariant marks the Tracks page. The set had no road or map glyph.
	RoadVariant = "road-variant"
```

and add `RoadVariant` to the `All` slice on the line containing `Podium, Trophy, Speedometer, Timer,`.

- [ ] **Step 3: Run the icon tests**

Run: `CGO_ENABLED=0 go test ./internal/ui/icons/ -v`
Expected: PASS. The package's existing test asserts every name in `All` resolves to an embedded file, so a missing or misnamed SVG fails here.

- [ ] **Step 4: Write the frontend helper test**

Create `web/src/entity.test.ts`:

```ts
import { describe, expect, it } from 'vitest'

import { consistencyBand, dimensionLabel, otherLabel } from './entity'

describe('dimension labels', () => {
  it('names the entity and its opposite', () => {
    expect(dimensionLabel('car')).toBe('Car')
    expect(dimensionLabel('track')).toBe('Track')
    // A car page breaks down by track, and vice versa. Getting this backwards
    // would label every row of the pace table wrongly.
    expect(otherLabel('car')).toBe('Track')
    expect(otherLabel('track')).toBe('Car')
  })
})

describe('consistencyBand', () => {
  it('bands on the convention the research found', () => {
    expect(consistencyBand(99.4)).toBe('good')
    expect(consistencyBand(98)).toBe('good')
    expect(consistencyBand(97.9)).toBe('fair')
    expect(consistencyBand(95)).toBe('fair')
    expect(consistencyBand(94.9)).toBe('poor')
  })

  it('has no band for a missing value', () => {
    // Suppressed consistency must not be coloured as though it were poor.
    expect(consistencyBand(null)).toBe('none')
    expect(consistencyBand(undefined)).toBe('none')
  })
})
```

- [ ] **Step 5: Run it to verify it fails**

Run: `cd web && npx vitest run src/entity.test.ts`
Expected: FAIL — cannot resolve `./entity`.

- [ ] **Step 6: Write the helper**

Create `web/src/entity.ts`:

```ts
/*
 * Pure helpers for the Cars and Tracks pages.
 *
 * Separate from the page component so they are testable without a DOM, and so the
 * two pages cannot disagree about what a band means.
 */

/** Dimension is which entity a page is about. */
export type Dimension = 'car' | 'track'

/** dimensionLabel names the entity itself. */
export function dimensionLabel(d: Dimension): string {
  return d === 'car' ? 'Car' : 'Track'
}

/**
 * otherLabel names the opposite dimension, which is what a per-entity breakdown
 * groups by: a car's pace is reported per track, and a track's per car.
 */
export function otherLabel(d: Dimension): string {
  return d === 'car' ? 'Track' : 'Car'
}

/** ConsistencyBand is how a consistency percentage should be presented. */
export type ConsistencyBand = 'good' | 'fair' | 'poor' | 'none'

/**
 * consistencyBand maps a percentage onto a band.
 *
 * The thresholds follow the convention the research found in iRacing Insights,
 * which colours at 98 and 95 percent. They are only meaningful against the
 * per-session definition the store implements; applied to a figure pooled across
 * sessions they would call a consistent driver merely fair.
 *
 * A missing value bands as "none" rather than "poor": consistency is suppressed
 * below five laps, and colouring that as bad would invent a judgement.
 */
export function consistencyBand(pct: number | null | undefined): ConsistencyBand {
  if (pct == null || !Number.isFinite(pct)) return 'none'
  if (pct >= 98) return 'good'
  if (pct >= 95) return 'fair'
  return 'poor'
}
```

- [ ] **Step 7: Run it to verify it passes**

Run: `cd web && npx vitest run src/entity.test.ts`
Expected: PASS, four tests.

- [ ] **Step 8: Add the API types and client methods**

In `web/src/api.ts`, add the interfaces beside the existing row types:

```ts
export interface EntityRow {
  id: number
  name: string
  drivingHours: number
  sessions: number
  laps: number
}

export interface EntityStats {
  id: number
  name: string
  drivingHours: number
  inCarHours: number
  connectedHours: number
  sessions: number
  laps: number
  distanceKm: number
  incidentPoints: number
  incidentPointsPer100Km: number | null
  cleanLapPct: number | null
  races: number
  wins: number
  podiums: number
  avgPositionsGained: number | null
}

export interface PaceRow {
  otherId: number
  otherName: string
  personalBestS: number | null
  bestInRangeS: number | null
  laps: number
  sessions: number
  consistencyPct: number | null
  consistencyDeltaS: number | null
}

export interface ProgressionRow {
  month: string
  bestLapS: number
  laps: number
}

export interface RivalRow {
  name: string
  passedThem: number
  lostTo: number
  net: number
}

export interface QualiPace {
  pairs: number
  avgDeltaS: number | null
}
```

and the methods inside the exported `api` object, after `breakdown`:

```ts
  entities: (f: Filter, by: string) =>
    get<EntityRow[]>(`/api/entities?${toQuery(f, { by })}`),
  entity: (f: Filter, by: string, id: number) =>
    get<EntityStats>(`/api/entity?${toQuery(f, { by, id: String(id) })}`),
  pace: (f: Filter, by: string, id: number) =>
    get<PaceRow[]>(`/api/pace?${toQuery(f, { by, id: String(id) })}`),
  progression: (f: Filter, by: string, id: number, other: number) =>
    get<ProgressionRow[]>(
      `/api/progression?${toQuery(f, { by, id: String(id), other: String(other) })}`,
    ),
  rivals: (f: Filter) => get<RivalRow[]>(`/api/rivals?${toQuery(f)}`),
  qualiPace: (f: Filter, by: string, id: number) =>
    get<QualiPace>(`/api/quali-pace?${toQuery(f, { by, id: String(id) })}`),
```

- [ ] **Step 9: Write the page skeleton**

Create `web/src/pages/Entity.tsx`:

```tsx
/*
 * The Cars and Tracks pages.
 *
 * One component, rendered twice with a different dimension, because the two pages
 * are the same view of two dimensions. A car page lists cars and breaks the
 * selected one down by track; a track page does the mirror image. Writing it once
 * is what keeps the metric definitions from drifting apart.
 */

import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'

import { api } from '../api'
import { hours, num } from '../format'
import { useFilter } from '../useFilter'
import { Empty, ErrorNote, Loading } from '../components/ui'
import { Filters } from '../components/Filters'
import { isEmptyArray, keepPrevious, viewState } from '../query'
import { dimensionLabel, type Dimension } from '../entity'

export function EntityPage({ dimension }: { dimension: Dimension }) {
  const { filter } = useFilter()
  const [params, setParams] = useSearchParams()

  const list = useQuery({
    queryKey: ['entities', dimension, filter],
    queryFn: () => api.entities(filter, dimension),
    ...keepPrevious,
  })

  const items = list.data ?? []
  const selectedParam = params.get(dimension)
  const selected = selectedParam ? Number(selectedParam) : (items[0]?.id ?? null)

  function select(id: number) {
    const next = new URLSearchParams(params)
    next.set(dimension, String(id))
    setParams(next, { replace: true })
  }

  return (
    <>
      <div className="page-head">
        <h1>{dimensionLabel(dimension)}s</h1>
      </div>
      <p className="page-sub">
        How you drive each {dimension}, and whether you are getting better at it.
      </p>

      <Filters hide={[dimension]} />

      {list.isError && <ErrorNote error={list.error} />}

      <div className="explorer">
        <div className="session-list">
          {viewState(list, isEmptyArray) === 'loading' ? (
            <Loading />
          ) : items.length === 0 ? (
            <Empty>Nothing matches this filter.</Empty>
          ) : (
            items.map((e) => (
              <button
                key={e.id}
                type="button"
                className={`session-row${selected === e.id ? ' active' : ''}`}
                onClick={() => select(e.id)}
              >
                <div className="when">{e.name}</div>
                <div className="what">
                  {hours(e.drivingHours)} · {num(e.laps)} laps · {num(e.sessions)} sessions
                </div>
              </button>
            ))
          )}
        </div>

        <div>
          {selected == null ? (
            <Empty>Select a {dimension}.</Empty>
          ) : (
            <Review dimension={dimension} id={selected} />
          )}
        </div>
      </div>
    </>
  )
}

/** Review is the right-hand pane. Task 8 fills it in. */
function Review({ dimension, id }: { dimension: Dimension; id: number }) {
  const { filter } = useFilter()
  const stats = useQuery({
    queryKey: ['entity', dimension, id, filter],
    queryFn: () => api.entity(filter, dimension, id),
    ...keepPrevious,
  })

  if (stats.isError) return <ErrorNote error={stats.error} />
  if (!stats.data) return <Loading />
  return <div className="card">{stats.data.name}</div>
}
```

Note the `explorer` and `session-list` classes are reused from `Sessions`, which already lays out a two-column list-and-detail. The `explorer` grid has three columns there; here the first is absent, so add this rule to `web/src/styles.css` beside the existing `.explorer` block:

```css
/* Cars and Tracks reuse the explorer layout with no facet sidebar. */
.explorer.two {
  grid-template-columns: 260px 1fr;
}
```

and use `className="explorer two"` in `EntityPage`.

- [ ] **Step 10: Add the `hide` prop to Filters**

In `web/src/components/Filters.tsx`, change the props type to include:

```tsx
  /**
   * hide suppresses a dimension's dropdown.
   *
   * The Cars and Tracks pages set their own dimension from the selected entity, so
   * showing a second control for the same thing invites the two to disagree about
   * what the page is about.
   */
  hide?: ('car' | 'track')[]
```

and wrap the track and car `<label>` blocks so each renders only when not hidden, for example `{!hide?.includes('car') && ( … )}`.

- [ ] **Step 11: Wire nav and routes**

In `web/src/App.tsx`, extend `nav`:

```tsx
const nav = [
  { to: '/dashboard', label: 'Dashboard', icon: 'speedometer' },
  { to: '/cars', label: 'Cars', icon: 'car-sports' },
  { to: '/tracks', label: 'Tracks', icon: 'road-variant' },
  { to: '/sessions', label: 'Sessions', icon: 'flag-checkered' },
  { to: '/laps', label: 'Laps', icon: 'timer-outline' },
  { to: '/export', label: 'Export', icon: 'download' },
]
```

add the import `import { EntityPage } from './pages/Entity'`, and add the routes beside the others:

```tsx
          <Route path="/cars" element={<EntityPage dimension="car" />} />
          <Route path="/tracks" element={<EntityPage dimension="track" />} />
```

- [ ] **Step 12: Typecheck, test and build**

```bash
cd web && npx tsc -b --noEmit && npm run test
cd .. && make ui
```
Expected: no type errors; vitest passes; the bundle builds.

- [ ] **Step 13: Verify both pages load**

```bash
make run-ctl &
sleep 6
curl -s -o /dev/null -w "cars   %{http_code}\n" 'http://127.0.0.1:47047/cars'
curl -s -o /dev/null -w "tracks %{http_code}\n" 'http://127.0.0.1:47047/tracks'
curl -s 'http://127.0.0.1:47047/api/entities?by=track&range=all' | head -c 200
curl -s -o /dev/null -w "icon   %{http_code}\n" 'http://127.0.0.1:47047/icons/road-variant.svg'
```
Expected: both routes 200, the icon 200, and the entities call returning a JSON array of tracks.

- [ ] **Step 14: Commit**

```bash
git add internal/ui/icons/ web/src/ internal/web/dist/
git commit -m "Add Cars and Tracks pages with entity list and navigation"
```

---

### Task 8: The review pane

**Files:**
- Modify: `web/src/pages/Entity.tsx`

**Interfaces:**
- Consumes: `api.entity`, `api.pace`, `api.progression`, `api.rivals`, `consistencyBand`, `otherLabel`; `Card`, `Stat`, `Empty`, `Loading`, `ErrorNote`, `Legend` from `../components/ui`; `Chart`, `baseGrid`, `axisStyle`, `valueAxisStyle`, `tooltipStyle` from `../components/Chart`; `StackedByCategory` from `../components/StackedByCategory`.
- Produces: the finished `Review` component.

- [ ] **Step 1: Replace the placeholder Review**

In `web/src/pages/Entity.tsx`, replace the whole `Review` function with the following, and extend the imports to include `Card`, `Stat`, `lapTime`, `pct`, `StackedByCategory`, `Chart` and its helpers, `useTheme`, `consistencyBand`, `otherLabel`, `useMemo` and `useState`.

```tsx
function Review({ dimension, id }: { dimension: Dimension; id: number }) {
  const { filter } = useFilter()
  const theme = useTheme()

  const stats = useQuery({
    queryKey: ['entity', dimension, id, filter],
    queryFn: () => api.entity(filter, dimension, id),
    ...keepPrevious,
  })
  const pace = useQuery({
    queryKey: ['pace', dimension, id, filter],
    queryFn: () => api.pace(filter, dimension, id),
    ...keepPrevious,
  })
  // Race pace against qualifying pace. A positive delta is normal: qualifying runs
  // on low fuel with a clear track.
  const quali = useQuery({
    queryKey: ['quali-pace', dimension, id, filter],
    queryFn: () => api.qualiPace(filter, dimension, id),
    ...keepPrevious,
  })

  // The progression chart needs one value of the opposite dimension, because a
  // line mixing tracks would rise and fall with the circuit rather than the pace.
  const [other, setOther] = useState<number | null>(null)
  const paceRows = pace.data ?? []
  const otherId = other ?? paceRows[0]?.otherId ?? null

  if (stats.isError) return <ErrorNote error={stats.error} />
  if (!stats.data) return <Loading />
  const s = stats.data

  // The entity's own dimension is fixed by the selection, so the filter passed to
  // the shared breakdown must carry it.
  const scoped = { ...filter, [dimension === 'car' ? 'carId' : 'trackId']: id }

  return (
    <>
      <div className="card" style={{ marginBottom: 14 }}>
        <strong style={{ fontSize: 15 }}>{s.name}</strong>
        <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 3 }}>
          {hours(s.drivingHours)} driving · {num(s.sessions)} sessions ·{' '}
          {num(s.laps)} laps · {num(Math.round(s.distanceKm))} km
        </div>
      </div>

      <div className="grid kpis" style={{ gridTemplateColumns: 'repeat(5, 1fr)' }}>
        <Stat
          label="Clean laps"
          value={s.cleanLapPct == null ? '—' : pct(s.cleanLapPct / 100)}
          note="no incident on the lap"
        />
        <Stat
          label="Incident points / 100 km"
          value={s.incidentPointsPer100Km == null ? '—' : s.incidentPointsPer100Km.toFixed(2)}
          note={`${num(s.incidentPoints)} points total`}
        />
        <Stat
          label="Positions gained"
          value={s.avgPositionsGained == null ? '—' : s.avgPositionsGained.toFixed(2)}
          note={`${num(s.races)} races`}
        />
        <Stat
          label="Wins / podiums"
          value={`${num(s.wins)} / ${num(s.podiums)}`}
          note="human races"
        />
        <Stat
          label="Race vs qualifying"
          value={
            quali.data?.avgDeltaS == null ? '—' : `+${quali.data.avgDeltaS.toFixed(2)}s`
          }
          note={
            quali.data == null || quali.data.pairs === 0
              ? 'no paired weekends'
              : `${num(quali.data.pairs)} weekends`
          }
        />
      </div>

      <div className="grid" style={{ marginTop: 14, marginBottom: 14 }}>
        <Card
          title={`Pace by ${otherLabel(dimension).toLowerCase()}`}
          table={<PaceTable rows={paceRows} dimension={dimension} />}
        >
          {viewState(pace, isEmptyArray) === 'loading' ? (
            <Loading />
          ) : paceRows.length === 0 ? (
            <Empty>No timed laps in this range.</Empty>
          ) : (
            <PaceTable rows={paceRows} dimension={dimension} />
          )}
        </Card>
      </div>

      {otherId != null && (
        <div className="grid" style={{ marginBottom: 14 }}>
          <Progression
            dimension={dimension}
            id={id}
            otherId={otherId}
            rows={paceRows}
            onPick={setOther}
            theme={theme}
          />
        </div>
      )}

      <StackedByCategory
        title="Driving time by category"
        by={dimension === 'car' ? 'track' : 'car'}
        filter={scoped}
      />

      <div className="grid" style={{ marginTop: 14 }}>
        <RivalsPanel filter={scoped} />
      </div>
    </>
  )
}

/**
 * PaceTable is both the chart and the table view for pace.
 *
 * It is a table in both slots deliberately: the values are a mix of times,
 * percentages and counts across many rows, which a bar chart cannot carry without
 * either dropping columns or needing two axes.
 */
function PaceTable({ rows, dimension }: { rows: PaceRow[]; dimension: Dimension }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">{otherLabel(dimension)}</th>
            <th className="no-sort num">Personal best</th>
            <th className="no-sort num">Best in range</th>
            <th className="no-sort num">Consistency</th>
            <th className="no-sort num">Laps</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const band = consistencyBand(r.consistencyPct)
            return (
              <tr key={r.otherId}>
                <td>{r.otherName}</td>
                <td className="num">{lapTime(r.personalBestS)}</td>
                <td className="num">{lapTime(r.bestInRangeS)}</td>
                <td className={`num cons-${band}`}>
                  {r.consistencyPct == null ? '—' : `${r.consistencyPct.toFixed(2)}%`}
                </td>
                <td className="num">{num(r.laps)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function Progression({
  dimension,
  id,
  otherId,
  rows,
  onPick,
  theme,
}: {
  dimension: Dimension
  id: number
  otherId: number
  rows: PaceRow[]
  onPick: (id: number) => void
  theme: Theme
}) {
  const { filter } = useFilter()
  const q = useQuery({
    queryKey: ['progression', dimension, id, otherId, filter],
    queryFn: () => api.progression(filter, dimension, id, otherId),
    ...keepPrevious,
  })
  const data = q.data ?? []

  const option = useMemo(
    () => ({
      grid: baseGrid,
      tooltip: {
        trigger: 'axis',
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (ps: { name: string; value: number }[]) => {
          const p = ps[0]
          return p ? `${p.name}<br/><strong>${lapTime(p.value)}</strong> best lap` : ''
        },
      },
      xAxis: {
        type: 'category',
        data: data.map((r) => r.month),
        ...axisStyle(theme.textMuted, theme.baseline),
      },
      // Lower is better for a lap time, so the axis is inverted: a line rising on
      // screen means improvement, which is the direction a reader expects.
      yAxis: { type: 'value', inverse: true, ...valueAxisStyle(theme.textMuted, theme.line) },
      series: [
        {
          type: 'line',
          data: data.map((r) => Number(r.bestLapS.toFixed(3))),
          smooth: false,
          symbolSize: 8,
          lineStyle: { width: 2, color: theme.accent },
          itemStyle: { color: theme.accent },
        },
      ],
    }),
    [data, theme],
  )

  return (
    <Card
      title="Best lap by month"
      table={
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th className="no-sort">Month</th>
                <th className="no-sort num">Best lap</th>
                <th className="no-sort num">Laps</th>
              </tr>
            </thead>
            <tbody>
              {data.map((r) => (
                <tr key={r.month}>
                  <td>{r.month}</td>
                  <td className="num">{lapTime(r.bestLapS)}</td>
                  <td className="num">{num(r.laps)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      }
    >
      <div style={{ marginBottom: 8 }}>
        <select
          value={otherId}
          onChange={(e) => onPick(Number(e.target.value))}
          aria-label={`Choose a ${otherLabel(dimension).toLowerCase()}`}
        >
          {rows.map((r) => (
            <option key={r.otherId} value={r.otherId}>
              {r.otherName}
            </option>
          ))}
        </select>
      </div>
      {viewState(q, isEmptyArray) === 'loading' ? (
        <Loading />
      ) : data.length === 0 ? (
        <Empty>No timed laps here in this range.</Empty>
      ) : (
        <Chart option={option} ariaLabel="Best lap time per month" />
      )}
    </Card>
  )
}

function RivalsPanel({ filter }: { filter: Filter }) {
  const q = useQuery({ queryKey: ['rivals', filter], queryFn: () => api.rivals(filter), ...keepPrevious })
  const rows = (q.data ?? []).slice(0, 12)

  const table = (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Driver</th>
            <th className="no-sort num">Passed them</th>
            <th className="no-sort num">Lost to</th>
            <th className="no-sort num">Net</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.name}>
              <td>{r.name}</td>
              <td className="num">{num(r.passedThem)}</td>
              <td className="num">{num(r.lostTo)}</td>
              <td className="num">{r.net > 0 ? `+${r.net}` : num(r.net)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )

  return (
    <Card title="Rivals" table={table}>
      {viewState(q, isEmptyArray) === 'loading' ? (
        <Loading />
      ) : rows.length === 0 ? (
        <Empty>
          No on-track passes against a named opponent in this range. Practice and AI
          sessions do not contribute.
        </Empty>
      ) : (
        table
      )}
    </Card>
  )
}
```

- [ ] **Step 2: Add the consistency band styles**

Append to `web/src/styles.css`:

```css
/* Consistency bands. The number is always visible, so colour is a second signal
   rather than the only one. */
.cons-good { color: var(--status-good); }
.cons-fair { color: var(--status-warning); }
.cons-poor { color: var(--status-serious); }
.cons-none { color: var(--text-muted); }
```

These four custom properties already exist in `styles.css` — `--status-good: #0ca30c`, `--status-warning: #fab219`, `--status-serious: #ec835a`, `--status-critical: #d03b3b` — so no new colour token is introduced.

- [ ] **Step 3: Typecheck and build**

```bash
cd web && npx tsc -b --noEmit && npm run test && cd .. && make ui
```
Expected: clean typecheck, tests pass, bundle builds. Add the missing type imports (`PaceRow`, `Filter`, `Theme`) that the typechecker reports.

- [ ] **Step 4: Verify against real data**

```bash
lsof -ti:47047 | xargs kill -9 2>/dev/null; make run-ctl &
sleep 6
curl -s 'http://127.0.0.1:47047/api/pace?by=car&id=173&range=all' | head -c 400
curl -s 'http://127.0.0.1:47047/api/entity?by=car&id=173&range=all'
```
Expected: pace rows carrying `personalBestS` and a `consistencyPct` near 99, and stats reporting about 492 driving hours with roughly 2.81 incident points per 100 km.

- [ ] **Step 5: Commit**

```bash
git add web/src/ internal/web/dist/
git commit -m "Fill in the Cars and Tracks review pane"
```

---

### Task 9: Visual verification and documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-08-04-lapdog-design.md` (§18.3, mark the rivals item delivered)

- [ ] **Step 1: Screenshot both pages**

With `make run-ctl` running:

```bash
cd web && node tools/shoot.mjs "PACE BY TRACK" 90,365,all /tmp/lapdog-shots
```

`tools/shoot.mjs` takes a card-title substring and a comma-separated list of ranges. Open the PNGs and check for label collisions, clipped columns and empty panels.

- [ ] **Step 2: Confirm charts still animate**

```bash
cd web && npm run verify-animation
```
Expected: PASS, with all canvases surviving the filter change. The new page keeps `keepPrevious` on every query, so a filter change must not unmount its charts.

- [ ] **Step 3: Run everything**

```bash
make ci
CGO_ENABLED=0 go test -race ./...
```
Expected: both green.

- [ ] **Step 4: Update the README**

In the package table, add the two rows:

```markdown
| `web/src/pages/Entity.tsx` | The Cars and Tracks pages: one component, two dimensions |
| `web/src/entity.ts` | Consistency banding and dimension labels, kept pure for testing |
```

And in "Things that will surprise you", add:

```markdown
**Consistency is computed per session, then averaged.** Pooling every lap a car has ever done at a track measures something else — it mixes repeatability with two years of improvement — and on real data the two forms differ by about 2.5 percentage points, enough to move a driver across the 98% threshold. Both return a plausible percentage, so a test pins the difference.
```

- [ ] **Step 5: Mark the deferred item delivered**

In `docs/superpowers/specs/2026-08-04-lapdog-design.md` §18.3, change the rivals bullet to:

```markdown
- ~~Per-opponent head-to-head records, since `position_events.opponent_name` is already stored.~~ **Delivered 2026-08-05**; see `2026-08-05-lapdog-cars-tracks-design.md` §5.7.
```

- [ ] **Step 6: Commit**

```bash
git add README.md docs/ web/
git commit -m "Verify the Cars and Tracks pages and update the docs"
```

---

---

### Task 10: Top car-and-track combinations heatmap

A dashboard panel, not part of the Cars or Tracks pages: the ten car-and-track
pairings the driver spends most time in, with the time split across session
categories. It answers "where do my hours actually go", which neither the per-car
nor the per-track view can — a pairing is the unit a driver practises.

**Files:**
- Modify: `internal/store/entities.go`
- Modify: `internal/store/entities_test.go`
- Modify: `internal/api/handlers.go`, `internal/api/server.go`, `internal/api/api_test.go`
- Modify: `web/src/api.ts`
- Modify: `web/src/pages/Dashboard.tsx`

**Interfaces:**
- Consumes: `Filter`, `Filter.where()`.
- Produces:
  - `type ComboCell struct { Combo string; Category string; Hours float64; ComboHours float64 }`
  - `func (s *Store) TopCombos(f Filter, limit int) ([]ComboCell, error)`
  - `GET /api/combos?limit=`
  - `api.combos(f, limit)` and `ComboCell` in `web/src/api.ts`

- [ ] **Step 1: Write the failing test**

Append to `internal/store/entities_test.go`:

```go
func TestTopCombosRanksByTotalTime(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	cells, err := s.TopCombos(Filter{}, 10)
	if err != nil {
		t.Fatalf("TopCombos: %v", err)
	}
	if len(cells) == 0 {
		t.Fatal("no cells")
	}
	// The seed pairs the Porsche with Watkins Glen across four sessions and with Spa
	// once, so the Watkins Glen pairing leads.
	if cells[0].Combo != "Porsche 911 GT3 R / Watkins Glen" {
		t.Errorf("cells[0].Combo = %q, want the most-driven pairing first", cells[0].Combo)
	}
	// Every cell of one combo carries that combo's total, which is what lets the
	// frontend order rows without re-summing.
	first := cells[0].ComboHours
	for _, c := range cells {
		if c.Combo == cells[0].Combo && c.ComboHours != first {
			t.Errorf("combo %q has inconsistent ComboHours %v vs %v", c.Combo, c.ComboHours, first)
		}
		if c.Category == "" || !strings.Contains(c.Category, "/") {
			t.Errorf("category %q is not a type/context pair", c.Category)
		}
	}
	// Rows are ordered by combo total descending.
	for i := 1; i < len(cells); i++ {
		if cells[i].ComboHours > cells[i-1].ComboHours {
			t.Errorf("cell %d has a larger combo total than cell %d", i, i-1)
		}
	}
}

// The limit caps distinct combos, not cells: each combo contributes one cell per
// category it appears in.
func TestTopCombosLimitCapsCombosNotCells(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	cells, err := s.TopCombos(Filter{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range cells {
		seen[c.Combo] = true
	}
	if len(seen) != 1 {
		t.Errorf("got %d combos with limit 1: %v", len(seen), seen)
	}
	if len(cells) < 2 {
		t.Errorf("got %d cells; the leading combo spans several categories", len(cells))
	}
}

// Honouring the filter is the point: a narrowed range must narrow the accumulated
// time rather than always reporting all-time totals.
func TestTopCombosHonoursFilter(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	all, err := s.TopCombos(Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := s.TopCombos(Filter{From: "2026-07-14T00:00:00Z"}, 10)
	if err != nil {
		t.Fatal(err)
	}

	sum := func(cs []ComboCell) float64 {
		var v float64
		for _, c := range cs {
			v += c.Hours
		}
		return v
	}
	if sum(narrow) >= sum(all) {
		t.Errorf("narrowed range accumulated %.3f h, unfiltered %.3f h — the filter is "+
			"not being applied", sum(narrow), sum(all))
	}
	// Only the Spa sessions fall after 14 July.
	for _, c := range narrow {
		if !strings.Contains(c.Combo, "Spa") {
			t.Errorf("combo %q is outside the filtered range", c.Combo)
		}
	}
}

func TestTopCombosRejectsNonPositiveLimit(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	// A limit of zero would return nothing and read as "no data" rather than as a
	// caller mistake, so it is clamped to the default instead.
	cells, err := s.TopCombos(Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) == 0 {
		t.Error("a limit of 0 returned nothing; it should clamp to the default")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run TopCombos -v`
Expected: FAIL — `s.TopCombos undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/store/entities.go`:

```go
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

	q := `
WITH combo AS (
  SELECT s.car_id AS ci, s.track_id AS ti,
         COALESCE(s.car_name, 'Unknown car') || ' / ' ||
         COALESCE(s.track_name, 'Unknown track') AS label,
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
```

Add `"strings"` to the test file's imports if it is not already there.

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./internal/store/ -run TopCombos -v`
Expected: PASS, four tests.

- [ ] **Step 5: Add the endpoint**

In `internal/api/server.go`, after the `/api/quali-pace` line:

```go
	mux.HandleFunc("GET /api/combos", s.handleCombos)
```

In `internal/api/handlers.go`:

```go
func (s *Server) handleCombos(w http.ResponseWriter, r *http.Request) {
	f, ok := s.filterOrFail(w, r)
	if !ok {
		return
	}
	// limit is optional; the store clamps a missing or non-positive value.
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			s.fail(w, http.StatusBadRequest,
				fmt.Errorf("%w: limit must be an integer", ErrBadRequest))
			return
		}
		limit = v
	}
	cells, err := s.st.TopCombos(f, limit)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(w, cells)
}
```

In `internal/api/api_test.go`:

```go
func TestCombosEndpoint(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/combos?range=all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var cells []store.ComboCell
	if err := json.Unmarshal(rec.Body.Bytes(), &cells); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestCombosRejectsNonNumericLimit(t *testing.T) {
	h, _, _ := newTestServer(t)
	rec := get(t, h, "/api/combos?limit=ten", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
```

Run: `CGO_ENABLED=0 go test ./internal/api/ -run Combos -v`
Expected: PASS.

- [ ] **Step 6: Add the client method**

In `web/src/api.ts`, beside the other row types:

```ts
export interface ComboCell {
  combo: string
  category: string
  hours: number
  comboHours: number
}
```

and in the `api` object:

```ts
  combos: (f: Filter, limit = 10) =>
    get<ComboCell[]>(`/api/combos?${toQuery(f, { limit: String(limit) })}`),
```

- [ ] **Step 7: Add the dashboard panel**

In `web/src/pages/Dashboard.tsx`, add the query beside the existing four:

```tsx
  const combos = useQuery({
    queryKey: ['combos', filter],
    queryFn: () => api.combos(filter, 10),
    ...keepPrevious,
  })
```

Render it in its own full-width grid row, after the calendar heatmap card and before
`<CarAndTrackBreakdown />`:

```tsx
      <div className="grid" style={{ marginBottom: 14 }}>
        <Card
          title="Where the time goes: top car and track pairings"
          table={<ComboTable cells={combos.data ?? []} />}
        >
          {viewState(combos, isEmptyArray) === 'loading' ? (
            <Loading />
          ) : viewState(combos, isEmptyArray) === 'empty' ? (
            <Empty>No sessions in this range.</Empty>
          ) : (
            <ComboHeatmap cells={combos.data ?? []} theme={theme} />
          )}
        </Card>
      </div>
```

And add the two components near `CalendarHeatmap`:

```tsx
/* ------------------------------------------------- car and track combo heatmap */

/**
 * ComboHeatmap shows the busiest car-and-track pairings against session category.
 *
 * A pairing is the unit a driver actually practises — a car at a track — which
 * neither the per-car nor the per-track breakdown can express. The colour job is
 * sequential because the value is a magnitude, so the eight-slot categorical
 * ceiling does not apply here and every category keeps its own column.
 */
function ComboHeatmap({ cells, theme }: { cells: ComboCell[]; theme: Theme }) {
  const option = useMemo(() => {
    // Rows keep the order the store chose, which is by pairing total descending.
    // Re-sorting here would risk the axis disagreeing with the ranking.
    const rows: string[] = []
    for (const c of cells) if (!rows.includes(c.combo)) rows.push(c.combo)

    // Columns are ordered by total hours so the categories that matter sit left.
    const colTotals = new Map<string, number>()
    for (const c of cells) colTotals.set(c.category, (colTotals.get(c.category) ?? 0) + c.hours)
    const cols = [...colTotals.entries()].sort((a, b) => b[1] - a[1]).map(([k]) => k)

    const max = Math.max(0.01, ...cells.map((c) => c.hours))
    const data = cells.map((c) => [cols.indexOf(c.category), rows.indexOf(c.combo), c.hours])

    return {
      // A category axis on both sides needs room for long pairing labels.
      grid: { left: 8, right: 20, top: 8, bottom: 64, containLabel: true },
      tooltip: {
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (p: { value: [number, number, number] }) =>
          `${rows[p.value[1]]}<br/>${labelForKey(cols[p.value[0]] ?? '')}` +
          `<br/><strong>${hours(p.value[2])}</strong> driving`,
      },
      visualMap: {
        min: 0,
        max,
        type: 'continuous',
        orient: 'horizontal',
        left: 'center',
        bottom: 2,
        // For a continuous visual map ECharts treats itemHeight as the bar's length
        // and itemWidth as its thickness, and swaps them for horizontal orientation.
        itemWidth: 11,
        itemHeight: 90,
        text: [hours(max), '0'],
        textStyle: { color: theme.textMuted, fontSize: 10 },
        inRange: { color: theme.seq },
      },
      xAxis: {
        type: 'category',
        data: cols.map((c) => labelForKey(c)),
        axisLabel: { color: theme.textMuted, fontSize: 10, rotate: 30 },
        axisLine: { lineStyle: { color: theme.baseline } },
        axisTick: { show: false },
        splitArea: { show: true, areaStyle: { color: ['transparent'] } },
      },
      yAxis: {
        type: 'category',
        // Reversed so the busiest pairing is the top row, the conventional
        // direction for a ranking on a category axis.
        data: [...rows].reverse(),
        axisLabel: { color: theme.textSecondary, fontSize: 10 },
        axisLine: { lineStyle: { color: theme.baseline } },
        axisTick: { show: false },
      },
      series: [
        {
          type: 'heatmap',
          data: data.map(([x, y, v]) => [x, rows.length - 1 - y, v]),
          itemStyle: { borderColor: theme.surface, borderWidth: 2 },
        },
      ],
    }
  }, [cells, theme])

  return (
    <Chart
      option={option}
      className="chart tall"
      ariaLabel="Driving hours per car and track pairing, split by session category"
    />
  )
}

function ComboTable({ cells }: { cells: ComboCell[] }) {
  // One row per pairing, with its categories listed, so nothing the heatmap encodes
  // only as colour is unavailable as a number.
  const byCombo = new Map<string, { total: number; parts: ComboCell[] }>()
  for (const c of cells) {
    const e = byCombo.get(c.combo) ?? { total: c.comboHours, parts: [] }
    e.parts.push(c)
    byCombo.set(c.combo, e)
  }
  const ordered = [...byCombo.entries()].sort((a, b) => b[1].total - a[1].total)

  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Car and track</th>
            <th className="no-sort num">Driving</th>
            <th className="no-sort">Split by category</th>
          </tr>
        </thead>
        <tbody>
          {ordered.map(([combo, e]) => (
            <tr key={combo}>
              <td>{combo}</td>
              <td className="num">{hours(e.total)}</td>
              <td>
                {[...e.parts]
                  .sort((a, b) => b.hours - a.hours)
                  .map((p) => `${labelForKey(p.category)} ${p.hours.toFixed(1)}`)
                  .join(' · ')}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
```

Extend the Dashboard imports with `type ComboCell` from `../api`.

- [ ] **Step 8: Typecheck, build and verify**

```bash
cd web && npx tsc -b --noEmit && npm run test && cd .. && make ui
lsof -ti:47047 | xargs kill -9 2>/dev/null; make run-ctl &
sleep 6
curl -s 'http://127.0.0.1:47047/api/combos?range=all&limit=10' | head -c 300
```
Expected: clean typecheck and build; the endpoint returning cells whose `combo`
fields are `car / track` strings and whose `comboHours` repeat per pairing.

- [ ] **Step 9: Screenshot the panel**

```bash
cd web && node tools/shoot.mjs "WHERE THE TIME GOES" 90,all /tmp/lapdog-shots
```
Check the pairing labels are not clipped and the rotated category labels do not
collide. The panel is full width and uses `chart tall`, so ten rows have room.

- [ ] **Step 10: Commit**

```bash
git add internal/store/ internal/api/ web/src/ internal/web/dist/
git commit -m "Add top car and track pairing heatmap to the dashboard"
```

## Definition of done

- [ ] `make ci` passes, and `CGO_ENABLED=0 go test -race ./...` passes.
- [ ] `/cars` and `/tracks` both render, with a working left-hand list and a review pane.
- [ ] Selecting an entity puts it in the URL and survives a reload.
- [ ] The car dropdown is absent on `/cars`, and the track dropdown on `/tracks`.
- [ ] A car's personal best does not change when the date range narrows, while its in-range best does.
- [ ] Per-entity driving hours sum to the figure `Totals` reports, asserted by a test.
- [ ] Consistency is suppressed rather than guessed below five laps, and the per-session test fails under the pooled definition.
- [ ] `npm run verify-animation` passes with the new charts mounted.
- [ ] `/api/entities?by=driver` returns 400; `/api/entity?by=car&id=999999` returns 404.
- [ ] Race-versus-qualifying pace reports an em dash rather than zero when no weekend has both sessions.
- [ ] The dashboard's car-and-track heatmap shows ten pairings, and narrowing the date range reduces the hours it accumulates rather than always showing all-time totals.

## Self-review notes

Checked against the spec section by section.

- §3 navigation and the new icon: Task 7.
- §4 layout, routing and URL selection: Task 7. §4.1 filter semantics: the `hide` prop in Task 7 step 10, and the unfiltered personal best in Task 3.
- §5.1 headline stats: Task 2. §5.2 pace by track: Task 3. §5.3 consistency: Task 4. §5.4 progression: Task 5 and Task 8. §5.5 category split: Task 8, reusing `StackedByCategory`. §5.6 racecraft: Task 2 covers positions gained, wins and podiums.
- §5.7 rivals: Task 5 and Task 8.
- §6 store queries and the fan-out rule: Tasks 1–5, with the guard test in Task 1 and its discrimination check in Task 2 step 5.
- §7 API: Task 6.
- §9 testing: distributed across tasks; the outlier fixture requirement is met by `seedPace` in Task 4 rather than by the synthetic dataset, which contains no outlier.

One spec field is returned but not displayed: `EntityStats.inCarHours` and `connectedHours`. They are included in the struct because the headline line already shows driving hours and the three-counter comparison belongs on the session detail page, which has it. No panel needs them here; they cost nothing to return and a later task can surface them without a schema or API change.

Everything else in the spec maps to a task. The qualifying-versus-race pace delta from §5.6 was initially left out of this plan; the self-review caught it as a spec-coverage gap, and it is now implemented across Task 5 (the store query, paired on `subsession_id`), Task 6 (the endpoint) and Task 8 (the headline stat).
