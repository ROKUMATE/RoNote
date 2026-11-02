//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rokumate/ronote/internal/pgtest"
)

var testDB *pgtest.DB

func TestMain(m *testing.M) {
	pgtest.Main(m, &testDB)
}

func TestUsers(t *testing.T) {
	create := func(email string) func(context.Context, *Store) (User, error) {
		return func(ctx context.Context, s *Store) (User, error) { return s.CreateUser(ctx, email, "Ada") }
	}
	get := func(email string) func(context.Context, *Store) (User, error) {
		return func(ctx context.Context, s *Store) (User, error) { return s.GetUserByEmail(ctx, email) }
	}

	tests := []struct {
		name      string
		existing  []string
		call      func(context.Context, *Store) (User, error)
		wantEmail string
		wantErr   error
	}{
		{name: "create", call: create("ada@example.com"), wantEmail: "ada@example.com"},
		{
			name: "create duplicate email differing only in case", existing: []string{"ada@example.com"},
			call: create("ADA@Example.com"), wantErr: ErrConflict,
		},
		{name: "get missing user", call: get("nobody@example.com"), wantErr: ErrNotFound},
		{
			name: "get ignores case", existing: []string{"ada@example.com"},
			call: get("ADA@EXAMPLE.COM"), wantEmail: "ada@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDB.Reset(t)
			s := New(testDB.Pool)
			for _, email := range tt.existing {
				if _, err := s.CreateUser(t.Context(), email, "Existing"); err != nil {
					t.Fatalf("seed %s: %v", email, err)
				}
			}

			got, err := tt.call(t.Context(), s)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) {
					t.Errorf("err = %v exposes a pgx error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got.Email != tt.wantEmail {
				t.Errorf("email = %q, want %q", got.Email, tt.wantEmail)
			}
			if got.ID.Version() != 7 {
				t.Errorf("id = %v, want a UUIDv7", got.ID)
			}
			// The database clock (in a VM on macOS) can run slightly ahead of ours.
			if age := time.Since(got.CreatedAt).Abs(); age > time.Minute {
				t.Errorf("created_at = %v, want about now", got.CreatedAt)
			}
		})
	}
}
