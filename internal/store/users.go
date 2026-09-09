package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

func (r *Repo) CreateUser(ctx context.Context, db DBTX, email, passwordHash string) (User, error) {
	var u User
	err := db.QueryRow(ctx,
		`INSERT INTO app_user (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at`,
		email, passwordHash,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	return u, err
}

func (r *Repo) UserByEmail(ctx context.Context, db DBTX, email string) (User, error) {
	var u User
	err := db.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM app_user
		 WHERE lower(email) = lower($1)`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (r *Repo) UserByID(ctx context.Context, db DBTX, id uuid.UUID) (User, error) {
	var u User
	err := db.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at FROM app_user WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (r *Repo) CountUsers(ctx context.Context, db DBTX) (int, error) {
	var n int
	err := db.QueryRow(ctx, `SELECT count(*) FROM app_user`).Scan(&n)
	return n, err
}
