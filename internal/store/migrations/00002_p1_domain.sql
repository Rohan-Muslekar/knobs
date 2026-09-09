-- +goose Up
CREATE TABLE project (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL,
    slug       text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE app_user (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text NOT NULL,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX app_user_email_lower_key ON app_user (lower(email));

CREATE TABLE environment (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         uuid NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    name               text NOT NULL,
    current_version_id uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, name)
);

CREATE TABLE config_schema (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid NOT NULL UNIQUE REFERENCES project (id) ON DELETE CASCADE,
    definition     jsonb NOT NULL DEFAULT '{"fields":[]}'::jsonb,
    schema_version integer NOT NULL DEFAULT 1,
    updated_by     uuid,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE config_version (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id uuid NOT NULL REFERENCES environment (id) ON DELETE CASCADE,
    version        integer NOT NULL,
    values         jsonb NOT NULL,
    schema_version integer NOT NULL,
    created_by     uuid,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (environment_id, version)
);

-- Circular reference resolved after both tables exist.
ALTER TABLE environment
    ADD CONSTRAINT environment_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES config_version (id) ON DELETE SET NULL;

CREATE TABLE audit_log (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid REFERENCES project (id) ON DELETE CASCADE,
    actor      uuid,
    action     text NOT NULL,
    target     text NOT NULL,
    diff       jsonb,
    at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_project_at_idx ON audit_log (project_id, at DESC);

-- +goose Down
DROP TABLE audit_log;
ALTER TABLE environment DROP CONSTRAINT environment_current_version_fk;
DROP TABLE config_version;
DROP TABLE config_schema;
DROP TABLE environment;
DROP TABLE app_user;
DROP TABLE project;
