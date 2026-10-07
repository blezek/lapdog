-- Databases opened by the earlier schema-10 build may contain automatically
-- queued history. Keep only combinations the user explicitly requested.
DROP TRIGGER IF EXISTS brake_garage61_queue_insert;
DROP TRIGGER IF EXISTS brake_garage61_queue_update;
DELETE FROM brake_garage61_queue WHERE source='history';
