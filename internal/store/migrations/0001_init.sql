CREATE TABLE schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version (version) VALUES (1);

CREATE TABLE sessions (
  id                     INTEGER PRIMARY KEY,
  uuid                   TEXT    NOT NULL UNIQUE,
  session_key            TEXT    NOT NULL UNIQUE,
  subsession_id          INTEGER NOT NULL DEFAULT 0,
  session_num            INTEGER NOT NULL,
  session_type           TEXT    NOT NULL,
  event_context          TEXT    NOT NULL,
  league_id              INTEGER NOT NULL DEFAULT 0,
  series_id              INTEGER NOT NULL DEFAULT 0,
  season_id              INTEGER NOT NULL DEFAULT 0,
  official               INTEGER NOT NULL DEFAULT 0,
  track_id               INTEGER,
  track_name             TEXT,
  track_config           TEXT,
  track_length_km        REAL,
  car_id                 INTEGER,
  car_name               TEXT,
  car_class_id           INTEGER,
  car_class_name         TEXT,
  started_at             TEXT    NOT NULL,
  ended_at               TEXT,
  connected_seconds      REAL    NOT NULL DEFAULT 0,
  in_car_seconds         REAL    NOT NULL DEFAULT 0,
  driving_seconds        REAL    NOT NULL DEFAULT 0,
  laps_completed         INTEGER NOT NULL DEFAULT 0,
  incidents              INTEGER NOT NULL DEFAULT 0,
  best_lap_time_s        REAL,
  starting_position      INTEGER,
  finish_position        INTEGER,
  finish_class_position  INTEGER,
  qualify_position       INTEGER,
  qualify_class_position INTEGER,
  qualify_best_time_s    REAL,
  field_size             INTEGER,
  ai_opponent_count      INTEGER NOT NULL DEFAULT 0,
  ai_detection           TEXT,
  incident_source        TEXT    NOT NULL DEFAULT 'yaml',
  classify_source_json   TEXT    NOT NULL,
  capture_file           TEXT,
  created_at             TEXT    NOT NULL,
  updated_at             TEXT    NOT NULL,
  uploaded_at            TEXT
);

CREATE INDEX idx_sessions_started  ON sessions(started_at);
CREATE INDEX idx_sessions_type_ctx ON sessions(session_type, event_context);
CREATE INDEX idx_sessions_track    ON sessions(track_id);
CREATE INDEX idx_sessions_car      ON sessions(car_id);
CREATE INDEX idx_sessions_upload   ON sessions(uploaded_at);
CREATE INDEX idx_sessions_ai       ON sessions(ai_detection);

CREATE TABLE laps (
  id               INTEGER PRIMARY KEY,
  uuid             TEXT    NOT NULL UNIQUE,
  session_id       INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  lap_number       INTEGER NOT NULL,
  lap_time_s       REAL,
  delta_to_best_s  REAL,
  fuel_used_l      REAL,
  fuel_level_end_l REAL,
  incidents_on_lap INTEGER NOT NULL DEFAULT 0,
  is_pit_lap       INTEGER NOT NULL DEFAULT 0,
  position         INTEGER,
  class_position   INTEGER,
  recorded_at      TEXT    NOT NULL,
  uploaded_at      TEXT,
  UNIQUE(session_id, lap_number)
);

CREATE INDEX idx_laps_session ON laps(session_id, lap_number);
CREATE INDEX idx_laps_time    ON laps(lap_time_s);

CREATE TABLE position_events (
  id               INTEGER PRIMARY KEY,
  uuid             TEXT    NOT NULL UNIQUE,
  session_id       INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  lap_number       INTEGER NOT NULL,
  session_time_s   REAL    NOT NULL,
  from_position    INTEGER NOT NULL,
  to_position      INTEGER NOT NULL,
  is_class         INTEGER NOT NULL DEFAULT 0,
  opponent_car_idx INTEGER,
  opponent_name    TEXT,
  cause            TEXT    NOT NULL,
  recorded_at      TEXT    NOT NULL,
  uploaded_at      TEXT
);

CREATE INDEX idx_pos_session ON position_events(session_id, lap_number);
CREATE INDEX idx_pos_cause   ON position_events(cause);
