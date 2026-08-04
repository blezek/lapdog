package store

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Lap is one row of the laps table.
type Lap struct {
	ID        int64  `json:"id"`
	UUID      string `json:"uuid"`
	SessionID int64  `json:"sessionId"`
	LapNumber int    `json:"lapNumber"`

	LapTimeS      *float64 `json:"lapTimeS"`
	DeltaToBestS  *float64 `json:"deltaToBestS"`
	FuelUsedL     *float64 `json:"fuelUsedL"`
	FuelLevelEndL *float64 `json:"fuelLevelEndL"`

	IncidentsOnLap int  `json:"incidentsOnLap"`
	IsPitLap       bool `json:"isPitLap"`

	Position      *int `json:"position"`
	ClassPosition *int `json:"classPosition"`

	RecordedAt string  `json:"recordedAt"`
	UploadedAt *string `json:"uploadedAt"`
}

const lapColumns = `
	id, uuid, session_id, lap_number,
	lap_time_s, delta_to_best_s, fuel_used_l, fuel_level_end_l,
	incidents_on_lap, is_pit_lap,
	position, class_position,
	recorded_at, uploaded_at`

// InsertLap records a completed lap.
//
// It is idempotent on (session_id, lap_number): a collector restart can
// legitimately re-observe a lap it already wrote, and the first write wins.
// Returning a duplicate-key error here would crash the poll loop for no benefit.
func (s *Store) InsertLap(rec *Lap) (int64, error) {
	if rec == nil {
		return 0, errors.New("store: InsertLap called with nil record")
	}
	if rec.UUID == "" {
		rec.UUID = uuid.NewString()
	}
	if rec.RecordedAt == "" {
		rec.RecordedAt = Now()
	}

	const q = `
INSERT INTO laps (
	uuid, session_id, lap_number,
	lap_time_s, delta_to_best_s, fuel_used_l, fuel_level_end_l,
	incidents_on_lap, is_pit_lap,
	position, class_position, recorded_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, lap_number) DO NOTHING`

	if _, err := s.writer.Exec(q,
		rec.UUID, rec.SessionID, rec.LapNumber,
		rec.LapTimeS, rec.DeltaToBestS, rec.FuelUsedL, rec.FuelLevelEndL,
		rec.IncidentsOnLap, rec.IsPitLap,
		rec.Position, rec.ClassPosition, rec.RecordedAt,
	); err != nil {
		return 0, fmt.Errorf("store: insert lap %d of session %d: %w", rec.LapNumber, rec.SessionID, err)
	}

	// DO NOTHING means RETURNING yields no row, so the id is read back. This also
	// gives the pre-existing id when the insert was a no-op.
	var id int64
	if err := s.writer.QueryRow(
		`SELECT id FROM laps WHERE session_id = ? AND lap_number = ?`,
		rec.SessionID, rec.LapNumber,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: read back lap %d of session %d: %w", rec.LapNumber, rec.SessionID, err)
	}
	rec.ID = id
	return id, nil
}

// LapsForSession returns every recorded lap for a session, in lap order. A
// session with no laps yields an empty slice, not ErrNotFound.
func (s *Store) LapsForSession(sessionID int64) ([]Lap, error) {
	rows, err := s.reader.Query(
		`SELECT `+lapColumns+` FROM laps WHERE session_id = ? ORDER BY lap_number`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query laps for session %d: %w", sessionID, err)
	}
	defer rows.Close()

	out := []Lap{}
	for rows.Next() {
		var r Lap
		if err := rows.Scan(
			&r.ID, &r.UUID, &r.SessionID, &r.LapNumber,
			&r.LapTimeS, &r.DeltaToBestS, &r.FuelUsedL, &r.FuelLevelEndL,
			&r.IncidentsOnLap, &r.IsPitLap,
			&r.Position, &r.ClassPosition,
			&r.RecordedAt, &r.UploadedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan lap: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate laps: %w", err)
	}
	return out, nil
}
