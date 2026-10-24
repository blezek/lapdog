package collector

import (
	"testing"
	"time"

	"github.com/blezek/lapdog/internal/classify"
	"github.com/blezek/lapdog/internal/irsdk"
	"github.com/blezek/lapdog/internal/sessionyaml"
	"github.com/blezek/lapdog/internal/source"
	"github.com/blezek/lapdog/internal/store"
	"github.com/blezek/lapdog/internal/synth"
	"gopkg.in/yaml.v3"
)

func spectatorInfo() *sessionyaml.Info {
	yes, no := 1, 0
	return &sessionyaml.Info{
		WeekendInfo: sessionyaml.Weekend{SubSessionID: 123, Official: 1, SimMode: "full", TrackID: 7},
		DriverInfo: sessionyaml.Drivers{DriverCarIdx: 7, Drivers: []sessionyaml.Driver{
			{CarIdx: 0, IsSpectator: &yes, CarIsAI: &no},
			{CarIdx: 7, UserID: 42, CarID: 9, IsSpectator: &yes, CarIsAI: &no, IRating: 1500, LicString: "D 2.50"},
		}},
		SessionInfo:        sessionyaml.Sessions{Sessions: []sessionyaml.Session{{SessionNum: 0, SessionType: "Race", ResultsPositions: []sessionyaml.ResultPosition{{CarIdx: 7, Position: 1, ClassPosition: 1, Incidents: 8}}}}},
		QualifyResultsInfo: sessionyaml.QualifyResults{Results: []sessionyaml.QualifyResult{{CarIdx: 7, Position: 1, FastestTime: 80}}},
	}
}

func TestSpectatorClassificationUsesLocalExplicitFlag(t *testing.T) {
	info := spectatorInfo()
	for _, tc := range []struct {
		name string
		flag *int
		want classify.SessionType
	}{
		{"spectator", newInt(1), classify.TypeSpectator},
		{"competitor with spectator opponent", newInt(0), classify.TypeRace},
		{"absent flag", nil, classify.TypeRace},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info.DriverInfo.Drivers[1].IsSpectator = tc.flag
			raw, err := yaml.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := sessionyaml.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			got := classify.Classify(parsed, 0)
			if got.SessionType != tc.want || got.EventContext != classify.ContextOfficialRace || got.RawSessionType != "Race" {
				t.Fatalf("classification = %+v", got)
			}
			if tc.flag == nil && parsed.DriverInfo.Drivers[1].IsSpectator != nil {
				t.Fatal("absent flag became zero")
			}
		})
	}
	info.DriverInfo.DriverCarIdx = 99
	if info.IsSpectator() {
		t.Fatal("another driver's spectator flag was used")
	}
	for _, context := range []classify.EventContext{classify.ContextOfficialRace, classify.ContextOffline, classify.ContextLeague} {
		if got := classify.Label(classify.TypeSpectator, context); got != "Spectating / ghost driving" {
			t.Fatalf("label = %q", got)
		}
	}
}

func newInt(n int) *int { return &n }

