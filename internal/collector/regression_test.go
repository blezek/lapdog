package collector

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

// Regression: NoteIncidents assigned whatever it was given, so a frame where the
// live counter was momentarily absent — every garage and cool-down frame, since
// those rebuild the row from scratch — reset the session's incident total to zero.
// Every session ingested with zero incidents.
//
// The counter is monotonic within a session, so a lower reading is a missing read
// rather than incidents being forgiven.
func TestNoteIncidentsIsMonotonic(t *testing.T) {
	g := &Segment{}

	g.NoteIncidents(3)
	if got := g.Incidents(); got != 3 {
		t.Fatalf("Incidents = %d, want 3", got)
	}
	g.NoteIncidents(6)
	if got := g.Incidents(); got != 6 {
		t.Fatalf("Incidents = %d, want 6", got)
	}
	// A garage frame reads zero; the total must survive it.
	g.NoteIncidents(0)
	if got := g.Incidents(); got != 6 {
		t.Errorf("Incidents = %d after a zero reading, want 6 retained", got)
	}
	g.NoteIncidents(2)
	if got := g.Incidents(); got != 6 {
		t.Errorf("Incidents = %d after a lower reading, want 6 retained", got)
	}
	g.NoteIncidents(9)
	if got := g.Incidents(); got != 9 {
		t.Errorf("Incidents = %d, want 9 — a higher reading must still be taken", got)
	}
}

// Ingested sessions must actually carry incidents. Zero everywhere is the exact
// symptom the monotonic guard fixes, and it is invisible unless asserted.
func TestIngestRecordsIncidents(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "league-race-weekend.lpd"), nil)

	totals, err := st.Totals(store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if totals.Incidents == 0 {
		t.Error("no incidents ingested from a full race weekend; " +
			"a garage frame is probably resetting the counter")
	}
	if totals.IncidentsPerHour <= 0 {
		t.Errorf("IncidentsPerHour = %v, want positive", totals.IncidentsPerHour)
	}
}

// Regression: with the per-car position array unpopulated, every position change
// was recorded with cause Unknown and no opponent, so the pass/passed ratio was
// always zero and the attribution logic was never exercised.
func TestIngestAttributesPositionChanges(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)

	rows, _, err := st.ListSessions(store.Filter{SessionType: []string{"Race"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no race session ingested")
	}
	evs, err := st.PositionEventsForSession(rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 {
		t.Fatal("the race produced no position events")
	}

	unknown, attributed := 0, 0
	for _, ev := range evs {
		if ev.Cause == store.CauseUnknown {
			unknown++
			continue
		}
		attributed++
		if ev.OpponentCarIdx == nil {
			t.Errorf("event with cause %s has no opponent car index", ev.Cause)
		}
	}
	if attributed == 0 {
		t.Errorf("all %d position events are cause Unknown; the per-car position array "+
			"is not being read", unknown)
	}
}

// A position cannot be lost to a car that is stopped: you do not fall behind
// someone sitting in the pit lane or parked in the gravel. An earlier generator
// version produced exactly that, which would have made the pass/passed ratio
// quietly wrong in the opposite direction from the original bug.
func TestNoLossIsAttributedToAStoppedCar(t *testing.T) {
	dir := fixtureDir(t)
	for _, name := range []string{
		"official-race-weekend.lpd", "league-race-weekend.lpd", "hosted-race.lpd",
	} {
		st := ingest(t, filepath.Join(dir, name), nil)
		rows, _, err := st.ListSessions(store.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			evs, err := st.PositionEventsForSession(r.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range evs {
				lost := ev.ToPosition > ev.FromPosition
				stopped := ev.Cause == store.CauseOpponentPit || ev.Cause == store.CauseOpponentOffWorld
				if lost && stopped {
					t.Errorf("%s: lost position %d to %d with cause %s — a stopped car cannot take a place",
						name, ev.FromPosition, ev.ToPosition, ev.Cause)
				}
			}
		}
	}
}

// Regression: an offline session's key is "offline/<num>/<started_at>" at
// one-second resolution, which is all live recording needs. Ingesting many
// captures within the same second made two offline sessions collide on that key,
// so the second silently overwrote the first and a segment vanished.
//
// This asserts the key itself distinguishes sessions one second apart, which is
// the property the ingest tool relies on when it staggers start times.
func TestOfflineKeysDistinguishByStartTime(t *testing.T) {
	base := store.SessionKey(0, 0, mustTime("2026-08-04T15:59:26Z"))
	next := store.SessionKey(0, 0, mustTime("2026-08-04T15:59:27Z"))
	if base == next {
		t.Fatalf("offline keys one second apart collide: %q", base)
	}
	// And a subsession-bearing key must not depend on the start time at all, or
	// re-ingesting the same online session would duplicate it.
	a := store.SessionKey(4242, 1, mustTime("2026-08-04T15:59:26Z"))
	b := store.SessionKey(4242, 1, mustTime("2027-01-01T00:00:00Z"))
	if a != b {
		t.Errorf("online keys differ by start time: %q vs %q", a, b)
	}
}

// mustTime parses an RFC3339 timestamp or fails the test.
func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
