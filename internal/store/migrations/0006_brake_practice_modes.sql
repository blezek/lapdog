-- Distinguish a deliberately omitted acceleration exercise from a perfect
-- accelerator ramp. Existing runs predate this option and included acceleration.

ALTER TABLE brake_runs
  RENAME COLUMN accelerator_ramp_error_percent TO accelerator_ramp_error_percent_v1;

ALTER TABLE brake_runs
  ADD COLUMN acceleration_included INTEGER NOT NULL DEFAULT 1
  CHECK (acceleration_included IN (0, 1));

ALTER TABLE brake_runs
  ADD COLUMN accelerator_ramp_error_percent REAL;

UPDATE brake_runs
SET accelerator_ramp_error_percent = accelerator_ramp_error_percent_v1;
