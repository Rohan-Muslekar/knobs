-- +goose Up
CREATE TABLE api_key (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environment(id) ON DELETE CASCADE,
    project_id     uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name           text NOT NULL,
    hash           text NOT NULL UNIQUE,
    scope          text NOT NULL DEFAULT 'read',
    created_at     timestamptz NOT NULL DEFAULT now(),
    last_used_at   timestamptz
);

-- +goose Down
DROP TABLE api_key;
