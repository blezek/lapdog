-- Driver identity and rating, recorded per session.
--
-- The customer id is here so a database can say whose driving it holds. Until now
-- the local driver's identity was only present incidentally, inside the
-- classify_source_json blob, which exists to make a wrong classification rule
-- fixable and is the wrong place to read identity from.
--
-- iRating and Safety Rating are per session rather than per driver because they are
-- the point: both move after almost every official race, so a single current value
-- would answer "what is my iRating" while discarding "how did it get there". Stored
-- beside the session that observed them, they become a progression.
--
-- Every column is nullable. A session recorded before this migration has no values,
-- and offline or AI sessions may carry no rating at all — absent and zero are
-- different facts, and an iRating of 0 is a real value for an unrated licence.

ALTER TABLE sessions ADD COLUMN driver_user_id       INTEGER;
ALTER TABLE sessions ADD COLUMN driver_irating       INTEGER;
ALTER TABLE sessions ADD COLUMN driver_lic_string    TEXT;
ALTER TABLE sessions ADD COLUMN driver_lic_level     INTEGER;
ALTER TABLE sessions ADD COLUMN driver_lic_sublevel  INTEGER;

-- The Safety Rating as a number, derived from the licence string the simulator
-- displays. Kept alongside the raw fields rather than computed on read so a query
-- can order and average by it without re-parsing text.
ALTER TABLE sessions ADD COLUMN driver_safety_rating REAL;

-- Rating progression is read by time, and identity is read to answer "whose data is
-- this", so both want an index that supports ordering rather than lookup.
CREATE INDEX idx_sessions_driver ON sessions(driver_user_id, started_at);
