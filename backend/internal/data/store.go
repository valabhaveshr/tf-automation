package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgUniqueViolationCode is the Postgres SQLSTATE for a unique_violation.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const pgUniqueViolationCode = "23505"

// Store is the WS-A repository: a thin, typed wrapper around a
// *pgxpool.Pool exposing CRUD and domain-specific operations for every
// table owned by the Data Layer. Callers (WS-D, WS-G, WS-I) should depend
// on this type, not on pgxpool directly, so the schema/error-handling
// details stay in one place.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps an already-constructed pool. Prefer this in tests / when
// the caller wants to manage pool lifecycle (e.g. share one pool across
// multiple packages).
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Connect builds a pgxpool.Pool from a standard Postgres connection string
// (e.g. "postgres://user:pass@host:5432/dbname?sslmode=disable") and
// verifies connectivity with a Ping before returning.
func Connect(ctx context.Context, connString string) (*Store, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("data: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("data: connect: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases all pooled connections. Safe to call once during shutdown.
func (s *Store) Close() {
	s.pool.Close()
}

// Pool exposes the underlying pool for callers that need to run raw
// queries or compose a multi-table transaction spanning operations this
// package doesn't otherwise provide (e.g. WS-G's save transaction that
// allocates a naming counter and inserts a record together).
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// rowScanner is satisfied by both pgx.Row (QueryRow) and *pgx.Rows
// (Query), letting scan helpers work against either.
type rowScanner interface {
	Scan(dest ...any) error
}

// asUniqueViolation returns the *pgconn.PgError and true if err represents a
// Postgres unique_violation (SQLSTATE 23505).
func asUniqueViolation(err error) (*pgconn.PgError, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode {
		return pgErr, true
	}
	return nil, false
}

// wrapConflict converts a unique_violation into a *ConflictError; callers
// pass the original error through unchanged when it isn't one.
func wrapConflict(err error) error {
	if pgErr, ok := asUniqueViolation(err); ok {
		return &ConflictError{Constraint: pgErr.ConstraintName, Detail: pgErr.Message}
	}
	return err
}
