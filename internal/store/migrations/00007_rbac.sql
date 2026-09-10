-- +goose Up
CREATE TABLE organization (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    slug text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE organization_member (
    organization_id uuid NOT NULL REFERENCES organization(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    role text NOT NULL CHECK (role IN ('viewer','editor','admin','owner')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id)
);
ALTER TABLE project ADD COLUMN organization_id uuid REFERENCES organization(id) ON DELETE CASCADE;
INSERT INTO organization (name, slug) VALUES ('Default', 'default');
UPDATE project SET organization_id = (SELECT id FROM organization WHERE slug = 'default') WHERE organization_id IS NULL;
ALTER TABLE project ALTER COLUMN organization_id SET NOT NULL;
CREATE INDEX project_org_idx ON project (organization_id);
CREATE INDEX org_member_user_idx ON organization_member (user_id);

-- +goose Down
DROP INDEX org_member_user_idx;
DROP INDEX project_org_idx;
ALTER TABLE project DROP COLUMN organization_id;
DROP TABLE organization_member;
DROP TABLE organization;