func spectatorFrame(t *testing.T, info *sessionyaml.Info, at float64, onTrack bool, idx int32, surface irsdk.TrkLoc, lap int32, replay bool) source.Frame {
	t.Helper()
	headers, size := synth.Layout()
	rb := irsdk.NewRowBuilder(headers, size)
	for name, value := range map[string]bool{"IsOnTrack": onTrack, "IsOnTrackCar": true, "IsReplayPlaying": replay} {
		if err := rb.SetBool(name, value); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]int32{"PlayerCarIdx": idx, "SessionNum": 0, "SessionState": 4, "Lap": lap, "PlayerCarPosition": 1} {
		if err := rb.SetInt(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := rb.SetIntAt("CarIdxTrackSurface", 7, int32(surface)); err != nil {
		t.Fatal(err)
	}
	if err := rb.SetFloat("LapLastLapTime", 80); err != nil {
		t.Fatal(err)
	}
	raw, err := yaml.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	return source.Frame{T: at, Row: irsdk.NewRow(headers, rb.Bytes()), SessionYAML: raw, YAMLChanged: true}
}

func TestSpectatorWatchingGhostDrivingAndReplay(t *testing.T) {
	c, src := collectorForFixture(t, "public-practice.lpd")
	defer src.Close()
	info := spectatorInfo()
	handle := func(at float64, onTrack bool, idx int32, surface irsdk.TrkLoc, lap int32, replay bool) {
		t.Helper()
		if err := c.handle(spectatorFrame(t, info, at, onTrack, idx, surface, lap, replay)); err != nil {
			t.Fatal(err)
		}
	}
	// Watching works without a player index or race/lap telemetry.
	frame := spectatorFrame(t, info, 0, false, -1, irsdk.OnTrack, 10, false)
	var minimal []irsdk.VarHeader
	for _, h := range src.Meta().VarHeaders {
		switch h.Name {
		case "SessionNum", "IsOnTrack", "IsOnTrackCar", "IsReplayPlaying":
			minimal = append(minimal, h)
		}
	}
	frame.Row = irsdk.NewRow(minimal, frame.Row.Raw())
	if err := c.handle(frame); err != nil {
		t.Fatal(err)
	}
	if c.seg == nil {
		t.Fatal("spectating required race telemetry")
	}
	handle(1, false, -1, irsdk.OnTrack, 11, false)
	if live := c.Live().Frame; live == nil || live.Reason != "spectating" || live.Speed != nil || live.Lap != nil || live.InCar || live.Driving {
		t.Fatalf("watching frame = %+v", live)
	}
	handle(2, true, 7, irsdk.InPitStall, 1, false)
	handle(3, true, 7, irsdk.OnTrack, 1, false)
	handle(4, true, 7, irsdk.OnTrack, 2, false)
	handle(5, false, 7, irsdk.OnTrack, 20, false)
	handle(6, true, 7, irsdk.OnTrack, 21, true)
	handle(7, true, 7, irsdk.OnTrack, 22, false)
	handle(8, true, 7, irsdk.OnTrack, 23, false)
	if c.seg.Acct.Connected != 6 || c.seg.Acct.InCar != 4 || c.seg.Acct.Driving != 3 {
		t.Fatalf("counters = %+v", c.seg.Acct)
	}
	if err := c.flush(); err != nil {
		t.Fatal(err)
	}
	rec, err := c.st.SessionByKey(c.seg.Key)
	if err != nil {
		t.Fatal(err)
	}
	if rec.SessionType != "Spectator" || rec.StartingPosition != nil || rec.FinishPosition != nil || rec.QualifyPosition != nil || rec.DriverIRating != nil || rec.DriverSafetyRating != nil || rec.DriverUserID == nil {
		t.Fatalf("spectator persisted competitive facts: %+v", rec)
	}
	laps, err := c.st.LapsForSession(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(laps) != 2 || laps[0].LapNumber != 1 || laps[1].LapNumber != 22 {
		t.Fatalf("laps include watched/replay crossings: %+v", laps)
	}
	_, races, err := c.st.ListSessions(store.Filter{SessionType: []string{"Race"}})
	if err != nil || races != 0 {
		t.Fatalf("race list = %d, %v", races, err)
	}
	stats, err := c.st.EntityStats(store.Filter{}, "car", 9)
	if err != nil || stats.Races != 0 || stats.Wins != 0 {
		t.Fatalf("race statistics = %+v, %v", stats, err)
	}
	events, err := c.st.PositionEventsForSession(rec.ID)
	if err != nil || len(events) != 0 {
		t.Fatalf("spectator position events = %+v, %v", events, err)
	}
}

func TestSpectatorModeSwitchAndReconnectKeepSeparateTotals(t *testing.T) {
	c, src := collectorForFixture(t, "public-practice.lpd")
	defer src.Close()
	info := spectatorInfo()
	handle := func(at float64) {
		t.Helper()
		c.clock.(*FakeClock).Advance(time.Second)
		if err := c.handle(spectatorFrame(t, info, at, true, 7, irsdk.OnTrack, 1, false)); err != nil {
			t.Fatal(err)
		}
	}
	handle(0)
	handle(1)
	spectatorKey := c.seg.Key
	info.DriverInfo.Drivers[1].IsSpectator = newInt(0)
	handle(2)
	handle(3)
	if c.seg.Key == spectatorKey || !c.seg.IsRace() || c.seg.Acct.Driving != 1 {
		t.Fatalf("competitor inherited spectator state: %+v", c.seg)
	}
	competitorKey := c.seg.Key
	c.disconnect()
	info.DriverInfo.Drivers[1].IsSpectator = newInt(1)
	handle(4)
	handle(5)
	if c.seg.Key != spectatorKey || c.seg.Acct.Driving != 2 || c.seg.IsRace() {
		t.Fatalf("spectator reconnect = %+v", c.seg)
	}
	c.disconnect()
	info.DriverInfo.Drivers[1].IsSpectator = newInt(0)
	handle(6)
	handle(7)
	if c.seg.Key != competitorKey || c.seg.Acct.Driving != 2 {
		t.Fatalf("competitor reconnect = %+v", c.seg)
	}
}

func TestSpectatorSampleRequiresLocalDrivingEvidence(t *testing.T) {
	info := spectatorInfo()
	for _, tc := range []struct {
		name           string
		onTrack, inCar bool
		idx            int32
		omit           string
		ok, driving    bool
	}{
		{"watching another car", false, true, 7, "", true, false},
		{"no local car", true, false, 7, "", true, false},
		{"ghost on track", true, true, 7, "", true, true},
		{"unknown player", true, true, -1, "", false, false},
		{"missing on-track flag", true, true, 7, "IsOnTrack", false, false},
		{"missing car flag", true, true, 7, "IsOnTrackCar", false, false},
		{"missing replay flag", true, true, 7, "IsReplayPlaying", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame := spectatorFrame(t, info, 0, tc.onTrack, tc.idx, irsdk.OnTrack, 1, false)
			headers, size := synth.Layout()
			rb := irsdk.NewRowBuilder(headers, size)
			copy(rb.Bytes(), frame.Row.Raw())
			if err := rb.SetBool("IsOnTrackCar", tc.inCar); err != nil {
				t.Fatal(err)
			}
			var kept []irsdk.VarHeader
			for _, h := range headers {
				if h.Name != tc.omit {
					kept = append(kept, h)
				}
			}
			got, ok := SpectatorSampleFrom(irsdk.NewRow(kept, rb.Bytes()))
			if ok != tc.ok || got.Driving != tc.driving {
				t.Fatalf("sample = %+v, %v", got, ok)
			}
		})
	}
}
