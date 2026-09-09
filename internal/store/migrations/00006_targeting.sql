-- +goose Up
ALTER TABLE config_version ADD COLUMN targeting jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE config_version DROP COLUMN targeting;
