CREATE TABLE brake_garage61_refresh (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  snooze_until TEXT
);
INSERT INTO brake_garage61_refresh (id) VALUES (1);
ALTER TABLE brake_scenarios ADD COLUMN review_needed INTEGER NOT NULL DEFAULT 0;
