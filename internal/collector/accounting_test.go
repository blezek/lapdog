package collector

import (
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/synth"
)

func TestAccountantKeepsBothPollRatesAtASettingsChange(t *testing.T) {
	a := NewAccountant(time.Second)
	add := func(at float64) { a.Add(Sample{T: at, InCar: true, Driving: true}) }
	add(0)
	add(1)
	a.SetInterval(30 * time.Second)
	add(31)
	a.SetInterval(time.Second)
	add(61) // A wait already in progress may still use the old 30-second rate.
	add(62)
	if a.Connected != 62 || a.InCar != 62 || a.Driving != 62 {
		t.Fatalf("after rate changes: connected=%v in-car=%v driving=%v, want 62 each", a.Connected, a.InCar, a.Driving)
	}
	add(100) // A genuine stall must still be clamped at the new rate.
	if a.Connected != 63 || a.Clamped != 1 {
		t.Fatalf("after stall: connected=%v clamped=%d, want 63 and 1", a.Connected, a.Clamped)
	}
}

func TestAccountantDoesNotCreditReplayExitGap(t *testing.T) {
	a := NewAccountant(time.Second)
	a.Add(Sample{T: 0, InCar: true, Driving: true})
	a.Add(Sample{T: 1, InCar: true, Driving: true, Replay: true})
	a.Add(Sample{T: 2, InCar: true, Driving: true})
	a.Add(Sample{T: 3, InCar: true, Driving: true})
	if a.Connected != 1 || a.InCar != 1 || a.Driving != 1 {
		t.Fatalf("replay boundary: connected=%v in-car=%v driving=%v, want 1 each", a.Connected, a.InCar, a.Driving)
	}
}

// rowWith builds a telemetry row with only the fields the accounting reads.
func rowWith(t *testing.T, inCar, replay bool, loc irsdk.TrkLoc) irsdk.Row {
	t.Helper()
	vh, bufLen := synth.Layout()
	rb := irsdk.NewRowBuilder(vh, bufLen)
	if err := rb.SetBool("IsOnTrackCar", inCar); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetBool("IsReplayPlaying", replay); err != nil {
		t.Fatal(err)
	}
	// Put the player away from slot zero so a fixed or stale index cannot make
	// these tests pass. Real sessions change PlayerCarIdx as rosters populate.
	if err := rb.SetInt("PlayerCarIdx", 7); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetIntAt("CarIdxTrackSurface", 7, int32(loc)); err != nil {
		t.Fatal(err)
	}
	return irsdk.NewRow(vh, rb.Bytes())
}

// The reason mirrors Accountant.Add's precedence, because that is what actually
// decides whether time accrues. Replay outranks everything: Add returns before
// crediting even connected time.
func TestNotDrivingReasonFollowsAccountingPrecedence(t *testing.T) {
	cases := []struct {
		name   string
		inCar  bool
		replay bool
		loc    irsdk.TrkLoc
		want   NotDrivingReason
	}{
		{"driving on track", true, false, irsdk.OnTrack, ReasonNone},
		{"driving off track is still driving", true, false, irsdk.OffTrack, ReasonNone},
		{"approaching pits is still driving", true, false, irsdk.ApproachingPits, ReasonNone},
		{"stationary in the pit box", true, false, irsdk.InPitStall, ReasonPitBox},
		{"not in the world", true, false, irsdk.NotInWorld, ReasonNotOnTrack},
		{"not in the car", false, false, irsdk.OnTrack, ReasonNotInCar},
		// Replay wins even when everything else looks like driving.
		{"replay while apparently driving", true, true, irsdk.OnTrack, ReasonReplay},
		// And replay wins over the more specific location reasons too.
		{"replay in the pit box", true, true, irsdk.InPitStall, ReasonReplay},
		{"replay and not in the car", false, true, irsdk.NotInWorld, ReasonReplay},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := rowWith(t, c.inCar, c.replay, c.loc)
			if got := NotDrivingReasonFrom(row); got != c.want {
				t.Errorf("reason = %q, want %q", got, c.want)
			}
		})
	}
}

// The reason must agree with the boolean it explains: empty exactly when the
// accountant would credit driving time, non-empty exactly when it would not.
// A disagreement here is the class of bug this project spent a week fixing.
func TestNotDrivingReasonAgreesWithSample(t *testing.T) {
	for _, loc := range []irsdk.TrkLoc{
		irsdk.NotInWorld, irsdk.OffTrack, irsdk.InPitStall, irsdk.ApproachingPits, irsdk.OnTrack,
	} {
		for _, inCar := range []bool{true, false} {
			row := rowWith(t, inCar, false, loc)
			s, ok := SampleFrom(row)
			if !ok {
				t.Fatalf("SampleFrom refused a complete row (loc %d, inCar %v)", loc, inCar)
			}
			credited := s.InCar && s.Driving
			reason := NotDrivingReasonFrom(row)
			if credited && reason != ReasonNone {
				t.Errorf("loc %d inCar %v: driving time is credited but reason says %q",
					loc, inCar, reason)
			}
			if !credited && reason == ReasonNone {
				t.Errorf("loc %d inCar %v: driving time is not credited but no reason given",
					loc, inCar)
			}
		}
	}
}

func TestSampleUsesTelemetryPlayerCarIdx(t *testing.T) {
	vh, bufLen := synth.Layout()
	rb := irsdk.NewRowBuilder(vh, bufLen)
	if err := rb.SetBool("IsOnTrackCar", true); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetInt("PlayerCarIdx", 7); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetIntAt("CarIdxTrackSurface", 0, int32(irsdk.InPitStall)); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetIntAt("CarIdxTrackSurface", 7, int32(irsdk.OnTrack)); err != nil {
		t.Fatal(err)
	}

	sample, ok := SampleFrom(irsdk.NewRow(vh, rb.Bytes()))
	if !ok {
		t.Fatal("SampleFrom refused a complete row")
	}
	if !sample.Driving {
		t.Error("Driving = false; collector read stale slot 0 instead of PlayerCarIdx 7")
	}
}

// An incomplete row yields no reason rather than a wrong one. The collector
// refuses such a session anyway, so inventing an explanation would be worse
// than declining to give one.
func TestNotDrivingReasonOnAnIncompleteRow(t *testing.T) {
	vh, bufLen := synth.Layout()
	bare := irsdk.NewRow(vh, make([]byte, bufLen))
	// A zeroed row has IsOnTrackCar false, which is a legitimate reading; the
	// out-of-range index is what makes the surface unavailable.
	if got := NotDrivingReasonFrom(bare); got != ReasonNotInCar {
		t.Errorf("reason = %q for the zero-valued player slot, want not-in-car", got)
	}
}
