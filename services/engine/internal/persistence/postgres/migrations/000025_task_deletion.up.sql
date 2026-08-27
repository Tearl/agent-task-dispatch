ALTER TABLE tasks ADD COLUMN IF NOT EXISTS deletion_requested_at timestamptz;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

CREATE INDEX IF NOT EXISTS tasks_publisher_visible_idx
    ON tasks (publisher_id, created_at DESC)
    WHERE deleted_at IS NULL;
