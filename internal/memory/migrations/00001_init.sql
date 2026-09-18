-- +goose Up
CREATE TABLE sessions (
    id         TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    work_dir   TEXT NOT NULL DEFAULT '',
    summary    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE commands (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL DEFAULT '',
    raw        TEXT NOT NULL,
    exit_code  INTEGER NOT NULL DEFAULT 0,
    stdout     TEXT NOT NULL DEFAULT '',
    stderr     TEXT NOT NULL DEFAULT '',
    elapsed_ms INTEGER NOT NULL DEFAULT 0,
    work_dir   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX index_commands_session_created ON commands (session_id, id DESC);

CREATE TABLE patterns (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    fail_cmd     TEXT NOT NULL,
    fail_output  TEXT NOT NULL DEFAULT '',
    fix_cmd      TEXT NOT NULL,
    success_rate REAL NOT NULL DEFAULT 0,
    use_count    INTEGER NOT NULL DEFAULT 0,
    embedding    BLOB NOT NULL,
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX index_patterns_fail_cmd ON patterns (fail_cmd);

CREATE TABLE permissions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    pattern    TEXT NOT NULL UNIQUE,
    decision   TEXT NOT NULL DEFAULT 'ask' CHECK (decision IN ('allow', 'deny', 'ask')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS patterns;
DROP TABLE IF EXISTS commands;
DROP TABLE IF EXISTS sessions;