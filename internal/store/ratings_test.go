package store

import "testing"

// rated writes a session with the given ratings on the given day.
func rated(t *testing.T, s *Store, key, day string, ir *int, sr *float64) {
	t.Helper()
	rec := &Session{
		UUID: "u-" + key, SessionKey: key, SessionType: "Race",
		EventContext: "OfficialRace",
		StartedAt:    day + "T18:00:00Z",
		DriverUserID: intp(271828), DriverIRating: ir, DriverSafetyRating: sr,
	}
	if sr != nil {
		rec.DriverLicString = strp("A 3.55")
	}
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatalf("UpsertSession %s: %v", key, err)
	}
}

// The progression runs oldest first, and the headline values come from its ends.
//
// Order is what makes the series readable as a progression at all, and taking the
// headline from the same ordering is what stops the card disagreeing with the chart
// beside it.
func TestRatingsProgressionIsOldestFirst(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-c", "2026-03-03", intp(2600), f64p(3.90))
	rated(t, s, "k-a", "2026-03-01", intp(2431), f64p(3.55))
	rated(t, s, "k-b", "2026-03-02", intp(2498), f64p(3.71))

	got, err := s.Ratings(Filter{})
	if err != nil {
		t.Fatalf("Ratings: %v", err)
	}
	if len(got.Points) != 3 {
		t.Fatalf("points = %d, want 3", len(got.Points))
	}
	for i, want := range []int{2431, 2498, 2600} {
		if got.Points[i].IRating == nil || *got.Points[i].IRating != want {
			t.Errorf("point %d iRating = %s, want %d", i, showI(got.Points[i].IRating), want)
		}
	}
	if got.IRating == nil || *got.IRating != 2600 {
		t.Errorf("headline iRating = %s, want the newest, 2600", showI(got.IRating))
	}
	if got.IRatingDelta == nil || *got.IRatingDelta != 169 {
		t.Errorf("iRating delta = %s, want 2600-2431 = 169", showI(got.IRatingDelta))
	}
	if got.PeakIRating == nil || *got.PeakIRating != 2600 {
		t.Errorf("peak = %s, want 2600", showI(got.PeakIRating))
	}
	if got.UserID == nil || *got.UserID != 271828 {
		t.Errorf("UserID = %s, want 271828", showI(got.UserID))
	}
}

// A losing streak reports a negative delta rather than an absolute movement.
func TestRatingsDeltaIsSignedNotMagnitude(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-a", "2026-03-01", intp(2600), f64p(3.90))
	rated(t, s, "k-b", "2026-03-02", intp(2431), f64p(2.80))

	got, err := s.Ratings(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.IRatingDelta == nil || *got.IRatingDelta != -169 {
		t.Errorf("iRating delta = %s, want -169", showI(got.IRatingDelta))
	}
	if got.SafetyRatingDelta == nil || *got.SafetyRatingDelta > -1.09 {
		t.Errorf("SR delta = %s, want about -1.10", showF(got.SafetyRatingDelta))
	}
	// The peak is the best reading in range, not the latest one.
	if got.PeakIRating == nil || *got.PeakIRating != 2600 {
		t.Errorf("peak = %s, want 2600 — the peak must survive a decline", showI(got.PeakIRating))
	}
}

// One observation reports no delta, because there is nothing to compare it to.
//
// A zero delta would read as "no movement this range", which is a different claim.
func TestRatingsSingleObservationHasNoDelta(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-only", "2026-03-01", intp(2431), f64p(3.55))

	got, err := s.Ratings(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.IRatingDelta != nil {
		t.Errorf("delta = %s from a single observation, want absent", showI(got.IRatingDelta))
	}
	if got.SafetyRatingDelta != nil {
		t.Errorf("SR delta = %s from a single observation, want absent", showF(got.SafetyRatingDelta))
	}
	if got.IRating == nil || *got.IRating != 2431 {
		t.Errorf("iRating = %s, want 2431", showI(got.IRating))
	}
}

// Two readings of the same value are two observations, so the delta is zero and
// present — not absent.
func TestRatingsUnchangedRatingReportsZeroDelta(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-a", "2026-03-01", intp(2431), nil)
	rated(t, s, "k-b", "2026-03-02", intp(2431), nil)

	got, err := s.Ratings(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.IRatingDelta == nil {
		t.Fatal("delta is absent after two equal readings; unchanged is not unknown")
	}
	if *got.IRatingDelta != 0 {
		t.Errorf("delta = %d, want 0", *got.IRatingDelta)
	}
}

// The filter bounds the range, so the delta is computed over what is displayed.
//
// This is the requirement that the accumulated figures honour the filter: a range
// starting mid-season has a different first point, and therefore a different delta,
// than the whole history.
func TestRatingsHonoursTheFilter(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-a", "2026-03-01", intp(2000), nil)
	rated(t, s, "k-b", "2026-03-05", intp(2431), nil)
	rated(t, s, "k-c", "2026-03-06", intp(2498), nil)

	got, err := s.Ratings(Filter{From: "2026-03-05", To: "2026-03-07"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 2 {
		t.Fatalf("points = %d, want 2 — the filter did not bound the range", len(got.Points))
	}
	if got.IRatingDelta == nil || *got.IRatingDelta != 67 {
		t.Errorf("delta = %s, want 2498-2431 = 67; the out-of-range 2000 was used as the baseline",
			showI(got.IRatingDelta))
	}
	if got.PeakIRating == nil || *got.PeakIRating != 2498 {
		t.Errorf("peak = %s, want 2498 from within the range", showI(got.PeakIRating))
	}
}

// A session with no ratings contributes no point, rather than a gap in the line.
func TestRatingsSkipsUnratedSessions(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-a", "2026-03-01", intp(2431), f64p(3.55))
	if _, err := s.UpsertSession(&Session{
		UUID: "u-bare", SessionKey: "k-bare", SessionType: "Practice",
		StartedAt: "2026-03-02T18:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Ratings(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 1 {
		t.Errorf("points = %d, want 1; a session with no rating was emitted", len(got.Points))
	}
}

// An empty database reports no ratings without failing.
//
// This is the state of a fresh install and of every database recorded before the
// identity migration, so it is the common case rather than an edge one.
func TestRatingsOnAnEmptyDatabase(t *testing.T) {
	got, err := openTemp(t).Ratings(Filter{})
	if err != nil {
		t.Fatalf("Ratings on an empty database: %v", err)
	}
	if len(got.Points) != 0 {
		t.Errorf("points = %d, want none", len(got.Points))
	}
	if got.UserID != nil || got.IRating != nil || got.SafetyRating != nil {
		t.Error("an empty database reported an identity or a rating")
	}
}

// The Safety Rating carries its licence string, so the interface can show "A 3.55"
// rather than a bare number with no class.
func TestRatingsCarriesLicenceString(t *testing.T) {
	s := openTemp(t)
	rated(t, s, "k-a", "2026-03-01", intp(2431), f64p(3.55))
	got, err := s.Ratings(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.LicString == nil || *got.LicString != "A 3.55" {
		t.Errorf("LicString = %v, want \"A 3.55\"", got.LicString)
	}
}
