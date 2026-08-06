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
	withDriver.DriverInfo.DriverCarIdx = 0
	withDriver.DriverInfo.Drivers = []Driver{{CarIdx: 0, UserID: 1, IRating: 0}}
	if id := withDriver.MyIdentity(); id.IRating == nil || *id.IRating != 0 {
		t.Errorf("IRating = %v, want a present zero for an unrated licence", id.IRating)
	}

	noDriver := &Info{}
	noDriver.DriverInfo.DriverCarIdx = 7
	if id := noDriver.MyIdentity(); id.IRating != nil {
		t.Errorf("IRating = %v, want nil when no driver entry matches", *id.IRating)
	}
	if id := (*Info)(nil).MyIdentity(); id.UserID != nil {
		t.Error("a nil Info produced an identity")
	}
}
