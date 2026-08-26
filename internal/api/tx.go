package api

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// withTx runs fn inside a database transaction: begins, runs fn, then
// commits on success or rolls back on error/panic. Shared by every handler
// that needs more than one statement to be atomic (progress upsert + XP,
// streak-shield consumption, workout split/session writes).
func withTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op if already committed

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
