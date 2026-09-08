package store

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

// Migrations live in-package (not repo-root) because //go:embed cannot reach outside the package tree.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// RunMigrations applies every embedded migration up. It expects a database/sql
// handle opened with the pgx stdlib driver.
func RunMigrations(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
