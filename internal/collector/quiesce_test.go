package collector

import "testing"

func TestTryQuiesceIsAtomicWithActiveSegment(t *testing.T) {
	c := &Collector{}
	c.activeMu.Lock()
	c.seg = &Segment{}
	c.activeMu.Unlock()
	if c.TryQuiesce() {
		t.Fatal("active segment permitted quiescing")
	}
	c.activeMu.Lock()
	c.seg = nil
	c.activeMu.Unlock()
	if !c.TryQuiesce() {
		t.Fatal("closed segment did not permit quiescing")
	}
	c.activeMu.Lock()
	got := c.quiesced
	c.activeMu.Unlock()
	if !got {
		t.Fatal("successful quiesce did not block new segments")
	}
	c.ResumeRecording()
	c.activeMu.Lock()
	got = c.quiesced
	c.activeMu.Unlock()
	if got {
		t.Fatal("resume left collector quiesced")
	}
}
