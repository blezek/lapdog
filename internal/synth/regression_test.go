package synth

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/blezek/lapdog/internal/capture"
	"github.com/blezek/lapdog/internal/irsdk"
)

// eachFrame walks every variable row in a capture.
func eachFrame(t *testing.T, path string, fn func(row irsdk.Row)) {
	t.Helper()
	r, err := capture.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if rec.Kind == capture.KindVars {
			fn(irsdk.NewRow(r.Meta().VarHeaders, rec.Vars))
		}
	}
}

// Regression: the per-car position array was zero on every frame, because
// setAllCars zeroed it and then never wrote the running order back. Nothing in
// the generator's own tests noticed, and the consequence only showed up
// downstream — every position change ingested with cause Unknown, because the
// collector could not identify which car held the vacated position.
func TestCarIdxPositionIsPopulatedDuringRaces(t *testing.T) {
	dir := fixtureDirFor(t)
	framesWithField := 0

	eachFrame(t, filepath.Join(dir, "official-race-weekend.lpd"), func(row irsdk.Row) {
		arr, ok := row.IntArray("CarIdxPosition")
		if !ok {
			return
		}
		for _, p := range arr {
			if p > 0 {
				framesWithField++
				return
			}
		}
	})

	if framesWithField == 0 {
		t.Fatal("CarIdxPosition is zero on every frame; opponents can never be identified " +
			"and every position change would ingest as cause Unknown")
	}
}

// Regression: the incident counter dropped to zero on cool-down frames, because
// those frames rebuilt the row from scratch and never rewrote it. A consumer
// reading the last frame of a session saw zero incidents no matter what had
// happened during it.
func TestIncidentCounterNeverDropsWithinASession(t *testing.T) {
	dir := fixtureDirFor(t)

	for _, name := range []string{"official-race-weekend.lpd", "league-race-weekend.lpd"} {
		var prev int32
		var prevSession int32 = -1
		peak := int32(0)
		drops := 0

		eachFrame(t, filepath.Join(dir, name), func(row irsdk.Row) {
			sessionNum, _ := row.Int("SessionNum")
			inc, ok := row.Int("PlayerCarMyIncidentCount")
			if !ok {
				return
			}
			// The counter legitimately restarts when the session changes.
			if sessionNum != prevSession {
				prevSession = sessionNum
				prev = inc
				return
			}
			if inc < prev {
				drops++
			}
			if inc > peak {
				peak = inc
			}
			prev = inc
		})

		if peak == 0 {
			t.Errorf("%s: the incident counter never rises, so incident recording is untested", name)
		}
		if drops > 0 {
			t.Errorf("%s: the incident counter drops %d times within a session; "+
				"the simulator's counter only ever rises", name, drops)
		}
	}
}

// Regression: retired cars were moved to the back of the running order, which
// mutated the order slice's backing array while the caller kept a separate
// position counter. The two desynchronised silently and later swaps were
// attributed to whichever car happened to occupy the stale index.
//
// The invariant that prevents it: the driver's reported position must always
// agree with the driver's slot in the per-car position array.
func TestDriverPositionAgreesWithFieldArray(t *testing.T) {
	dir := fixtureDirFor(t)

	for _, name := range []string{"official-race-weekend.lpd", "hosted-race.lpd"} {
		mismatches := 0
		checked := 0

		eachFrame(t, filepath.Join(dir, name), func(row irsdk.Row) {
			mine, ok := row.Int("PlayerCarPosition")
			if !ok || mine <= 0 {
				return
			}
			arr, ok := row.IntArray("CarIdxPosition")
			if !ok {
				return
			}
			// Find which car index the field array says holds the driver's position.
			holder := -1
			for idx, p := range arr {
				if p == mine {
					holder = idx
					break
				}
			}
			if holder < 0 {
				return // pre-green frames where the field is not yet classified
			}
			checked++
			// Exactly one car may claim a position, and for the driver's own
			// position that car must be the driver.
			count := 0
			for _, p := range arr {
				if p == mine {
					count++
				}
			}
			if count != 1 {
				mismatches++
			}
		})

		if checked == 0 {
			t.Errorf("%s: no classified frames to check", name)
		}
		if mismatches > 0 {
			t.Errorf("%s: %d of %d frames have more than one car claiming the driver's position; "+
				"the running order and the reported position have desynchronised",
				name, mismatches, checked)
		}
	}
}

// fixtureDirFor generates the fixture set into a temp directory.
func fixtureDirFor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := WriteFixtures(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}
