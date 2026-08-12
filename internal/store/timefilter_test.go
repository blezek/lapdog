package store

import (
	"fmt"
	"sort"
	"testing"
	"time"
)

// seedAt inserts a bare session whose only meaningful field is when it started.
func seedAt(t *testing.T, s *Store, key, startedAtUTC string) {
	t.Helper()
	rec := minimalSession(key)
	rec.StartedAt = startedAtUTC
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatalf("seed %s: %v", key, err)
	}
}

// filteredKeys returns the session keys ListSessions yields for a filter.
func filteredKeys(t *testing.T, s *Store, f Filter) []string {
	t.Helper()
	f.Limit = 0
	rows, _, err := s.ListSessions(f)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.SessionKey
	}
	sort.Strings(keys)
	return keys
}

// TestHourFilterMatchesLocalHour seeds one session per hour of a UTC day and
// asserts the hour filter keeps exactly those whose *local* start hour falls in
// range. Computing the expectation with Go's own time zone rather than hard-coding
// it is what keeps the test honest in whatever zone it runs, and the 24-hour spread
// guarantees the filter both admits and rejects sessions in every zone — so a
// predicate that let everything through, or matched on the UTC hour instead of the
// local one, would fail here.
func TestHourFilterMatchesLocalHour(t *testing.T) {
	s := openTemp(t)

	base := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC) // a Tuesday
	var want []string
	for h := 0; h < 24; h++ {
		inst := base.Add(time.Duration(h) * time.Hour)
		key := fmt.Sprintf("hour/%02d", h)
		seedAt(t, s, key, inst.Format(time.RFC3339))
		local := inst.In(time.Local).Hour()
		if local >= 18 && local <= 23 {
			want = append(want, key)
		}
	}
	sort.Strings(want)

	from, to := 18, 23
	got := filteredKeys(t, s, Filter{HourFrom: &from, HourTo: &to})

	if len(want) == 0 || len(want) == 24 {
		t.Fatalf("expectation is trivial (%d of 24) — the test could not fail", len(want))
	}
	if !equalStrings(got, want) {
		t.Errorf("hour filter 18..23\n got  %v\n want %v", got, want)
	}
}

// TestWeekdayFilterMatchesLocalDay seeds fourteen consecutive days so every weekday
// appears twice, then asserts a single-weekday filter keeps exactly the sessions
// whose local weekday matches. As with the hour test the expectation is derived from
// Go's time zone, so it stays correct in any zone and still narrows the set.
func TestWeekdayFilterMatchesLocalDay(t *testing.T) {
	s := openTemp(t)

	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) // a Sunday, at noon
	byWeekday := map[int][]string{}
	total := 0
	for d := 0; d < 14; d++ {
		inst := base.AddDate(0, 0, d)
		key := fmt.Sprintf("day/%02d", d)
		seedAt(t, s, key, inst.Format(time.RFC3339))
		wd := int(inst.In(time.Local).Weekday())
		byWeekday[wd] = append(byWeekday[wd], key)
		total++
	}

	// Saturday (6) in the strftime numbering.
	want := append([]string(nil), byWeekday[6]...)
	sort.Strings(want)

	got := filteredKeys(t, s, Filter{Weekdays: []int{6}})

	if len(want) == 0 || len(want) == total {
		t.Fatalf("expectation is trivial (%d of %d) — the test could not fail", len(want), total)
	}
	if !equalStrings(got, want) {
		t.Errorf("weekday filter {Saturday}\n got  %v\n want %v", got, want)
	}
}

func TestMultipleCarsAndTracksMatchWithinEachDimension(t *testing.T) {
	s := openTemp(t)
	rows := []struct {
		key            string
		carID, trackID int
	}{
		{"wanted/a", 10, 100},
		{"wanted/b", 20, 200},
		{"wrong/car", 30, 100},
		{"wrong/track", 10, 300},
	}
	for _, row := range rows {
		rec := minimalSession(row.key)
		rec.CarID = intp(row.carID)
		rec.TrackID = intp(row.trackID)
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatal(err)
		}
	}

	got := filteredKeys(t, s, Filter{CarIDs: []int{10, 20}, TrackIDs: []int{100, 200}})
	want := []string{"wanted/a", "wanted/b"}
	if !equalStrings(got, want) {
		t.Errorf("multi-entity filter\n got  %v\n want %v", got, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
