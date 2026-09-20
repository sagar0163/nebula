-- +goose Up
CREATE TABLE tasks (
    id         TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    input      TEXT NOT NULL,
    response   TEXT NOT NULL DEFAULT '',
    domain     TEXT NOT NULL DEFAULT 'general',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX index_tasks_session_created ON tasks (session_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS tasks;