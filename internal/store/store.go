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
	q *sqlcdb.Queries
}

// New returns a Store that uses pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{q: sqlcdb.New(pool)}
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
