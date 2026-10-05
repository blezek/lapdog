-- Searchable catalog metadata for read-only Brake-It scenarios. The imported
-- source JSON is deliberately aggregate-only; raw telemetry and lap/driver
-- identifiers never enter the LapDog database.

ALTER TABLE brake_scenarios ADD COLUMN car_name TEXT;
ALTER TABLE brake_scenarios ADD COLUMN track_name TEXT;
ALTER TABLE brake_scenarios ADD COLUMN source_provider TEXT
  CHECK (source_provider IS NULL OR source_provider = 'garage61');

CREATE INDEX idx_brake_scenarios_car_track
  ON brake_scenarios(car_name, track_name, retired);
