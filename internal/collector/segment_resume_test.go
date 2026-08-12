package collector

import (
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/store"
)

func TestSegmentResumeCarriesAccumulatedFacts(t *testing.T) {
	best := 91.5
	start := 4
	existing := &store.Session{
		ConnectedSeconds: 120,
		InCarSeconds:     110,
		DrivingSeconds:   100,
		LapsCompleted:    3,
		Incidents:        2,
		BestLapTimeS:     &best,
		StartingPosition: &start,
	}
	seg := NewSegment(nil, 1, time.Now(), time.Second)

	seg.Resume(existing)

	if seg.Acct.Connected != 120 || seg.Acct.InCar != 110 || seg.Acct.Driving != 100 {
		t.Errorf("accounting totals not resumed: %+v", seg.Acct)
	}
	if seg.lapsCompleted != 3 || seg.incidents != 2 {
		t.Errorf("session totals not resumed: laps %d incidents %d", seg.lapsCompleted, seg.incidents)
	}
	if seg.bestLapTimeS == nil || *seg.bestLapTimeS != best {
		t.Errorf("best lap not resumed: %v", seg.bestLapTimeS)
	}
	if seg.startingPosition == nil || *seg.startingPosition != start {
		t.Errorf("starting position not resumed: %v", seg.startingPosition)
	}
}
