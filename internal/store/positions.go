package store

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Cause records why a position change happened.
//
// A position change is not the same as an overtake: positions also shift when
// other drivers pit or retire, and crediting those as passes would inflate the
// ratio. Only CauseOnTrack counts toward pass and passed totals.
type Cause string

// Cause values.
const (
	// CauseOnTrack is a real on-track pass, and the only cause counted in the
	// pass/passed ratio.
	CauseOnTrack Cause = "OnTrack"
	// CauseOpponentPit means the car swapped with was on pit road.
	CauseOpponentPit Cause = "OpponentPit"
	// CauseOpponentOffWorld means the car swapped with had left the world:
	// crashed out, towed, or disconnected.
	CauseOpponentOffWorld Cause = "OpponentOffWorld"
	// CauseUnknown means the opponent could not be identified.
	CauseUnknown Cause = "Unknown"
)

// PositionEvent is one row of the position_events table.
type PositionEvent struct {
	ID        int64  `json:"id"`
	UUID      string `json:"uuid"`
	SessionID int64  `json:"sessionId"`

	LapNumber    int     `json:"lapNumber"`
	SessionTimeS float64 `json:"sessionTimeS"`
	FromPosition int     `json:"fromPosition"`
	ToPosition   int     `json:"toPosition"`
	IsClass      bool    `json:"isClass"`

	OpponentCarIdx *int    `json:"opponentCarIdx"`
	OpponentName   *string `json:"opponentName"`
	Cause          Cause   `json:"cause"`

	RecordedAt string  `json:"recordedAt"`
	UploadedAt *string `json:"uploadedAt"`
}

const positionColumns = `
	id, uuid, session_id, lap_number, session_time_s,
	from_position, to_position, is_class,
	opponent_car_idx, opponent_name, cause,
	recorded_at, uploaded_at`

// InsertPositionEvent records one position change.
//
// Unlike laps this is deliberately not deduplicated: repeated swaps between the
// same two positions are genuinely distinct events, and collapsing them would
// undercount a battle.
func (s *Store) InsertPositionEvent(rec *PositionEvent) (int64, error) {
	if rec == nil {
		return 0, errors.New("store: InsertPositionEvent called with nil record")
	}
	if rec.UUID == "" {
		rec.UUID = uuid.NewString()
	}
	if rec.RecordedAt == "" {
		rec.RecordedAt = Now()
	}
	if rec.Cause == "" {
		rec.Cause = CauseUnknown
	}

	const q = `
INSERT INTO position_events (
	uuid, session_id, lap_number, session_time_s,
	from_position, to_position, is_class,
	opponent_car_idx, opponent_name, cause, recorded_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id`

	var id int64
	err := s.writer.QueryRow(q,
		rec.UUID, rec.SessionID, rec.LapNumber, rec.SessionTimeS,
		rec.FromPosition, rec.ToPosition, rec.IsClass,
		rec.OpponentCarIdx, rec.OpponentName, string(rec.Cause), rec.RecordedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: insert position event for session %d: %w", rec.SessionID, err)
	}
	rec.ID = id
	return id, nil
}

// PositionEventsForSession returns a session's position events in time order.
func (s *Store) PositionEventsForSession(sessionID int64) ([]PositionEvent, error) {
	rows, err := s.reader.Query(
		`SELECT `+positionColumns+` FROM position_events WHERE session_id = ? ORDER BY session_time_s, id`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: query position events for session %d: %w", sessionID, err)
	}
	defer rows.Close()

	out := []PositionEvent{}
	for rows.Next() {
		var r PositionEvent
		var cause string
		if err := rows.Scan(
			&r.ID, &r.UUID, &r.SessionID, &r.LapNumber, &r.SessionTimeS,
			&r.FromPosition, &r.ToPosition, &r.IsClass,
			&r.OpponentCarIdx, &r.OpponentName, &cause,
			&r.RecordedAt, &r.UploadedAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan position event: %w", err)
		}
		r.Cause = Cause(cause)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate position events: %w", err)
	}
	return out, nil
}
