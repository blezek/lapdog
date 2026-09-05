-- The iRacing licence category whose iRating and Safety Rating the session
-- reported. WeekendInfo.Category is the authoritative source: current builds use
-- SportsCar, FormulaCar, Oval, DirtRoad and DirtOval; older captures used Road
-- before the paved-road licences split.
--
-- Keep the simulator's raw value here. The API maps known spellings to display
-- disciplines, while an unfamiliar future value stays identifiable rather than
-- being silently assigned to the wrong licence.

ALTER TABLE sessions ADD COLUMN driver_rating_category TEXT;

-- Classification provenance contains the full parsed WeekendInfo document, so
-- existing sessions can be attributed without replaying their capture files.
UPDATE sessions
SET driver_rating_category = json_extract(classify_source_json, '$.WeekendInfo.Category')
WHERE driver_irating IS NOT NULL OR driver_safety_rating IS NOT NULL;
