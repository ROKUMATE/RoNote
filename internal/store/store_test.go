package store

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMapErr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      error
		want    error
		wantMsg string
	}{
		{name: "no rows", in: pgx.ErrNoRows, want: ErrNotFound, wantMsg: "op: not found"},
		{
			name: "unique violation", in: &pgconn.PgError{Code: "23505", Message: "duplicate key"},
			want: ErrConflict, wantMsg: "op: conflict",
		},
		{
			name: "wrapped unique violation", in: fmt.Errorf("query: %w", &pgconn.PgError{Code: "23505"}),
			want: ErrConflict, wantMsg: "op: conflict",
		},
		{
			name: "other postgres error", in: &pgconn.PgError{Severity: "ERROR", Code: "23503", Message: "fk violation"},
			wantMsg: "op: ERROR: fk violation (SQLSTATE 23503)",
		},
		{name: "plain error", in: errors.New("connection reset"), wantMsg: "op: connection reset"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := mapErr("op", tt.in)

			if got.Error() != tt.wantMsg {
				t.Errorf("mapErr() = %q, want %q", got, tt.wantMsg)
			}
			for _, sentinel := range []error{ErrNotFound, ErrConflict} {
				if is, want := errors.Is(got, sentinel), errors.Is(tt.want, sentinel); is != want {
					t.Errorf("errors.Is(mapErr(), %v) = %v, want %v", sentinel, is, want)
				}
			}
			var pgErr *pgconn.PgError
			if errors.As(got, &pgErr) || errors.Is(got, pgx.ErrNoRows) {
				t.Errorf("mapErr() = %v exposes a pgx error", got)
			}
		})
	}
}
