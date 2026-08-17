package store

import (
	"errors"
	"strings"
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

// The clean-lap percentage is a fraction of timed non-pit laps, and the interface
// shows that fraction beside the percentage. This pins the numerator and
// denominator against a known mix so they cannot drift from the percentage, and
// so the denominator cannot silently become total laps: LapsCompleted is 7 here,
// but only 5 laps are timed and non-pit, of which 3 are clean — 60%, not 3/7.
func TestEntityStatsCleanLapCounts(t *testing.T) {
	s := openTemp(t)

	id, err := s.UpsertSession(&Session{
		SessionKey: "7001/0", SubsessionID: 7001, SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt: "2026-07-27T10:00:00Z", DrivingSeconds: 1800, LapsCompleted: 7,
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}

	laps := []*Lap{
		{LapNumber: 1, LapTimeS: f64p(100.0)},                    // clean
		{LapNumber: 2, LapTimeS: f64p(100.5)},                    // clean
		{LapNumber: 3, LapTimeS: f64p(101.0)},                    // clean
		{LapNumber: 4, LapTimeS: f64p(102.0), IncidentsOnLap: 1}, // timed, dirty
		{LapNumber: 5, LapTimeS: f64p(103.0), IncidentsOnLap: 2}, // timed, dirty
		{LapNumber: 6, LapTimeS: f64p(140.0), IsPitLap: true},    // pit, excluded
		{LapNumber: 7}, // untimed, excluded
	}
	for _, lap := range laps {
		lap.SessionID = id
		if _, err := s.InsertLap(lap); err != nil {
			t.Fatal(err)
		}
	}

	st, err := s.EntityStats(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityStats: %v", err)
	}
	if st.Laps != 7 {
		t.Errorf("Laps = %d, want 7 (LapsCompleted, the wrong denominator)", st.Laps)
	}
	if st.TimedLaps != 5 {
		t.Errorf("TimedLaps = %d, want 5 (pit and untimed laps excluded)", st.TimedLaps)
	}
	if st.CleanLaps != 3 {
		t.Errorf("CleanLaps = %d, want 3", st.CleanLaps)
	}
	if st.CleanLapPct == nil || *st.CleanLapPct != 60 {
		t.Errorf("CleanLapPct = %v, want 60 (3 of 5 timed laps)", st.CleanLapPct)
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
	// The pit lap is deliberately faster than the rest of the field, not slower. A
	// slow pit lap would already sit beyond the 110% outlier threshold and get
	// dropped by that rule alone, so the test would pass whether or not the
	// pit-lap exclusion existed. A fast in-lap that falls inside the threshold is
	// the only fixture that isolates is_pit_lap = 0 as the thing doing the work —
	// do not "fix" this back to a slow pit lap; that would silently remove the
	// coverage.
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2, 100.1, 100.0, 100.1, 90.0}, true)

	got := consistencyOf(t, s, Filter{})
	if got == nil {
		t.Fatal("consistency is nil")
	}
	if *got < 99.7 {
		t.Errorf("consistency = %.2f%%, want >= 99.7 — the fast 90 s pit lap is being "+
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

// decoyProgressionSession inserts one lap for a car/track pair in the given
// month, fast enough that it would win the MIN() if either the car_id or
// track_id filter leaked it into the aggregate for car 173 / track 18.
func decoyProgressionSession(t *testing.T, s *Store, key string, carID, trackID int, started string, lapTimeS float64) {
	t.Helper()
	id, err := s.UpsertSession(&Session{
		SessionKey: key, SubsessionID: 1, SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt: started, DrivingSeconds: 600, LapsCompleted: 1,
		TrackID: intp(trackID), TrackName: strp("Decoy track"),
		CarID: intp(carID), CarName: strp("Decoy car"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertLap(&Lap{SessionID: id, LapNumber: 1, LapTimeS: f64p(lapTimeS)}); err != nil {
		t.Fatal(err)
	}
}

func TestEntityProgressionByMonth(t *testing.T) {
	s := openTemp(t)
	seedPace(t, s, "p1/0", "2026-06-10T10:00:00Z",
		[]float64{105.0, 103.0, 103.2, 103.1, 103.0, 103.4}, false)
	seedPace(t, s, "p2/0", "2026-07-10T10:00:00Z",
		[]float64{104.0, 101.0, 101.2, 101.1, 101.0, 101.4}, false)

	// Decoy A: a different car (991) at the real track (18), with a lap far
	// faster than car 173's real bests. If the car_id filter were dropped
	// from the query, this would leak in and pull both months' MIN down to
	// ~90/~85 rather than ~103/~101.
	decoyProgressionSession(t, s, "decoy-car/0", 991, 18, "2026-06-15T10:00:00Z", 90.0)
	decoyProgressionSession(t, s, "decoy-car/1", 991, 18, "2026-07-15T10:00:00Z", 85.0)
	// Decoy B: the real car (173) at a different track (992), also faster
	// than the real bests. If the track_id filter were dropped, this would
	// leak in and pull both months' MIN down to ~80/~75.
	decoyProgressionSession(t, s, "decoy-track/0", 173, 992, "2026-06-20T10:00:00Z", 80.0)
	decoyProgressionSession(t, s, "decoy-track/1", 173, 992, "2026-07-20T10:00:00Z", 75.0)

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
	// June's best is 103.0, July's 101.0: the driver improved. Pinned to
	// absolute values (not just relative order) so that either decoy leaking
	// in — which would lower both months together — is caught rather than
	// passing because the ordering happens to survive.
	if rows[0].BestLapS < 102.9 || rows[0].BestLapS > 103.1 {
		t.Errorf("June BestLapS = %.3f, want ~103.0 (a decoy car or track may be leaking in)", rows[0].BestLapS)
	}
	if rows[1].BestLapS < 100.9 || rows[1].BestLapS > 101.1 {
		t.Errorf("July BestLapS = %.3f, want ~101.0 (a decoy car or track may be leaking in)", rows[1].BestLapS)
	}
	if rows[0].BestLapS <= rows[1].BestLapS {
		t.Errorf("June %.3f should be slower than July %.3f", rows[0].BestLapS, rows[1].BestLapS)
	}
	if rows[1].Laps != 6 {
		t.Errorf("July laps = %d, want 6", rows[1].Laps)
	}
}

func TestEntityProgressionUsesLocalMonth(t *testing.T) {
	s := openTemp(t)
	decoyProgressionSession(t, s, "evening/0", 173, 18, "2026-09-01T02:00:00Z", 101.5)

	rows, err := s.EntityProgression(Filter{}, "car", 173, 18)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Month != "2026-08" {
		t.Fatalf("progression = %+v, want the evening lap under local month 2026-08", rows)
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

// Race pace against qualifying pace, paired within one race weekend.
//
// The pair must come from the same subsession, or a qualifying session at one track
// would be compared against a race at another.
func TestQualifyingVsRacePairsWithinASubsession(t *testing.T) {
	s := openTemp(t)
	mk := func(key string, subsession, num, carID int, st string, best float64, quali *float64) {
		t.Helper()
		if _, err := s.UpsertSession(&Session{
			SessionKey: key, SubsessionID: subsession, SessionNum: num,
			SessionType: st, EventContext: "OfficialRace",
			StartedAt: "2026-07-12T18:00:00Z", DrivingSeconds: 600, LapsCompleted: 5,
			TrackID: intp(18), TrackName: strp("Watkins Glen"),
			CarID: intp(carID), CarName: strp("Porsche 911 GT3 R"),
			BestLapTimeS: f64p(best), QualifyBestTimeS: quali,
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Qualifying best 100.0, race best 101.5: race pace is 1.5 s slower, which is
	// the normal direction — lower fuel and a clear track in qualifying.
	mk("7001/1", 7001, 1, 173, "Qualify", 100.0, f64p(100.0))
	mk("7001/2", 7001, 2, 173, "Race", 101.5, nil)
	// Decoy: a different car (500) sharing the same subsession (7001), the way a
	// multiclass field would — one subsession, several cars. Its times are far
	// faster than the real car's, so if the car_id filter were dropped from
	// either CTE the MIN() for subsession 7001 would pick up the decoy's time
	// instead of car 173's, dragging AvgDeltaS far outside the 1.49-1.51 window.
	//
	// A decoy placed in a *different* subsession does not exercise this: the
	// join's `ON r.ss = q.ss` already isolates one car per subsession whenever
	// each subsession contains only one car's sessions, so dropping the car_id
	// filter would not leak anything cross-subsession in that shape. Sharing the
	// subsession is what makes the entity filter the only thing keeping the two
	// cars' bests apart.
	mk("7001/3", 7001, 3, 500, "Qualify", 10.0, f64p(10.0))
	mk("7001/4", 7001, 4, 500, "Race", 20.0, nil)

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
		BestLapTimeS:       f64p(101.0),
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

// TestQualifyingVsRaceDoesNotCrossSubsessions is not from the task-5 brief. The
// brief's own fixtures never seed a Qualify session in one subsession alongside a
// Race session in a different one, so they cannot tell a correct per-subsession
// join apart from a broken implementation that aggregates qualifying and race bests
// globally and then pairs them regardless of which weekend they came from.
//
// Subsession 9101 has a Qualify session only; subsession 9102 has a Race session
// only, at a different (fictitious) point in the season. Neither subsession has
// both halves of a pair, so a correct implementation must report zero pairs. A
// global-aggregate implementation would instead pair 9101's qualifying best against
// 9102's race best and report one pair with a large, wrong delta.
func TestQualifyingVsRaceDoesNotCrossSubsessions(t *testing.T) {
	s := openTemp(t)
	if _, err := s.UpsertSession(&Session{
		SessionKey: "9101/0", SubsessionID: 9101, SessionNum: 0,
		SessionType: "Qualify", EventContext: "OfficialRace",
		StartedAt: "2026-07-14T18:00:00Z", DrivingSeconds: 300, LapsCompleted: 3,
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		QualifyBestTimeS:   f64p(110.0),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertSession(&Session{
		SessionKey: "9102/0", SubsessionID: 9102, SessionNum: 0,
		SessionType: "Race", EventContext: "OfficialRace",
		StartedAt: "2026-07-21T18:00:00Z", DrivingSeconds: 1800, LapsCompleted: 10,
		TrackID: intp(18), TrackName: strp("Watkins Glen"),
		CarID: intp(173), CarName: strp("Porsche 911 GT3 R"),
		BestLapTimeS:       f64p(95.0),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.QualifyingVsRace(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("QualifyingVsRace: %v", err)
	}
	if got.Pairs != 0 || got.AvgDeltaS != nil {
		t.Errorf("got %+v, want zero pairs — 9101's qualifying and 9102's race "+
			"belong to different weekends and must not be paired", got)
	}
}

// TestConsistencyOutlierBaselineIsPerSession guards the outlier rule's baseline:
// a lap is compared against its own session's best, not the entity's all-time
// best. Every other fixture in this file uses sessions of similar pace, which
// gives the same answer under either baseline. Here the two sessions are
// deliberately far apart: a fast one around 100 s and a slow one around 140 s.
//
// Measured per-session, each session's own laps are compared to its own best and
// both contribute a normal, unremarkable consistency figure. Measured against an
// entity-wide baseline, the fast session's ~100 s best becomes the threshold
// anchor for both sessions: the slow session's laps are all well beyond 110% of
// 100 and are dropped as outliers wholesale, so the slow session vanishes from
// the average entirely and the result silently collapses to the fast session's
// figure alone (~99.88%, above the assertion's ceiling below).
func TestConsistencyOutlierBaselineIsPerSession(t *testing.T) {
	s := openTemp(t)
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2, 100.1, 100.0, 100.2}, false)
	seedPace(t, s, "p2/0", "2026-07-02T10:00:00Z",
		[]float64{148.0, 140.0, 141.0, 141.0, 141.0, 140.0, 141.0}, false)

	got := consistencyOf(t, s, Filter{})
	if got == nil {
		t.Fatal("consistency is nil; both sessions have six laps after the first")
	}
	// Correctly averaged per session this comes to about 99.57% (the fast
	// session near 99.85% and the slow session near 99.29%, averaged). Under an
	// entity-wide baseline the slow session's laps are dropped as outliers
	// entirely and only the fast session's ~99.85% would remain, so this ceiling
	// sits between the two readings.
	if *got >= 99.7 {
		t.Errorf("consistency = %.2f%%, want < 99.7 — the slow session's laps are "+
			"likely being measured against the fast session's best rather than "+
			"its own", *got)
	}
}

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

// seedStraddlingCombo adds one car-and-track pairing whose sessions fall on both
// sides of the 14 July filter boundary used by TestTopCombosHonoursFilter: two
// sessions before it (1800s = 0.5h each) and one after (3600s = 1.0h).
//
// This is the fixture shape the plain seed() lacks. Every seed() pairing sits
// entirely inside or entirely outside a narrowed range, so filtering the ranking
// and filtering the accumulated sum happen to agree even when only the ranking is
// actually filtered — the bug the honours-filter test exists to catch is invisible
// against that data. With a pairing split across the boundary, the correct
// accumulated hours for the narrowed query (1.0h, the one post-boundary session)
// differs from the buggy accumulated hours (2.0h, all three sessions), so the two
// implementations can no longer coincide.
//
// Do not collapse these back into three same-side sessions "to simplify the
// fixture" — that removes the one thing this test is checking. It has already
// happened three times elsewhere in this plan.
func seedStraddlingCombo(t *testing.T, s *Store) {
	t.Helper()
	rows := []struct {
		key     string
		started string
		drive   float64
	}{
		{"9001/0", "2026-07-02T10:00:00Z", 1800}, // before the boundary
		{"9002/0", "2026-07-09T10:00:00Z", 1800}, // before the boundary
		{"9003/0", "2026-07-16T10:00:00Z", 3600}, // after the boundary
	}
	for _, r := range rows {
		rec := &Session{
			SessionKey: r.key, SubsessionID: 1, SessionNum: 0,
			SessionType: "Practice", EventContext: "OfficialPractice",
			StartedAt:        r.started,
			ConnectedSeconds: r.drive, InCarSeconds: r.drive, DrivingSeconds: r.drive,
			LapsCompleted: 2, Incidents: 0,
			TrackID: intp(777), TrackName: strp("Sebring"),
			CarID: intp(777), CarName: strp("BMW M4 GT3"),
			ClassifySourceJSON: "{}", IncidentSource: "yaml",
		}
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatal(err)
		}
	}
}

// Honouring the filter is the point: a narrowed range must narrow the accumulated
// time rather than always reporting all-time totals.
func TestTopCombosHonoursFilter(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	seedStraddlingCombo(t, s)

	const straddler = "BMW M4 GT3 / Sebring"

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
	// Only the Spa sessions and the straddler's one post-boundary session fall
	// after 14 July; the straddler's two pre-boundary sessions must not.
	for _, c := range narrow {
		if !strings.Contains(c.Combo, "Spa") && c.Combo != straddler {
			t.Errorf("combo %q is outside the filtered range", c.Combo)
		}
	}
	// The discriminating check: the straddler has 2.0h total (two 0.5h sessions
	// before the boundary, one 1.0h session after), but only its post-boundary
	// session — 1.0h — may be counted once the range narrows. A store that filters
	// the ranking but not the accumulated sum would report 2.0h here (its total
	// minus nothing, since the JOIN still finds all three of its sessions), not
	// 1.0h.
	found := false
	for _, c := range narrow {
		if c.Combo == straddler {
			found = true
			if c.Hours != 1.0 {
				t.Errorf("straddler's narrowed hours = %.3f, want 1.0 — only its "+
					"post-boundary session should be counted", c.Hours)
			}
			if c.ComboHours != 1.0 {
				t.Errorf("straddler's narrowed ComboHours = %.3f, want 1.0", c.ComboHours)
			}
		}
	}
	if !found {
		t.Fatal("straddler combo missing from the narrowed result")
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

// ------------------------------------------------------- distance and exposure

// seedDistance inserts one session with a track length recorded, which is what
// makes DistanceKm — and therefore the per-100-km incident rate — computable.
//
// No other fixture in this package or in internal/api sets TrackLengthKm, so
// before this helper existed DistanceKm was always 0 and IncidentPointsPer100Km
// always nil: the whole rate branch went unexecuted across the entire suite, and
// inverting the ratio, dividing by hours or dropping the ×100 would each have left
// every test green.
func seedDistance(t *testing.T, s *Store, key, started string,
	trackID int, trackName string, lengthKm float64, laps, incidents int,
) {
	t.Helper()
	_, err := s.UpsertSession(&Session{
		SessionKey: key, SubsessionID: 1, SessionNum: 0,
		SessionType: "Practice", EventContext: "OfficialPractice",
		StartedAt: started, DrivingSeconds: 3600,
		LapsCompleted: laps, Incidents: incidents,
		TrackID: intp(trackID), TrackName: strp(trackName),
		TrackLengthKm: f64p(lengthKm),
		CarID:         intp(173), CarName: strp("Porsche 911 GT3 R"),
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The incident rate is per 100 km, not per hour and not a rate of kilometres per
// point. Safety Rating normalises incidents by corners driven, which LapDog cannot
// reproduce without per-track corner counts; distance is the closest available
// proxy for exposure and is far better than time, which would punish a driver for a
// long practice stint at a slow track.
//
// The fixture's numbers are chosen so that every plausible way of getting the
// arithmetic wrong lands somewhere else: the correct answer is 20.00, the inverted
// ratio gives 500, per driving hour gives 12.5, and dropping the ×100 gives 0.20.
func TestEntityStatsIncidentRatePer100Km(t *testing.T) {
	s := openTemp(t)
	// 20 laps of a 5.0 km track is 100 km; 10 laps of a 2.5 km track is 25 km.
	seedDistance(t, s, "d1/0", "2026-07-01T10:00:00Z", 18, "Watkins Glen", 5.0, 20, 5)
	seedDistance(t, s, "d2/0", "2026-07-02T10:00:00Z", 341, "Spa", 2.5, 10, 20)

	got, err := s.EntityStats(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityStats: %v", err)
	}
	if got.DistanceKm < 124.999 || got.DistanceKm > 125.001 {
		t.Errorf("DistanceKm = %.4f, want 125 (20×5.0 + 10×2.5)", got.DistanceKm)
	}
	if got.IncidentPoints != 25 {
		t.Fatalf("IncidentPoints = %d, want 25", got.IncidentPoints)
	}
	if got.IncidentPointsPer100Km == nil {
		t.Fatal("IncidentPointsPer100Km is nil with 125 km driven; the rate is " +
			"only suppressed at zero exposure")
	}
	// 100 × 25 / 125 = 20.00 points per 100 km.
	if *got.IncidentPointsPer100Km < 19.999 || *got.IncidentPointsPer100Km > 20.001 {
		t.Errorf("IncidentPointsPer100Km = %.4f, want 20.00 — 100 × 25 points / 125 km",
			*got.IncidentPointsPer100Km)
	}
}

// A track with no recorded length contributes no distance, so the rate stays
// suppressed rather than being computed against a partial denominator.
func TestEntityStatsRateSuppressedWithoutTrackLength(t *testing.T) {
	s := openTemp(t)
	seed(t, s)

	got, err := s.EntityStats(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("EntityStats: %v", err)
	}
	if got.DistanceKm != 0 {
		t.Errorf("DistanceKm = %v, want 0 — the shared seed records no track length", got.DistanceKm)
	}
	if got.IncidentPointsPer100Km != nil {
		t.Errorf("IncidentPointsPer100Km = %v, want nil at zero exposure — a rate of "+
			"0.00 would claim a clean record the data cannot support",
			*got.IncidentPointsPer100Km)
	}
}

// ------------------------------------------------------------------- racecraft

// seedRacecraftRace inserts one race with a grid and finish position, plus its
// position events.
//
// A nil start or finish means the sim never logged it, which is a real state: a
// race can be recorded without either.
func seedRacecraftRace(t *testing.T, s *Store, key, ctx, started string,
	carID, trackID int, sessionType string, start, finish *int, evs []PositionEvent,
) {
	t.Helper()
	id, err := s.UpsertSession(&Session{
		SessionKey: key, SubsessionID: 1, SessionNum: 0,
		SessionType: sessionType, EventContext: ctx,
		StartedAt: started, DrivingSeconds: 1800, LapsCompleted: 10,
		TrackID: intp(trackID), TrackName: strp("Watkins Glen"),
		CarID: intp(carID), CarName: strp("Porsche 911 GT3 R"),
		StartingPosition: start, FinishPosition: finish,
		ClassifySourceJSON: "{}", IncidentSource: "yaml",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		ev.SessionID = id
		if _, err := s.InsertPositionEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
}

// Only cause = 'OnTrack' counts as a pass. Inheriting a place because the other car
// pitted or left the world is not overtaking, and counting it would flatter the
// record — which is exactly what the decoy events here would do if the cause
// predicate were removed.
func TestRacecraftCountsOnlyOnTrackPasses(t *testing.T) {
	s := openTemp(t)
	seedRacecraftRace(t, s, "r1/2", "OfficialRace", "2026-07-08T18:45:00Z",
		173, 18, "Race", intp(6), intp(3), []PositionEvent{
			// Two real passes and one real loss.
			{LapNumber: 2, SessionTimeS: 100, FromPosition: 6, ToPosition: 5, Cause: CauseOnTrack},
			{LapNumber: 3, SessionTimeS: 150, FromPosition: 5, ToPosition: 4, Cause: CauseOnTrack},
			{LapNumber: 4, SessionTimeS: 200, FromPosition: 4, ToPosition: 5, Cause: CauseOnTrack},
			// Decoys: a place inherited because someone else stopped, and one
			// because they left the world. Both move the position forwards, so
			// dropping the cause predicate silently counts them as passes.
			{LapNumber: 5, SessionTimeS: 250, FromPosition: 5, ToPosition: 4, Cause: CauseOpponentPit},
			{LapNumber: 6, SessionTimeS: 300, FromPosition: 4, ToPosition: 3, Cause: CauseOpponentOffWorld},
		})

	got, err := s.Racecraft(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("Racecraft: %v", err)
	}
	if got.PassesMade != 2 {
		t.Errorf("PassesMade = %d, want 2 — the OpponentPit and OpponentOffWorld "+
			"events are inherited places, not overtakes", got.PassesMade)
	}
	if got.TimesPassed != 1 {
		t.Errorf("TimesPassed = %d, want 1", got.TimesPassed)
	}
}

// The averages are taken over races with both positions recorded, and Races is the
// count of that same set, so the denominator cannot disagree with the numbers
// beside it. A qualifying session and a race missing its grid position are both
// present here to pin that.
func TestRacecraftGridToFinish(t *testing.T) {
	s := openTemp(t)
	seedRacecraftRace(t, s, "r1/2", "OfficialRace", "2026-07-08T18:45:00Z",
		173, 18, "Race", intp(8), intp(3), nil)
	seedRacecraftRace(t, s, "r2/2", "OfficialRace", "2026-07-15T18:45:00Z",
		173, 18, "Race", intp(4), intp(2), nil)
	// A race the sim never logged a grid position for: it cannot contribute to
	// either average, so it must not inflate Races either.
	seedRacecraftRace(t, s, "r3/2", "OfficialRace", "2026-07-22T18:45:00Z",
		173, 18, "Race", nil, intp(9), nil)
	// A qualifying session with both positions set. Qualifying is not racecraft,
	// and its P1 would drag the grid average from 6.0 down to 4.33 if the
	// session-type predicate were dropped.
	seedRacecraftRace(t, s, "r4/1", "OfficialRace", "2026-07-22T18:15:00Z",
		173, 18, "Qualify", intp(1), intp(1), nil)

	got, err := s.Racecraft(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("Racecraft: %v", err)
	}
	if got.Races != 2 {
		t.Errorf("Races = %d, want 2 — only races with both a grid and a finish "+
			"position count, and qualifying is not a race", got.Races)
	}
	if got.AvgStartPosition == nil || got.AvgFinishPosition == nil {
		t.Fatalf("averages are nil with two qualifying races: %+v", got)
	}
	if *got.AvgStartPosition < 5.999 || *got.AvgStartPosition > 6.001 {
		t.Errorf("AvgStartPosition = %.3f, want 6.0 ((8+4)/2)", *got.AvgStartPosition)
	}
	if *got.AvgFinishPosition < 2.499 || *got.AvgFinishPosition > 2.501 {
		t.Errorf("AvgFinishPosition = %.3f, want 2.5 ((3+2)/2)", *got.AvgFinishPosition)
	}
}

// With no race in range the averages are nil rather than zero. Zero would render as
// "grid 0.0 → finish 0.0", claiming a position that does not exist.
func TestRacecraftNilWithoutRaces(t *testing.T) {
	s := openTemp(t)
	seedPace(t, s, "p1/0", "2026-07-01T10:00:00Z",
		[]float64{105.0, 100.0, 100.1, 100.2, 100.1, 100.0}, false)

	got, err := s.Racecraft(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("Racecraft: %v", err)
	}
	if got.Races != 0 || got.AvgStartPosition != nil || got.AvgFinishPosition != nil {
		t.Errorf("got %+v, want zero races and nil averages for a car that only practised", got)
	}
	if got.PassesMade != 0 || got.TimesPassed != 0 {
		t.Errorf("got %+v, want no passes", got)
	}
}

// Racecraft is scoped to one entity and honours the filter. The AI race here is
// counted by default and dropped under ExcludeAI, which is what proves the
// exclusion belongs to the caller rather than being hard-coded in the query — the
// spec calls these figures human-only and the frontend panel opts in explicitly.
func TestRacecraftScopesToEntityAndHonoursFilter(t *testing.T) {
	s := openTemp(t)
	onTrackGain := []PositionEvent{
		{LapNumber: 2, SessionTimeS: 100, FromPosition: 6, ToPosition: 5, Cause: CauseOnTrack},
	}
	// The entity under test: one human race with one pass.
	seedRacecraftRace(t, s, "r1/2", "OfficialRace", "2026-07-08T18:45:00Z",
		173, 18, "Race", intp(6), intp(5), onTrackGain)
	// A different car at the same track, with two passes. If the entity predicate
	// were dropped these would be added to car 173's record.
	seedRacecraftRace(t, s, "decoy/2", "OfficialRace", "2026-07-09T18:45:00Z",
		991, 18, "Race", intp(10), intp(4), []PositionEvent{
			{LapNumber: 2, SessionTimeS: 100, FromPosition: 10, ToPosition: 9, Cause: CauseOnTrack},
			{LapNumber: 3, SessionTimeS: 150, FromPosition: 9, ToPosition: 8, Cause: CauseOnTrack},
		})
	// An AI race for the same car, with its own pass and a very different grid.
	seedRacecraftRace(t, s, "ai/2", "AI", "2026-07-10T18:45:00Z",
		173, 18, "Race", intp(20), intp(1), onTrackGain)

	all, err := s.Racecraft(Filter{}, "car", 173)
	if err != nil {
		t.Fatalf("Racecraft: %v", err)
	}
	if all.PassesMade != 2 || all.Races != 2 {
		t.Errorf("unfiltered got %+v, want 2 passes over 2 races — car 991's two "+
			"passes must not leak in, and the AI race is included by default", all)
	}

	human, err := s.Racecraft(Filter{ExcludeAI: true}, "car", 173)
	if err != nil {
		t.Fatalf("Racecraft with ExcludeAI: %v", err)
	}
	if human.PassesMade != 1 || human.Races != 1 {
		t.Errorf("ExcludeAI got %+v, want 1 pass over 1 race", human)
	}
	if human.AvgStartPosition == nil || *human.AvgStartPosition != 6 {
		t.Errorf("AvgStartPosition = %v, want 6 — the AI race started P20 and is excluded",
			human.AvgStartPosition)
	}
}

// Racecraft serves the track page from the same implementation, and rejects a
// dimension that is not allowlisted rather than silently answering a different
// question.
func TestRacecraftByTrackAndBadDimension(t *testing.T) {
	s := openTemp(t)
	seedRacecraftRace(t, s, "r1/2", "OfficialRace", "2026-07-08T18:45:00Z",
		173, 18, "Race", intp(6), intp(3), []PositionEvent{
			{LapNumber: 2, SessionTimeS: 100, FromPosition: 6, ToPosition: 5, Cause: CauseOnTrack},
		})

	got, err := s.Racecraft(Filter{}, "track", 18)
	if err != nil {
		t.Fatalf("Racecraft by track: %v", err)
	}
	if got.PassesMade != 1 || got.Races != 1 {
		t.Errorf("by track got %+v, want the same single race and pass", got)
	}

	if _, err := s.Racecraft(Filter{}, "driver", 18); !errors.Is(err, ErrBadGroupBy) {
		t.Errorf("Racecraft by driver: err = %v, want ErrBadGroupBy", err)
	}
}
