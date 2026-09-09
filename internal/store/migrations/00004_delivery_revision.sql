-- +goose Up
ALTER TABLE environment ADD COLUMN delivery_revision bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE environment DROP COLUMN delivery_revision;
