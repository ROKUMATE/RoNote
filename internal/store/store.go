// Package store persists RoNote data in Postgres. Callers get ErrNotFound and
// ErrConflict for the cases they handle; every other error is opaque and
// never exposes pgx types.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means the write would break a unique constraint.
	ErrConflict = errors.New("conflict")
)

// uniqueViolation is the Postgres SQLSTATE for unique_violation.
const uniqueViolation = "23505"

// User is a registered user.
type User = sqlcdb.User

// Store runs queries against the database.
type Store struct {
	pool *pgxpool.Pool // nil for a Store bound to a transaction
	q    *sqlcdb.Queries
}

// New returns a Store that uses pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlcdb.New(pool)}
}

// InTx runs fn in one transaction and commits it when fn returns nil. fn
// gets a Store bound to the transaction, and the transaction itself so that
// a job queue can commit its own writes with the same transaction. fn's
// error is returned unchanged.
func (s *Store) InTx(ctx context.Context, fn func(tx *Store, raw pgx.Tx) error) error {
	if s.pool == nil {
		return errors.New("store: InTx called inside a transaction")
	}
	var fnErr error
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		fnErr = fn(&Store{q: s.q.WithTx(tx)}, tx)
		return fnErr
	})
	if fnErr != nil {
		return fnErr
	}
	if err != nil {
		return mapErr("transaction", err)
	}
	return nil
}

// CreateUser inserts a user. It returns ErrConflict when another user has
// the same email, ignoring case.
func (s *Store) CreateUser(ctx context.Context, email, displayName string) (User, error) {
	u, err := s.q.CreateUser(ctx, sqlcdb.CreateUserParams{Email: email, DisplayName: displayName})
	if err != nil {
		return User{}, mapErr("create user", err)
	}
	return u, nil
}

// GetUserByEmail returns the user with email, ignoring case, or ErrNotFound.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		return User{}, mapErr("get user by email", err)
	}
	return u, nil
}

// mapErr turns driver errors into ErrNotFound or ErrConflict. Anything else
// keeps its message but not its error chain, so callers cannot depend on pgx.
func mapErr(op string, err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%s: %w", op, ErrNotFound)
	case errors.As(err, &pgErr) && pgErr.Code == uniqueViolation:
		return fmt.Errorf("%s: %w", op, ErrConflict)
	default:
		return fmt.Errorf("%s: %s", op, err.Error())
	}
}
