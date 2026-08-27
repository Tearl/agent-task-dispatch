DROP INDEX IF EXISTS tasks_publisher_visible_idx;
ALTER TABLE tasks DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE tasks DROP COLUMN IF EXISTS deletion_requested_at;
