-- A request made while a combination is running must survive that attempt's
-- completion. Seconds-resolution timestamps cannot distinguish same-second
-- requests, so retain the intent explicitly.
ALTER TABLE brake_garage61_queue
  ADD COLUMN rerun_requested INTEGER NOT NULL DEFAULT 0
  CHECK (rerun_requested IN (0, 1));
