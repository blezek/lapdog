package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound indicates no row matched the lookup.
var ErrNotFound = errors.New("store: not found")

// FormatTime renders t as an RFC3339 string in UTC, which is the only timestamp
// format stored in the database.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Now returns the current time in the stored format.
func Now() string { return FormatTime(time.Now()) }

// Session is one row of the sessions table.
//
// Nullable columns are pointers because absent and zero mean different things: a
// practice session has no finish position at all, which is not the same as
// finishing in position zero.
type Session struct {
	ID         int64  `json:"id"`
	UUID       string `json:"uuid"`
	SessionKey string `json:"sessionKey"`

	SubsessionID int    `json:"subsessionId"`
	SessionNum   int    `json:"sessionNum"`
	SessionType  string `json:"sessionType"`
	EventContext string `json:"eventContext"`

	LeagueID int `json:"leagueId"`
	SeriesID int `json:"seriesId"`
	SeasonID int `json:"seasonId"`
	Official int `json:"official"`

	TrackID       *int     `json:"trackId"`
	TrackName     *string  `json:"trackName"`
	TrackConfig   *string  `json:"trackConfig"`
	TrackLengthKm *float64 `json:"trackLengthKm"`

	CarID        *int    `json:"carId"`
	CarName      *string `json:"carName"`
	CarClassID   *int    `json:"carClassId"`
	CarClassName *string `json:"carClassName"`

	StartedAt string  `json:"startedAt"`
	EndedAt   *string `json:"endedAt"`

	ConnectedSeconds float64 `json:"connectedSeconds"`
	InCarSeconds     float64 `json:"inCarSeconds"`
	DrivingSeconds   float64 `json:"drivingSeconds"`

	LapsCompleted int      `json:"lapsCompleted"`
	Incidents     int      `json:"incidents"`
	BestLapTimeS  *float64 `json:"bestLapTimeS"`

	StartingPosition     *int     `json:"startingPosition"`
	FinishPosition       *int     `json:"finishPosition"`
	FinishClassPosition  *int     `json:"finishClassPosition"`
	QualifyPosition      *int     `json:"qualifyPosition"`
	QualifyClassPosition *int     `json:"qualifyClassPosition"`
	QualifyBestTimeS     *float64 `json:"qualifyBestTimeS"`
	FieldSize            *int     `json:"fieldSize"`

	AIOpponentCount int     `json:"aiOpponentCount"`
	AIDetection     *string `json:"aiDetection"`
	IncidentSource  string  `json:"incidentSource"`

	// Who was driving, and where their ratings stood at the time.
	//
	// Per session rather than per database because both ratings move after almost
	// every official race: a single current value would say what the iRating is while
	// discarding how it got there. Pointers because absent and zero differ — an
	// iRating of zero is a real value for an unrated licence.
	DriverUserID       *int     `json:"driverUserId"`
	DriverIRating      *int     `json:"driverIRating"`
	DriverLicString    *string  `json:"driverLicString"`
	DriverLicLevel     *int     `json:"driverLicLevel"`
	DriverLicSubLevel  *int     `json:"driverLicSubLevel"`
	DriverSafetyRating *float64 `json:"driverSafetyRating"`

	// ClassifySourceJSON is the YAML subset the classification was derived from.
	// It is what makes a wrong rule fixable retroactively, and is omitted from
	// API responses because it is large and only the reclassify path reads it.
	ClassifySourceJSON string  `json:"-"`
	CaptureFile        *string `json:"captureFile"`

	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	UploadedAt *string `json:"uploadedAt"`
}

// SessionKey derives the stable identity of a session segment.
//
// Online sessions are identified by subsession and session number. Offline
// sessions all report SubSessionID 0, so that pair is not unique among them and
// the start time is folded in — otherwise two offline test sessions on the same
// day would collide and the second would overwrite the first.
func SessionKey(subsessionID, sessionNum int, startedAt time.Time) string {
	if subsessionID != 0 {
		return strconv.Itoa(subsessionID) + "/" + strconv.Itoa(sessionNum)
	}
	return "offline/" + strconv.Itoa(sessionNum) + "/" + FormatTime(startedAt)
}

// sessionColumns is the column list used by every session read, in the order
// scanSession expects.
const sessionColumns = `
	id, uuid, session_key, subsession_id, session_num, session_type, event_context,
	league_id, series_id, season_id, official,
	track_id, track_name, track_config, track_length_km,
	car_id, car_name, car_class_id, car_class_name,
	started_at, ended_at,
	connected_seconds, in_car_seconds, driving_seconds,
	laps_completed, incidents, best_lap_time_s,
	starting_position, finish_position, finish_class_position,
	qualify_position, qualify_class_position, qualify_best_time_s, field_size,
	ai_opponent_count, ai_detection, incident_source,
	driver_user_id, driver_irating, driver_lic_string,
	driver_lic_level, driver_lic_sublevel, driver_safety_rating,
	classify_source_json, capture_file,
	created_at, updated_at, uploaded_at`

