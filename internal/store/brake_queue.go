package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// GarageQueueItem is one persistent car/layout preparation request. Platform
// IDs identify iRacing history; Garage61 IDs are filled when the catalog maps it.
type GarageQueueItem struct {
	ID              int64  `json:"id"`
	CarPlatformID   int    `json:"carPlatformId"`
	TrackPlatformID int    `json:"trackPlatformId"`
	GarageCarID     int    `json:"garageCarId"`
	GarageTrackID   int    `json:"garageTrackId"`
	CarName         string `json:"carName"`
	TrackName       string `json:"trackName"`
	Source          string `json:"source"`
	State           string `json:"state"`
	QueuedAt        string `json:"queuedAt"`
	StartedAt       string `json:"startedAt,omitempty"`
	FinishedAt      string `json:"finishedAt,omitempty"`
	AttemptCount    int    `json:"attemptCount"`
	Error           string `json:"error,omitempty"`
}

const garageQueueColumns = `id,car_platform_id,track_platform_id,garage_car_id,garage_track_id,
 car_name,track_name,source,state,queued_at,started_at,finished_at,attempt_count,error`

func scanGarageQueue(row interface{ Scan(...any) error }) (GarageQueueItem, error) {
	var item GarageQueueItem
	var carPlatform, trackPlatform, garageCar, garageTrack sql.NullInt64
	var started, finished sql.NullString
	err := row.Scan(&item.ID, &carPlatform, &trackPlatform, &garageCar, &garageTrack,
		&item.CarName, &item.TrackName, &item.Source, &item.State, &item.QueuedAt,
		&started, &finished, &item.AttemptCount, &item.Error)
	if err != nil {
		return GarageQueueItem{}, err
	}
	item.CarPlatformID, item.TrackPlatformID = int(carPlatform.Int64), int(trackPlatform.Int64)
	item.GarageCarID, item.GarageTrackID = int(garageCar.Int64), int(garageTrack.Int64)
	item.StartedAt, item.FinishedAt = started.String, finished.String
	return item, nil
}

