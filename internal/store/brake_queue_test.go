package store

import (
	"path/filepath"
	"testing"
)

func TestRecordedSessionsNeverQueueGarageCombinations(t *testing.T) {
	s := openTemp(t)
	rec := minimalSession("recorded-practice")
	rec.SessionType = "Practice"
	car, track := 67, 523
	carName, trackName, config := "Mazda", "Spa", "Grand Prix Pits"
	rec.CarID, rec.TrackID = &car, &track
	rec.CarName, rec.TrackName, rec.TrackConfig = &carName, &trackName, &config
	for i := range 3 {
		if _, err := s.UpsertSession(rec); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first, err := s.ListGarageQueue()
			if err != nil || len(first) != 0 {
				t.Fatalf("first practice save queued without a click: %+v %v", first, err)
			}
		}
	}
	queue, err := s.ListGarageQueue()
	if err != nil || len(queue) != 0 {
		t.Fatalf("practice queued without a click: %+v err=%v", queue, err)
	}
	if _, err := s.writer.Exec(`UPDATE sessions SET session_type='Race' WHERE session_key=?`, rec.SessionKey); err != nil {
		t.Fatal(err)
	}
	queue, err = s.ListGarageQueue()
	if err != nil || len(queue) != 0 {
		t.Fatalf("reclassified race queued without a click: %+v %v", queue, err)
	}
	qualify := minimalSession("queued-qualify")
	qualify.SessionType = "Qualify"
	qualify.CarID, qualify.TrackID = &car, &track
	if _, err := s.UpsertSession(qualify); err != nil {
		t.Fatal(err)
	}
	queue, err = s.ListGarageQueue()
	if err != nil || len(queue) != 0 {
		t.Fatalf("qualifying entered queue: %+v %v", queue, err)
	}
	newTrack := 524
	rec.TrackID = &newTrack
	if _, err := s.UpsertSession(rec); err != nil {
		t.Fatal(err)
	}
	queue, err = s.ListGarageQueue()
	if err != nil || len(queue) != 0 {
		t.Fatalf("changing layout queued without a click: %+v %v", queue, err)
	}
}

func TestMigrationRemovesPreviouslyAutomaticGarageQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "previous-queue.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Writer().Exec(`
INSERT INTO brake_garage61_queue(queue_key,car_name,track_name,source,queued_at)
 VALUES ('p:67:523','Mazda','Spa','history','2026-10-06T12:00:00Z'),
        ('g:101:202','Mazda','Spa','manual','2026-10-06T12:00:00Z');
CREATE TRIGGER brake_garage61_queue_insert AFTER INSERT ON sessions BEGIN SELECT 1; END;
CREATE TRIGGER brake_garage61_queue_update AFTER UPDATE ON sessions BEGIN SELECT 1; END;
UPDATE schema_version SET version=10;`)
	if err != nil {
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
	queue, err := s.ListGarageQueue()
	if err != nil || len(queue) != 1 || queue[0].Source != "manual" {
		t.Fatalf("migration did not retain only requested work: %+v %v", queue, err)
	}
	var triggerCount int
	if err := s.Reader().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='trigger'
 AND name IN ('brake_garage61_queue_insert','brake_garage61_queue_update')`).Scan(&triggerCount); err != nil || triggerCount != 0 {
		t.Fatalf("automatic queue triggers remained: %d %v", triggerCount, err)
	}
}

func TestGarageQueuePersistsAndRecoversInterruptedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.EnqueueGaragePair(67, 523, 101, 202, "Mazda", "Spa Grand Prix", "manual")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNextGarageQueue()
	if err != nil || claimed == nil || claimed.ID != item.ID || claimed.State != "running" {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RecoverGarageQueue(); err != nil {
		t.Fatal(err)
	}
	claimed, err = s.ClaimNextGarageQueue()
	if err != nil || claimed == nil || claimed.ID != item.ID || claimed.AttemptCount != 2 {
		t.Fatalf("recovered claim=%+v err=%v", claimed, err)
	}
	if err := s.FinishGarageQueue(item.ID, "done", ""); err != nil {
		t.Fatal(err)
	}
	queue, err := s.ListGarageQueue()
	if err != nil || len(queue) != 1 || queue[0].State != "done" {
		t.Fatalf("done queue=%+v err=%v", queue, err)
	}
}