// sessionColumnsAliased is sessionColumns with every column qualified by the
// table alias s, for queries that join.
//
// It is spelled out rather than derived from sessionColumns by string
// substitution, because a silent mismatch between the two would surface as a
// runtime scan error instead of a compile failure. A test pins that they agree.
const sessionColumnsAliased = `
	s.id, s.uuid, s.session_key, s.subsession_id, s.session_num, s.session_type, s.event_context,
	s.league_id, s.series_id, s.season_id, s.official,
	s.track_id, s.track_name, s.track_config, s.track_length_km,
	s.car_id, s.car_name, s.car_class_id, s.car_class_name,
	s.started_at, s.ended_at,
	s.connected_seconds, s.in_car_seconds, s.driving_seconds,
	s.laps_completed, s.incidents, s.best_lap_time_s,
	s.starting_position, s.finish_position, s.finish_class_position,
	s.qualify_position, s.qualify_class_position, s.qualify_best_time_s, s.field_size,
	s.ai_opponent_count, s.ai_detection, s.incident_source,
	s.driver_user_id, s.driver_irating, s.driver_lic_string,
	s.driver_lic_level, s.driver_lic_sublevel, s.driver_safety_rating,
	s.classify_source_json, s.capture_file,
	s.created_at, s.updated_at, s.uploaded_at`

// UpsertSession inserts rec, or updates it if a row with the same session_key
// already exists.
//
// On update, id, uuid and created_at are preserved and everything else is
// overwritten. That is what lets the collector flush the same session every few
// seconds without churning its identity.
func (s *Store) UpsertSession(rec *Session) (int64, error) {
	if rec == nil {
		return 0, errors.New("store: UpsertSession called with nil record")
	}
	if rec.SessionKey == "" {
		return 0, errors.New("store: UpsertSession requires a SessionKey")
	}

	existing, err := s.SessionByKey(rec.SessionKey)
	switch {
	case err == nil:
		rec.ID = existing.ID
		rec.UUID = existing.UUID
		rec.CreatedAt = existing.CreatedAt
	case errors.Is(err, ErrNotFound):
		if rec.UUID == "" {
			rec.UUID = uuid.NewString()
		}
		if rec.CreatedAt == "" {
			rec.CreatedAt = Now()
		}
	default:
		return 0, err
	}
	rec.UpdatedAt = Now()
	if rec.IncidentSource == "" {
		rec.IncidentSource = "yaml"
	}
	if rec.ClassifySourceJSON == "" {
		// NOT NULL in the schema, and an empty object is honest about having
		// captured nothing rather than failing the write.
		rec.ClassifySourceJSON = "{}"
	}

	const q = `
INSERT INTO sessions (
	uuid, session_key, subsession_id, session_num, session_type, event_context,
	league_id, series_id, season_id, official,
	track_id, track_name, track_config, track_length_km,
	car_id, car_name, car_class_id, car_class_name,
	started_at, ended_at,
	connected_seconds, in_car_seconds, driving_seconds,
	laps_completed, incidents, best_lap_time_s,
	starting_position, finish_position, finish_class_position,
	qualify_position, qualify_class_position, qualify_best_time_s, field_size,
	ai_opponent_count, ai_detection, incident_source,
	driver_user_id, driver_irating, driver_lic_string,
	driver_lic_level, driver_lic_sublevel, driver_safety_rating,
	classify_source_json, capture_file,
	created_at, updated_at
) VALUES (
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
	?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
	?, ?, ?, ?, ?, ?
)
ON CONFLICT(session_key) DO UPDATE SET
	subsession_id = excluded.subsession_id,
	session_num = excluded.session_num,
	session_type = excluded.session_type,
	event_context = excluded.event_context,
	league_id = excluded.league_id,
	series_id = excluded.series_id,
	season_id = excluded.season_id,
	official = excluded.official,
	track_id = excluded.track_id,
	track_name = excluded.track_name,
	track_config = excluded.track_config,
	track_length_km = excluded.track_length_km,
	car_id = excluded.car_id,
	car_name = excluded.car_name,
	car_class_id = excluded.car_class_id,
	car_class_name = excluded.car_class_name,
	started_at = excluded.started_at,
	ended_at = excluded.ended_at,
	connected_seconds = excluded.connected_seconds,
	in_car_seconds = excluded.in_car_seconds,
	driving_seconds = excluded.driving_seconds,
	laps_completed = excluded.laps_completed,
	incidents = excluded.incidents,
	best_lap_time_s = excluded.best_lap_time_s,
	starting_position = excluded.starting_position,
	finish_position = excluded.finish_position,
	finish_class_position = excluded.finish_class_position,
	qualify_position = excluded.qualify_position,
	qualify_class_position = excluded.qualify_class_position,
	qualify_best_time_s = excluded.qualify_best_time_s,
	field_size = excluded.field_size,
	ai_opponent_count = excluded.ai_opponent_count,
	ai_detection = excluded.ai_detection,
	incident_source = excluded.incident_source,
	driver_user_id = excluded.driver_user_id,
	driver_irating = excluded.driver_irating,
	driver_lic_string = excluded.driver_lic_string,
	driver_lic_level = excluded.driver_lic_level,
	driver_lic_sublevel = excluded.driver_lic_sublevel,
	driver_safety_rating = excluded.driver_safety_rating,
	classify_source_json = excluded.classify_source_json,
	capture_file = excluded.capture_file,
	updated_at = excluded.updated_at
RETURNING id`

	var id int64
	err = s.writer.QueryRow(q,
		rec.UUID, rec.SessionKey, rec.SubsessionID, rec.SessionNum, rec.SessionType, rec.EventContext,
		rec.LeagueID, rec.SeriesID, rec.SeasonID, rec.Official,
		rec.TrackID, rec.TrackName, rec.TrackConfig, rec.TrackLengthKm,
		rec.CarID, rec.CarName, rec.CarClassID, rec.CarClassName,
		rec.StartedAt, rec.EndedAt,
		rec.ConnectedSeconds, rec.InCarSeconds, rec.DrivingSeconds,
		rec.LapsCompleted, rec.Incidents, rec.BestLapTimeS,
		rec.StartingPosition, rec.FinishPosition, rec.FinishClassPosition,
		rec.QualifyPosition, rec.QualifyClassPosition, rec.QualifyBestTimeS, rec.FieldSize,
		rec.AIOpponentCount, rec.AIDetection, rec.IncidentSource,
		rec.DriverUserID, rec.DriverIRating, rec.DriverLicString,
		rec.DriverLicLevel, rec.DriverLicSubLevel, rec.DriverSafetyRating,
		rec.ClassifySourceJSON, rec.CaptureFile,
		rec.CreatedAt, rec.UpdatedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: upsert session %s: %w", rec.SessionKey, err)
	}
	rec.ID = id
	return id, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(...any) error }

