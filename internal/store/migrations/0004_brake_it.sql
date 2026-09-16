-- Brake-it pedal-training scenarios and completed runs.
--
-- These tables live beside LapDog history so changing the interface port or
-- browser cannot make training data disappear. Browser-only device handles are
-- deliberately not persisted: Web Serial owns those permissions.

CREATE TABLE brake_scenarios (
  id                           TEXT PRIMARY KEY,
  name                         TEXT    NOT NULL,
  description                  TEXT    NOT NULL DEFAULT '',
  approach_ms                  INTEGER NOT NULL CHECK (approach_ms BETWEEN 0 AND 30000),
  accelerator_fall_target_ms   INTEGER NOT NULL CHECK (accelerator_fall_target_ms BETWEEN 1 AND 10000),
  brake_rise_target_ms         INTEGER NOT NULL CHECK (brake_rise_target_ms BETWEEN 1 AND 10000),
  target_brake_percent         REAL    NOT NULL CHECK (target_brake_percent BETWEEN 0 AND 100),
  brake_tolerance_percent      REAL    NOT NULL CHECK (brake_tolerance_percent BETWEEN 0 AND 100),
  brake_hold_ms                INTEGER NOT NULL CHECK (brake_hold_ms BETWEEN 0 AND 30000),
  trail_brake_release_ms       INTEGER NOT NULL CHECK (trail_brake_release_ms BETWEEN 1 AND 30000),
  transition_enabled           INTEGER NOT NULL DEFAULT 0 CHECK (transition_enabled IN (0, 1)),
  transition_mode              TEXT CHECK (transition_mode IS NULL OR transition_mode IN ('coast', 'low-brake')),
  transition_duration_ms       INTEGER CHECK (transition_duration_ms IS NULL OR transition_duration_ms BETWEEN 1 AND 10000),
  transition_brake_percent     REAL CHECK (transition_brake_percent IS NULL OR transition_brake_percent BETWEEN 0 AND 100),
  accelerator_ramp_ms          INTEGER NOT NULL CHECK (accelerator_ramp_ms BETWEEN 1 AND 30000),
  origin                       TEXT    NOT NULL CHECK (origin IN ('builtin', 'custom')),
  catalog_version              INTEGER,
  retired                      INTEGER NOT NULL DEFAULT 0 CHECK (retired IN (0, 1)),
  source_json                  TEXT CHECK (source_json IS NULL OR json_valid(source_json)),
  created_at                   TEXT    NOT NULL,
  updated_at                   TEXT    NOT NULL
);

CREATE TABLE brake_runs (
  id                                TEXT PRIMARY KEY,
  scenario_id                       TEXT NOT NULL REFERENCES brake_scenarios(id) ON DELETE RESTRICT,
  scenario_name                     TEXT NOT NULL,
  scenario_snapshot_json            TEXT NOT NULL CHECK (json_valid(scenario_snapshot_json)),
  device_label                      TEXT NOT NULL,
  scoring_version                   INTEGER NOT NULL,
  score                             REAL NOT NULL CHECK (score BETWEEN 0 AND 100),
  accelerator_fall_ms               REAL,
  brake_rise_ms                     REAL,
  average_brake_deviation_percent   REAL NOT NULL,
  hold_time_in_band_ms              REAL NOT NULL,
  trail_error_percent               REAL NOT NULL,
  transition_error_percent          REAL,
  accelerator_ramp_error_percent    REAL NOT NULL,
  created_at                        TEXT NOT NULL
);

CREATE INDEX idx_brake_runs_created ON brake_runs(created_at DESC);
CREATE INDEX idx_brake_runs_scenario ON brake_runs(scenario_id, created_at DESC);

CREATE TABLE brake_samples (
  run_id                TEXT    NOT NULL REFERENCES brake_runs(id) ON DELETE CASCADE,
  sequence              INTEGER NOT NULL,
  time_ms               REAL    NOT NULL CHECK (time_ms >= 0),
  accelerator_percent   REAL    NOT NULL CHECK (accelerator_percent BETWEEN 0 AND 100),
  brake_percent         REAL    NOT NULL CHECK (brake_percent BETWEEN 0 AND 100),
  source                TEXT    NOT NULL,
  PRIMARY KEY (run_id, sequence)
);

CREATE TABLE brake_settings (
  id                    INTEGER PRIMARY KEY CHECK (id = 1),
  selected_scenario_id  TEXT REFERENCES brake_scenarios(id) ON DELETE SET NULL,
  baud_rate             INTEGER NOT NULL CHECK (baud_rate BETWEEN 1200 AND 3000000),
  usb_vendor_id         TEXT,
  usb_product_id        TEXT,
  updated_at            TEXT NOT NULL
);

INSERT INTO brake_scenarios (
  id, name, description, approach_ms, accelerator_fall_target_ms,
  brake_rise_target_ms, target_brake_percent, brake_tolerance_percent,
  brake_hold_ms, trail_brake_release_ms, transition_enabled,
  transition_mode, transition_duration_ms, transition_brake_percent,
  accelerator_ramp_ms, origin, catalog_version, retired, created_at, updated_at
) VALUES (
  'builtin-threshold-to-trail-baseline',
  'Threshold to Trail Baseline',
  'Fast accelerator release, brake to target, hold the threshold window, trail off, then rebuild throttle.',
  2200, 280, 420, 76, 6, 1050, 1850, 0,
  NULL, NULL, NULL, 1700, 'builtin', 1, 0,
  strftime('%Y-%m-%dT%H:%M:%SZ', 'now'),
  strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
);

INSERT INTO brake_settings (
  id, selected_scenario_id, baud_rate, updated_at
) VALUES (
  1, 'builtin-threshold-to-trail-baseline', 115200,
  strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
);
