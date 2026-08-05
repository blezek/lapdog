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
	// Ordered by driving time descending, so Watkins Glen (four sessions) leads
	// over Spa (two sessions).
	if rows[0].Name != "Watkins Glen" {
		t.Errorf("rows[0].Name = %q, want the most-driven track first", rows[0].Name)
	}
	if rows[0].ID != 18 {
		t.Errorf("rows[0].ID = %d, want 18", rows[0].ID)
	}
	if rows[0].Sessions != 4 {
		t.Errorf("Watkins Glen sessions = %d, want 4", rows[0].Sessions)
	}
	if rows[1].DrivingHours > rows[0].DrivingHours {
		t.Error("rows are not ordered by driving time descending")
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

// TestEntityListRenamedEntityStaysOneRow guards the fix for a regression
// introduced by round 1 of this task: iRacing renames cars and track
// configurations between seasons, so two sessions can share a car_id while
// carrying different car_name values. Grouping by both id and name (round 1's
// fix) split that single car into two rows with its hours divided between
// them — a wrong total. The correct fix aggregates the name with MAX instead
// of grouping by it, keeping one row per id with the true, undivided totals.
func TestEntityListRenamedEntityStaysOneRow(t *testing.T) {
	s := openTemp(t)

	sessions := []struct {
		key, name, started string
		subsession, drive  int
	}{
		{"9001/0", "Porsche 911 GT3 R", "2026-06-01T10:00:00Z", 9001, 1000},
		{"9002/0", "Porsche 911 GT3 R (992)", "2026-07-01T10:00:00Z", 9002, 2000},
	}
	for _, r := range sessions {
		rec := &Session{
			SessionKey: r.key, SubsessionID: r.subsession, SessionNum: 0,
			SessionType: "Race", EventContext: "OfficialRace",
			StartedAt:      r.started,
			DrivingSeconds: float64(r.drive),
			CarID:          intp(173), CarName: strp(r.name),
			TrackID: intp(18), TrackName: strp("Watkins Glen"),
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.EntityList(Filter{}, "car")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows for one renamed car, want 1 — otherwise the left column lists the same car twice: %+v", len(rows), rows)
	}
	if want := float64(1000+2000) / 3600.0; rows[0].DrivingHours < want-0.0001 || rows[0].DrivingHours > want+0.0001 {
		t.Errorf("DrivingHours = %.4f, want %.4f (the sum of both sessions, not divided between two rows)", rows[0].DrivingHours, want)
	}
	// The arbitrary-name defect itself has no test: for a fixed query and
	// dataset, SQLite's MAX picks the same value every time, so a test cannot
	// force that choice to vary. This assertion pins the replacement contract
	// instead — the entity list reports the MAX of the names a renamed entity
	// has carried, chosen for stability rather than recency.
	wantName := sessions[0].name
	if sessions[1].name > wantName {
		wantName = sessions[1].name
	}
	if rows[0].Name != wantName {
		t.Errorf("Name = %q, want %q (the MAX of the names this car has carried, chosen for stability rather than recency)", rows[0].Name, wantName)
	}
	if rows[0].Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", rows[0].Sessions)
	}
}

func TestEntityListRejectsUnknownDimension(t *testing.T) {
	s := openTemp(t)
	if _, err := s.EntityList(Filter{}, "driver"); !errors.Is(err, ErrBadGroupBy) {
		t.Errorf("EntityList with an unknown dimension = %v, want ErrBadGroupBy", err)
	}
}

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
	// The shared seed records no finish or starting positions on any session, so
	// none of its rows count as a race under the recorded-finish definition —
	// that is why the result metrics are exercised by
	// TestEntityStatsResultMetrics below rather than here. Races, Wins and
	// Podiums must read zero and AvgPositionsGained must read nil (an em dash
	// on the page), not a zero, since a car with no recorded results is a
	// different state from a car with three losses.
	if st.Races != 0 {
		t.Errorf("Races = %d, want 0 (the seed records no finish positions)", st.Races)
	}
	if st.Wins != 0 {
		t.Errorf("Wins = %d, want 0 (the seed records no finish positions)", st.Wins)
	}
	if st.Podiums != 0 {
		t.Errorf("Podiums = %d, want 0 (the seed records no finish positions)", st.Podiums)
	}
	if st.AvgPositionsGained != nil {
		t.Errorf("AvgPositionsGained = %v, want nil (the seed records no starting/finish positions)", *st.AvgPositionsGained)
	}
}

// TestEntityStatsResultMetrics exercises the recorded-finish path that
// TestEntityStatsForCar's shared seed never touches. Three race sessions for
// one car and track carry starting and finish positions: one a win from 3rd,
// one a podium from 5th, and one a two-place loss from 6th to 8th.
func TestEntityStatsResultMetrics(t *testing.T) {
	s := openTemp(t)

	races := []struct {
		key        string
		subsession int
		start, fin int
	}{
		{"5001/0", 5001, 3, 1}, // win, gained 2
		{"5002/0", 5002, 5, 3}, // podium, gained 2
		{"5003/0", 5003, 6, 8}, // finished, lost 2
	}
	for _, r := range races {
		rec := &Session{
			SessionKey: r.key, SubsessionID: r.subsession, SessionNum: 0,
			SessionType: "Race", EventContext: "OfficialRace",
			StartedAt:      "2026-07-25T10:00:00Z",
			DrivingSeconds: 3000,
			CarID:          intp(173), CarName: strp("Porsche 911 GT3 R"),
			TrackID: intp(18), TrackName: strp("Watkins Glen"),
			StartingPosition: intp(r.start), FinishPosition: intp(r.fin),
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatal(err)
		}
	}

	st, err := s.EntityStats(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityStats: %v", err)
	}
	if st.Races != 3 {
		t.Errorf("Races = %d, want 3", st.Races)
	}
	if st.Wins != 1 {
		t.Errorf("Wins = %d, want 1", st.Wins)
	}
	if st.Podiums != 2 {
		t.Errorf("Podiums = %d, want 2", st.Podiums)
	}
	// (2 + 2 - 2) / 3.
	want := (2.0 + 2.0 - 2.0) / 3.0
	if st.AvgPositionsGained == nil {
		t.Fatal("AvgPositionsGained = nil, want a value since every race here has a recorded result")
	}
	if diff := *st.AvgPositionsGained - want; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("AvgPositionsGained = %.4f, want %.4f (a negative value would mean places lost on average)", *st.AvgPositionsGained, want)
	}
}

// A car can have driving time with no completed timed laps at all — time in the
// garage, or a session that ended before the driver crossed the line. Task 3's
// pace table is built to still show such a car (see
// TestEntityPaceIncludesEntityWithNoTimedLaps), so EntityStats must not error for
// it either. Nil, not zero, is the correct CleanLapPct here: a car with no timed
// laps has no clean-lap percentage, and reporting 0% would claim every lap was
// dirty.
func TestEntityStatsWithNoTimedLaps(t *testing.T) {
	s := openTemp(t)

	rec := &Session{
		SessionKey: "6001/0", SubsessionID: 6001, SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt:      "2026-07-26T10:00:00Z",
		DrivingSeconds: 900,
		CarID:          intp(173), CarName: strp("Porsche 911 GT3 R"),
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	}
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}

	st, err := s.EntityStats(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityStats: %v", err)
	}
	if want := 900.0 / 3600.0; st.DrivingHours != want {
		t.Errorf("DrivingHours = %v, want %v", st.DrivingHours, want)
	}
	if st.CleanLapPct != nil {
		t.Errorf("CleanLapPct = %v, want nil for a car with no timed laps", *st.CleanLapPct)
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
