package collector

import (
	"path/filepath"
	"testing"

	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/synth"
)

// Every recorded session carries the local driver's customer ID and the ratings
// that were in force at the time.
//
// The identity is read from the session YAML's DriverUserID and the matching entry
// in its Drivers list. Picking the wrong entry is the failure mode this guards: the
// list holds every car in the field, so a rule that took the first driver would
// record an opponent's rating as the user's own — plausible-looking and wrong.
func TestIngestRecordsDriverIdentityAndRatings(t *testing.T) {
	st := ingest(t, filepath.Join(fixtureDir(t), "official-race-weekend.lpd"), nil)

	rows, total, err := st.ListSessions(store.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("no sessions recorded")
	}

	for _, got := range rows {
		if got.DriverUserID == nil {
			t.Errorf("session %d has no DriverUserID; identity was not captured", got.SessionNum)
			continue
		}
		if *got.DriverUserID != synth.DriverUserID {
			t.Errorf("session %d DriverUserID = %d, want %d — a driver other than the user was recorded",
				got.SessionNum, *got.DriverUserID, synth.DriverUserID)
		}
		// The fixture's iRating varies by event, so the assertion is that a rating was
		// captured at all rather than a specific number.
		if got.DriverIRating == nil || *got.DriverIRating <= 0 {
			t.Errorf("session %d has no iRating", got.SessionNum)
		}
		if got.DriverLicString == nil || *got.DriverLicString != "A 3.55" {
			t.Errorf("session %d DriverLicString is not the fixture's licence", got.SessionNum)
		}
		if got.DriverLicLevel == nil || *got.DriverLicLevel != 13 {
			t.Errorf("session %d DriverLicLevel was not captured as 13", got.SessionNum)
		}
		if got.DriverSafetyRating == nil || *got.DriverSafetyRating != 3.55 {
			t.Errorf("session %d SafetyRating was not derived as 3.55 from %v",
				got.SessionNum, got.DriverLicString)
		}
	}
}
