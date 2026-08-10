package collector

import (
	"encoding/json"
	"os"
	"testing"
)

// realBuild is the variable layout a real iRacing build published.
type realBuild struct {
	Recorded string   `json:"recorded"`
	TickRate int32    `json:"tickRate"`
	NumVars  int32    `json:"numVars"`
	BufLen   int32    `json:"bufLen"`
	VarNames []string `json:"varNames"`
}

func loadRealBuild(t *testing.T) realBuild {
	t.Helper()
	b, err := os.ReadFile("testdata/real-build-vars.json")
	if err != nil {
		t.Fatalf("read the real-build fixture: %v", err)
	}
	var rb realBuild
	if err := json.Unmarshal(b, &rb); err != nil {
		t.Fatalf("decode the real-build fixture: %v", err)
	}
	if len(rb.VarNames) == 0 {
		t.Fatal("the fixture lists no variables")
	}
	return rb
}

// Every variable the collector requires is published by a real build.
//
// This is the test the required lists never had. Both were transcribed from
// documentation/telemetry_11_23_15.pdf — 2015 — and a name that a current build had
// renamed or dropped would not fail anywhere: the collector would refuse the session
// at runtime, on the user's machine, with the reason only in a log.
//
// It cannot be written against internal/synth, which is the reason this fixture is
// committed at all. layout.go declares whatever LapDog asks for, so asserting the
// required names against the generator proves only that the generator agrees with
// itself. Nothing but a real capture can settle whether iRacing still publishes them.
func TestRealBuildPublishesEveryRequiredVar(t *testing.T) {
	rb := loadRealBuild(t)
	published := make(map[string]bool, len(rb.VarNames))
	for _, n := range rb.VarNames {
		published[n] = true
	}

	for _, group := range []struct {
		name  string
		names []string
	}{
		{"RequiredCoreVars", RequiredCoreVars},
		{"RequiredRaceVars", RequiredRaceVars},
	} {
		for _, want := range group.names {
			if !published[want] {
				t.Errorf("%s names %q, which the %s build does not publish; "+
					"a session needing it would be refused at runtime",
					group.name, want, rb.Recorded)
			}
		}
	}
}

// The optional incident variable is present in a current build.
//
// It is optional because it postdates the 2015 documentation, and the collector falls
// back to the session YAML without it. The fallback is correct but coarser — YAML
// updates only when the sim bumps its counter — so it is worth knowing that the live
// variable is in fact available, and worth being told if a later build removes it.
func TestRealBuildPublishesTheIncidentVar(t *testing.T) {
	rb := loadRealBuild(t)
	for _, n := range rb.VarNames {
		if n == OptionalIncidentVar {
			return
		}
	}
	t.Errorf("the %s build does not publish %q; incident counting would silently "+
		"fall back to the session YAML", rb.Recorded, OptionalIncidentVar)
}

// The fixture describes a plausible layout, so a truncated or hand-edited copy is
// caught rather than quietly weakening the assertions above.
func TestRealBuildFixtureIsSelfConsistent(t *testing.T) {
	rb := loadRealBuild(t)
	if int(rb.NumVars) != len(rb.VarNames) {
		t.Errorf("numVars = %d but %d names are listed", rb.NumVars, len(rb.VarNames))
	}
	if rb.TickRate != 60 {
		t.Errorf("tickRate = %d, want 60; iRacing publishes at 60 Hz", rb.TickRate)
	}
	// Every required variable occupies at least one byte, so the buffer cannot be
	// smaller than the number of variables.
	if rb.BufLen < rb.NumVars {
		t.Errorf("bufLen %d is smaller than numVars %d", rb.BufLen, rb.NumVars)
	}
}

// The fixture carries no personal data, which is the condition of it being committed.
//
// A future refresh will be taken from another real capture, and the obvious mistake is
// to paste in the whole header — or the whole record — along with a customer id or a
// driver name. Variable names are CamelCase identifiers and never contain a space, so
// a spaced value is the cheap signal that something else came with them.
func TestRealBuildFixtureCarriesNoPersonalData(t *testing.T) {
	b, err := os.ReadFile("testdata/real-build-vars.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	// Only these keys, so a pasted-in driver array or session block fails here.
	allowed := map[string]bool{
		"_comment": true, "recorded": true, "tickRate": true,
		"numVars": true, "bufLen": true, "varNames": true,
	}
	for k := range raw {
		if !allowed[k] {
			t.Errorf("unexpected key %q in the fixture; only the layout belongs here", k)
		}
	}

	rb := loadRealBuild(t)
	for _, n := range rb.VarNames {
		for _, c := range n {
			if c == ' ' {
				t.Errorf("variable name %q contains a space; iRacing variable names are "+
					"identifiers, so this is probably not a variable name", n)
				break
			}
		}
	}
}
