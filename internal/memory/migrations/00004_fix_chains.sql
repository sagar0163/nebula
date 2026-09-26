-- +goose Up
ALTER TABLE patterns ADD COLUMN fix_chain TEXT DEFAULT '';

-- +goose Down
ALTER TABLE patterns DROP COLUMN fix_chain;
