-- +goose Up
ALTER TABLE patterns ADD COLUMN efficiency_score REAL DEFAULT 1.0;

-- +goose Down
ALTER TABLE patterns DROP COLUMN efficiency_score;
