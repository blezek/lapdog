package sessionyaml

import "testing"

// The Safety Rating comes from the string the driver actually sees, not only from the
// scaled integer beside it.
func TestSafetyRatingPrefersTheDisplayedString(t *testing.T) {
	cases := []struct {
		name string
		d    Driver
		want float64
		ok   bool
	}{
		{"licence string", Driver{LicString: "A 3.55", LicSubLevel: 355}, 3.55, true},
		{"rookie", Driver{LicString: "R 2.50", LicSubLevel: 250}, 2.50, true},
		{"class D with a high rating", Driver{LicString: "D 4.99", LicSubLevel: 499}, 4.99, true},
		// The two sources disagreeing is the case that decides which is authoritative:
		// the string is what the simulator shows, so it wins.
		{"sources disagree", Driver{LicString: "B 2.00", LicSubLevel: 999}, 2.00, true},
		// Falling back matters for a document that omits the string.
		{"sublevel only", Driver{LicSubLevel: 314}, 3.14, true},
		{"unparseable string falls back", Driver{LicString: "Pro", LicSubLevel: 400}, 4.00, true},
		{"nothing at all", Driver{}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := SafetyRating(c.d)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && (got < c.want-0.001 || got > c.want+0.001) {
				t.Errorf("SafetyRating = %v, want %v", got, c.want)
			}
		})
	}
}

// The customer id prefers the field that states it directly.
func TestMyIdentityPrefersTheDirectUserID(t *testing.T) {
	i := &Info{}
	i.WeekendInfo.SubSessionID = 12345678 // online, so the ratings are real
	i.DriverInfo.DriverCarIdx = 2
	i.DriverInfo.DriverUserID = 271828
	i.DriverInfo.Drivers = []Driver{
		{CarIdx: 1, UserID: 999999, IRating: 1000},
		{CarIdx: 2, UserID: 314159, IRating: 2345, LicString: "A 3.55", LicLevel: 13, LicSubLevel: 355},
	}

	id := i.MyIdentity()
	if id.UserID == nil || *id.UserID != 271828 {
		t.Errorf("UserID = %v, want the DriverUserID field, not the array entry", id.UserID)
	}
	// The ratings must come from the local driver's row, not another car's.
	if id.IRating == nil || *id.IRating != 2345 {
		t.Errorf("IRating = %v, want 2345 from the driver at CarIdx 2", id.IRating)
	}
	if id.SafetyRating == nil || *id.SafetyRating != 3.55 {
		t.Errorf("SafetyRating = %v, want 3.55", id.SafetyRating)
	}
}

// Without the direct field, the array entry is used.
func TestMyIdentityFallsBackToTheDriversArray(t *testing.T) {
	i := &Info{}
	i.DriverInfo.DriverCarIdx = 0
	i.DriverInfo.Drivers = []Driver{{CarIdx: 0, UserID: 424242, IRating: 1500}}

	id := i.MyIdentity()
	if id.UserID == nil || *id.UserID != 424242 {
		t.Errorf("UserID = %v, want 424242 from the drivers array", id.UserID)
	}
}

// An iRating of zero is a real licence state, so it must not be reported as absent —
// but a document with no driver entry at all has no rating to report.
func TestMyIdentityDistinguishesZeroFromAbsent(t *testing.T) {
	withDriver := &Info{}
	withDriver.WeekendInfo.SubSessionID = 12345678
	withDriver.DriverInfo.DriverCarIdx = 0
	withDriver.DriverInfo.Drivers = []Driver{{CarIdx: 0, UserID: 1, IRating: 0}}
	if id := withDriver.MyIdentity(); id.IRating == nil || *id.IRating != 0 {
		t.Errorf("IRating = %v, want a present zero for an unrated licence", id.IRating)
	}

	noDriver := &Info{}
	noDriver.WeekendInfo.SubSessionID = 12345678
	noDriver.DriverInfo.DriverCarIdx = 7
	if id := noDriver.MyIdentity(); id.IRating != nil {
		t.Errorf("IRating = %v, want nil when no driver entry matches", *id.IRating)
	}
	if id := (*Info)(nil).MyIdentity(); id.UserID != nil {
		t.Error("a nil Info produced an identity")
	}
}

// An offline session yields the identity but no ratings.
//
// iRacing does not report real ratings offline. A capture from an AI practice session
// gave an established account IRating 1, LicLevel 1, LicSubLevel 1 and LicString
// "R 0.01", and storing those produced a collapse to nothing on the progression chart.
// Absent is the honest answer; the customer id is still correct and still kept.
//
// The id here is synth.DriverUserID, the generator's fictional customer. A test has no
// business carrying a real account number, and this one briefly did.
func TestMyIdentityDropsRatingsFromAnOfflineSession(t *testing.T) {
	i := &Info{}
	i.WeekendInfo.SubSessionID = 0 // the offline marker
	i.DriverInfo.DriverCarIdx = 0
	i.DriverInfo.DriverUserID = 271828
	i.DriverInfo.Drivers = []Driver{
		{CarIdx: 0, UserID: 271828, IRating: 1, LicString: "R 0.01", LicLevel: 1, LicSubLevel: 1},
	}

	id := i.MyIdentity()
	if id.UserID == nil || *id.UserID != 271828 {
		t.Errorf("UserID = %v, want 271828 — the account is known even offline", id.UserID)
	}
	if id.IRating != nil {
		t.Errorf("IRating = %d from an offline session, want absent", *id.IRating)
	}
	if id.LicString != nil {
		t.Errorf("LicString = %q from an offline session, want absent", *id.LicString)
	}
	if id.LicLevel != nil || id.LicSubLevel != nil {
		t.Error("licence level or sublevel recorded from an offline session")
	}
	if id.SafetyRating != nil {
		t.Errorf("SafetyRating = %v from an offline session, want absent", *id.SafetyRating)
	}
}

// A hosted or league session is online but not official, and does report real ratings —
// so the gate must not be Official.
func TestMyIdentityKeepsRatingsFromAnUnofficialOnlineSession(t *testing.T) {
	i := &Info{}
	i.WeekendInfo.SubSessionID = 87654321
	i.WeekendInfo.Official = 0 // hosted or league
	i.DriverInfo.DriverCarIdx = 0
	i.DriverInfo.Drivers = []Driver{
		{CarIdx: 0, UserID: 271828, IRating: 2431, LicString: "A 3.55", LicLevel: 13, LicSubLevel: 355},
	}

	id := i.MyIdentity()
	if id.IRating == nil || *id.IRating != 2431 {
		t.Errorf("IRating = %v, want 2431; a hosted session is online and rated", id.IRating)
	}
	if id.SafetyRating == nil || *id.SafetyRating != 3.55 {
		t.Errorf("SafetyRating = %v, want 3.55", id.SafetyRating)
	}
}
