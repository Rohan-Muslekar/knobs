package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by repository reads when no row matches.
var ErrNotFound = errors.New("not found")

// DBTX is the subset of pgx used by repository methods. Both *pgxpool.Pool
// and pgx.Tx satisfy it, so a query runs either directly or inside a tx.
type DBTX interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Compile-time proof that DBTX matches pgx v5's real signatures: both the
// pool and a transaction must satisfy it.
var (
	_ DBTX = (*pgxpool.Pool)(nil)
	_ DBTX = pgx.Tx(nil)
)

// Repo is the persistence entry point.
type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Pool exposes the underlying pool for callers that pass it as a DBTX.
func (r *Repo) Pool() *pgxpool.Pool { return r.pool }

// WithTx runs fn inside a transaction, committing on success and rolling
// back on error or panic.
func (r *Repo) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