// scanSession reads one session row in sessionColumns order.
func scanSession(sc rowScanner) (*Session, error) {
	var r Session
	err := sc.Scan(
		&r.ID, &r.UUID, &r.SessionKey, &r.SubsessionID, &r.SessionNum, &r.SessionType, &r.EventContext,
		&r.LeagueID, &r.SeriesID, &r.SeasonID, &r.Official,
		&r.TrackID, &r.TrackName, &r.TrackConfig, &r.TrackLengthKm,
		&r.CarID, &r.CarName, &r.CarClassID, &r.CarClassName,
		&r.StartedAt, &r.EndedAt,
		&r.ConnectedSeconds, &r.InCarSeconds, &r.DrivingSeconds,
		&r.LapsCompleted, &r.Incidents, &r.BestLapTimeS,
		&r.StartingPosition, &r.FinishPosition, &r.FinishClassPosition,
		&r.QualifyPosition, &r.QualifyClassPosition, &r.QualifyBestTimeS, &r.FieldSize,
		&r.AIOpponentCount, &r.AIDetection, &r.IncidentSource,
		&r.DriverUserID, &r.DriverIRating, &r.DriverLicString,
		&r.DriverLicLevel, &r.DriverLicSubLevel, &r.DriverSafetyRating,
		&r.ClassifySourceJSON, &r.CaptureFile,
		&r.CreatedAt, &r.UpdatedAt, &r.UploadedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// SessionByKey looks up a session by its session_key.
//
// It reads through the writer connection because the upsert path calls it to
// decide insert-versus-update, and reading on the same connection the write will
// use avoids observing a stale WAL snapshot mid-flush.
func (s *Store) SessionByKey(key string) (*Session, error) {
	row := s.writer.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE session_key = ?`, key)
	rec, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: session_key %s", ErrNotFound, key)
	}
	if err != nil {
		return nil, fmt.Errorf("store: read session %s: %w", key, err)
	}
	return rec, nil
}

// SessionByID looks up a session by its primary key.
func (s *Store) SessionByID(id int64) (*Session, error) {
	row := s.reader.QueryRow(`SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	rec, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: session id %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("store: read session %d: %w", id, err)
	}
	return rec, nil
}

// DeleteSession removes a session and, by cascade, its laps and position events.
func (s *Store) DeleteSession(id int64) error {
	res, err := s.writer.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete session %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: delete session %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: session id %d", ErrNotFound, id)
	}
	return nil
}
