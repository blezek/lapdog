package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGarage61WeeklyReminderPersistsAndChecksDates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refresh.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if Garage61CombinationDue(now.Add(-6*24*time.Hour).Format(time.RFC3339), now) {
		t.Fatal("six-day-old scenarios should not be due")
	}
	if !Garage61CombinationDue(now.Add(-7*24*time.Hour).Format(time.RFC3339), now) {
		t.Fatal("seven-day-old scenarios should be due")
	}
	if !Garage61CombinationDue("invalid", now) {
		t.Fatal("invalid preparation date should not suppress reminder")
	}
	until := now.Add(24 * time.Hour)
	if err := s.SetGarage61SnoozeUntil(&until); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	saved, err := s.Garage61SnoozeUntil()
	if err != nil || saved == nil || !saved.Equal(until) {
		t.Fatalf("snooze after restart=%v, %v", saved, err)
	}
	if err := s.SetGarage61SnoozeUntil(nil); err != nil {
		t.Fatal(err)
	}
	saved, err = s.Garage61SnoozeUntil()
	if err != nil || saved != nil {
		t.Fatalf("cleared snooze=%v, %v", saved, err)
	}
}
func TestMyDrivenCombinationsExcludesQualifyingAndKeepsPlatformIDs(t *testing.T) {
	s := openTemp(t)
	for _, row := range []struct {
		key, kind  string
		car, track int
		seconds    int
	}{{"race", "Race", 67, 523, 3600}, {"practice", "Practice", 67, 523, 1800}, {"quali", "Qualify", 67, 524, 900}} {
		_, err := s.writer.Exec(`INSERT INTO sessions (uuid,session_key,session_num,session_type,event_context,track_id,track_name,track_config,car_id,car_name,started_at,driving_seconds,classify_source_json,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.key, row.key, 0, row.kind, "Test", row.track, "Spa", "Grand Prix", row.car, "Mazda", "2026-10-06T12:00:00Z", row.seconds, "{}", "2026-10-06T12:00:00Z", "2026-10-06T12:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
	}
	combos, err := s.MyDrivenCombinations()
	if err != nil {
		t.Fatal(err)
	}
	if len(combos) != 1 || combos[0].CarPlatformID != 67 || combos[0].TrackPlatformID != 523 || combos[0].DrivingHours != 1.5 {
		t.Fatalf("combos=%+v", combos)
	}
}
