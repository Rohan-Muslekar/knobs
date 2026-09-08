-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- knobs_meta is a scaffold baseline table so P0 can prove the migration
-- pipeline end to end. Domain tables arrive in P1.
CREATE TABLE knobs_meta (
    key        text PRIMARY KEY,
    value      text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO knobs_meta (key, value) VALUES ('schema_baseline', 'p0');

-- +goose Down
DROP TABLE knobs_meta;
DROP EXTENSION IF EXISTS pgcrypto;
