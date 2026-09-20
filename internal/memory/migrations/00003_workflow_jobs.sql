-- +goose Up
CREATE TABLE workflow_jobs (
    id            TEXT PRIMARY KEY,
    workflow_file TEXT NOT NULL,
    inputs        TEXT NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'running',
    current_step  TEXT NOT NULL DEFAULT '',
    output        TEXT NOT NULL DEFAULT '{}',
    error         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- +goose Down
DROP TABLE IF EXISTS workflow_jobs;
