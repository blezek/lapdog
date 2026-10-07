CREATE TABLE brake_garage61_queue (
  id INTEGER PRIMARY KEY,
  queue_key TEXT NOT NULL UNIQUE,
  car_platform_id INTEGER,
  track_platform_id INTEGER,
  garage_car_id INTEGER,
  garage_track_id INTEGER,
  car_name TEXT NOT NULL DEFAULT '',
  track_name TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL CHECK (source IN ('history','manual','refresh')),
  state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','done','review','failed','unmatched','cancelled','deleted')),
  queued_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_brake_garage61_queue_next ON brake_garage61_queue(state, queued_at, id);

CREATE TABLE brake_garage61_deleted (
  garage_car_id INTEGER NOT NULL,
  garage_track_id INTEGER NOT NULL,
  deleted_at TEXT NOT NULL,
  PRIMARY KEY (garage_car_id, garage_track_id)
);