func (s *Store) ListGarageQueue() ([]GarageQueueItem, error) {
	rows, err := s.reader.Query(`SELECT ` + garageQueueColumns + ` FROM brake_garage61_queue
 ORDER BY CASE state WHEN 'running' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,
 queued_at,id`)
	if err != nil {
		return nil, fmt.Errorf("store: list Garage61 queue: %w", err)
	}
	defer rows.Close()
	out := []GarageQueueItem{}
	for rows.Next() {
		item, err := scanGarageQueue(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan Garage61 queue: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// EnqueueGaragePair queues or requeues a combination only after a user action.
func (s *Store) EnqueueGaragePair(carPlatform, trackPlatform, garageCar, garageTrack int, carName, trackName, source string) (GarageQueueItem, error) {
	if source != "manual" && source != "refresh" {
		return GarageQueueItem{}, errors.New("store: invalid Garage61 queue source")
	}
	key := ""
	var platformCar, platformTrack, sourceCar, sourceTrack any
	if carPlatform > 0 && trackPlatform > 0 {
		key = fmt.Sprintf("p:%d:%d", carPlatform, trackPlatform)
		platformCar, platformTrack = carPlatform, trackPlatform
	} else if garageCar > 0 && garageTrack > 0 {
		key = fmt.Sprintf("g:%d:%d", garageCar, garageTrack)
	} else if source == "refresh" && carName != "" && trackName != "" {
		key = "n:" + carName + "\x1f" + trackName
	} else {
		return GarageQueueItem{}, errors.New("store: Garage61 queue needs a complete car and track pair")
	}
	if garageCar > 0 && garageTrack > 0 {
		sourceCar, sourceTrack = garageCar, garageTrack
	}
	tx, err := s.writer.Begin()
	if err != nil {
		return GarageQueueItem{}, fmt.Errorf("store: begin Garage61 enqueue: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO brake_garage61_queue
 (queue_key,car_platform_id,track_platform_id,garage_car_id,garage_track_id,
  car_name,track_name,source,state,queued_at)
 VALUES (?,?,?,?,?,?,?,?,'queued',?)
 ON CONFLICT(queue_key) DO UPDATE SET
 garage_car_id=COALESCE(excluded.garage_car_id,brake_garage61_queue.garage_car_id),
 garage_track_id=COALESCE(excluded.garage_track_id,brake_garage61_queue.garage_track_id),
 car_name=excluded.car_name, track_name=excluded.track_name,
 source=excluded.source,
 state=CASE WHEN brake_garage61_queue.state='running' THEN 'running' ELSE 'queued' END,
 rerun_requested=CASE WHEN brake_garage61_queue.state='running' THEN 1 ELSE 0 END,
 queued_at=excluded.queued_at,
 finished_at=CASE WHEN brake_garage61_queue.state='running' THEN brake_garage61_queue.finished_at ELSE NULL END,
 error=CASE WHEN brake_garage61_queue.state='running' THEN brake_garage61_queue.error ELSE '' END`,
		key, platformCar, platformTrack, sourceCar, sourceTrack, carName, trackName, source, Now())
	if err != nil {
		return GarageQueueItem{}, fmt.Errorf("store: enqueue Garage61 combination: %w", err)
	}
	if garageCar > 0 && garageTrack > 0 {
		if _, err := tx.Exec(`DELETE FROM brake_garage61_deleted WHERE garage_car_id=? AND garage_track_id=?`, garageCar, garageTrack); err != nil {
			return GarageQueueItem{}, fmt.Errorf("store: clear Garage61 deletion marker: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return GarageQueueItem{}, fmt.Errorf("store: commit Garage61 enqueue: %w", err)
	}
	item, err := scanGarageQueue(s.reader.QueryRow(`SELECT `+garageQueueColumns+` FROM brake_garage61_queue WHERE queue_key=?`, key))
	if err != nil {
		return GarageQueueItem{}, fmt.Errorf("store: read enqueued Garage61 combination: %w", err)
	}
	return item, nil
}

func (s *Store) RecoverGarageQueue() error {
	_, err := s.writer.Exec(`UPDATE brake_garage61_queue SET state='queued', started_at=NULL,
 rerun_requested=0, error='Interrupted before LapDog stopped' WHERE state='running'`)
	if err != nil {
		return fmt.Errorf("store: recover Garage61 queue: %w", err)
	}
	return nil
}

func (s *Store) ClaimNextGarageQueue() (*GarageQueueItem, error) {
	item, err := scanGarageQueue(s.writer.QueryRow(`UPDATE brake_garage61_queue
 SET state='running', started_at=?, finished_at=NULL, attempt_count=attempt_count+1,
 rerun_requested=0, error=''
 WHERE id=(SELECT id FROM brake_garage61_queue WHERE state='queued' ORDER BY queued_at,id LIMIT 1)
 RETURNING `+garageQueueColumns, Now()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: claim Garage61 queue: %w", err)
	}
	return &item, nil
}

func (s *Store) ResolveGarageQueue(id int64, carID, trackID int, carName, trackName string) error {
	result, err := s.writer.Exec(`UPDATE brake_garage61_queue SET garage_car_id=?,garage_track_id=?,
 car_name=?,track_name=? WHERE id=? AND state='running'`, carID, trackID, carName, trackName, id)
	if err != nil {
		return fmt.Errorf("store: resolve Garage61 queue: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: Garage61 queue item %d", ErrNotFound, id)
	}
	return nil
}

func (s *Store) FinishGarageQueue(id int64, state, detail string) error {
	switch state {
	case "done", "review", "failed", "unmatched", "cancelled", "deleted":
	default:
		return errors.New("store: invalid Garage61 queue result")
	}
	result, err := s.writer.Exec(`UPDATE brake_garage61_queue SET
 state=CASE WHEN rerun_requested=1 THEN 'queued' ELSE ? END,
 started_at=CASE WHEN rerun_requested=1 THEN NULL ELSE started_at END,
 finished_at=CASE WHEN rerun_requested=1 THEN NULL ELSE ? END,
 error=CASE WHEN rerun_requested=1 THEN '' ELSE ? END,
 rerun_requested=0
 WHERE id=? AND state='running'`, state, Now(), detail, id)
	if err != nil {
		return fmt.Errorf("store: finish Garage61 queue: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: Garage61 queue item %d", ErrNotFound, id)
	}
	return nil
}

func (s *Store) RetryGarageQueue(id int64) error {
	result, err := s.writer.Exec(`UPDATE brake_garage61_queue SET state='queued',queued_at=?,
 started_at=NULL,finished_at=NULL,error='' WHERE id=? AND state IN ('failed','unmatched','review','cancelled')`, Now(), id)
	if err != nil {
		return fmt.Errorf("store: retry Garage61 queue: %w", err)
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return fmt.Errorf("%w: retryable Garage61 queue item %d", ErrNotFound, id)
	}
	return nil
}

func (s *Store) GarageCombinationDeleted(carID, trackID int) (bool, error) {
	var deleted bool
	err := s.reader.QueryRow(`SELECT EXISTS(SELECT 1 FROM brake_garage61_deleted
 WHERE garage_car_id=? AND garage_track_id=?)`, carID, trackID).Scan(&deleted)
	return deleted, err
}

// DeleteGarageCombination hides downloaded scenarios, erases their citations,
// and leaves a tombstone so packaged imports do not silently restore them.
// Run references still need the scalar scenario row.
func (s *Store) DeleteGarageCombination(carID, trackID int) (int64, error) {
	return s.DeleteGarageCombinations([]GarageCombinationID{{CarID: carID, TrackID: trackID}})
}

// GarageCombinationID identifies one Garage61 car and track layout.
type GarageCombinationID struct {
	CarID   int `json:"carId"`
	TrackID int `json:"trackId"`
}

// DeleteGarageCombinations retires all requested combinations in one
// transaction, so an invalid selection cannot leave a partial deletion.
func (s *Store) DeleteGarageCombinations(combinations []GarageCombinationID) (int64, error) {
	if len(combinations) == 0 {
		return 0, errors.New("store: select at least one Garage61 combination")
	}
	seen := make(map[GarageCombinationID]bool, len(combinations))
	for _, pair := range combinations {
		if pair.CarID <= 0 || pair.TrackID <= 0 {
			return 0, errors.New("store: invalid Garage61 combination IDs")
		}
		if seen[pair] {
			return 0, errors.New("store: duplicate Garage61 combination")
		}
		seen[pair] = true
	}
	tx, err := s.writer.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var total int64
	for _, pair := range combinations {
		prefix := fmt.Sprintf("garage61-iracing-track-%d-car-%d-zone-", pair.TrackID, pair.CarID)
		result, err := tx.Exec(`UPDATE brake_scenarios SET retired=1,source_json=NULL,
 local_generated=1,review_needed=0 WHERE source_provider='garage61' AND retired=0
 AND substr(id,1,?)=?`, len(prefix), prefix)
		if err != nil {
			return 0, fmt.Errorf("store: delete Garage61 scenarios: %w", err)
		}
		count, _ := result.RowsAffected()
		if count == 0 {
			return 0, fmt.Errorf("%w: Garage61 combination %d/%d", ErrNotFound, pair.CarID, pair.TrackID)
		}
		total += count
		if _, err := tx.Exec(`UPDATE brake_runs SET scenario_snapshot_json=json_remove(scenario_snapshot_json,'$.source')
 WHERE substr(scenario_id,1,?)=?`, len(prefix), prefix); err != nil {
			return 0, fmt.Errorf("store: clear Garage61 citations in practice results: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO brake_garage61_deleted
 (garage_car_id,garage_track_id,deleted_at) VALUES (?,?,?)
 ON CONFLICT(garage_car_id,garage_track_id) DO UPDATE SET deleted_at=excluded.deleted_at`, pair.CarID, pair.TrackID, Now()); err != nil {
			return 0, fmt.Errorf("store: mark Garage61 combination deleted: %w", err)
		}
		if _, err := tx.Exec(`UPDATE brake_garage61_queue SET state='deleted',finished_at=?,
 error='Scenarios deleted locally' WHERE garage_car_id=? AND garage_track_id=?
 AND state!='running'`, Now(), pair.CarID, pair.TrackID); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return total, nil
}
