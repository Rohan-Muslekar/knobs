-- +goose Up
ALTER TABLE audit_log DROP CONSTRAINT audit_log_project_id_fkey;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_project_id_fkey FOREIGN KEY (project_id) REFERENCES project (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE audit_log DROP CONSTRAINT audit_log_project_id_fkey;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_project_id_fkey FOREIGN KEY (project_id) REFERENCES project (id) ON DELETE CASCADE;
