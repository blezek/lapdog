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
