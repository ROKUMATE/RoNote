//go:build integration

package store

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func ghIdentity(id, login, email string) Identity {
	return Identity{Provider: "github", ProviderUserID: id, Login: login, Email: email, DisplayName: login}
}

func TestSignIn(t *testing.T) {
	tests := []struct {
		name string
		// existing signs in, or creates users, before id signs in.
		existing  func(t *testing.T, s *Store) User
		id        Identity
		sameUser  bool
		wantLogin string
	}{
		{
			name:      "a new identity with a new email makes a new user",
			id:        ghIdentity("1", "ada", "ada@example.com"),
			wantLogin: "ada",
		},
		{
			name: "a known identity is its user, with the login refreshed",
			existing: func(t *testing.T, s *Store) User {
				u, err := s.SignIn(t.Context(), ghIdentity("1", "ada-old", "ada@example.com"))
				if err != nil {
					t.Fatal(err)
				}
				return u
			},
			id:        ghIdentity("1", "ada", "other@example.com"),
			sameUser:  true,
			wantLogin: "ada",
		},
		{
			name: "a new identity with an existing user's verified email joins that user",
			existing: func(t *testing.T, s *Store) User {
				u, err := s.CreateUser(t.Context(), "dev@ronote.local", "dev")
				if err != nil {
					t.Fatal(err)
				}
				return u
			},
			id:        ghIdentity("7", "dev", "DEV@ronote.local"),
			sameUser:  true,
			wantLogin: "dev",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDB.Reset(t)
			s := New(testDB.Pool)
			var before User
			if tt.existing != nil {
				before = tt.existing(t, s)
			}

			u, err := s.SignIn(t.Context(), tt.id)
			if err != nil {
				t.Fatal(err)
			}

			if same := u.ID == before.ID; same != tt.sameUser {
				t.Errorf("signed in as %v, existing user %v; same = %v, want %v", u.ID, before.ID, same, tt.sameUser)
			}
			a, err := s.GetAccount(t.Context(), u.ID, "github")
			if err != nil {
				t.Fatal(err)
			}
			if a.Login != tt.wantLogin {
				t.Errorf("login = %q, want %q", a.Login, tt.wantLogin)
			}
		})
	}
}

func TestSessions(t *testing.T) {
	const idle, lifetime = 30 * 24 * time.Hour, 90 * 24 * time.Hour
	tests := []struct {
		name  string
		age   string // how long ago the session started, as SQL
		since string // how long ago it was last seen, as SQL
		found bool
	}{
		{name: "fresh", age: "0 seconds", since: "0 seconds", found: true},
		{name: "seen 29 days ago", age: "40 days", since: "29 days", found: true},
		{name: "idle for 31 days", age: "40 days", since: "31 days"},
		{name: "past its absolute limit, though used today", age: "91 days", since: "0 seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDB.Reset(t)
			s := New(testDB.Pool)
			ctx := t.Context()
			u, err := s.CreateUser(ctx, "ada@example.com", "Ada")
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256([]byte("token"))
			if err := s.CreateSession(ctx, hash[:], u.ID, lifetime); err != nil {
				t.Fatal(err)
			}
			if _, err := testDB.Pool.Exec(ctx,
				`UPDATE sessions SET created_at = now() - $1::interval, expires_at = now() - $1::interval + interval '90 days',
				 last_seen_at = now() - $2::interval`, tt.age, tt.since); err != nil {
				t.Fatal(err)
			}

			sess, err := s.GetSession(ctx, hash[:], idle, time.Minute)

			switch {
			case tt.found && err != nil:
				t.Fatalf("GetSession() = %v, want the session", err)
			case tt.found && sess.UserID != u.ID:
				t.Errorf("session user = %v, want %v", sess.UserID, u.ID)
			case !tt.found && !errors.Is(err, ErrNotFound):
				t.Errorf("GetSession() = %+v, %v; want ErrNotFound", sess, err)
			}
		})
	}
}

func TestEndingSessions(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	ada, err := s.CreateUser(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	grace, err := s.CreateUser(ctx, "grace@example.com", "Grace")
	if err != nil {
		t.Fatal(err)
	}
	token := func(b byte) []byte { h := sha256.Sum256([]byte{b}); return h[:] }
	for i, u := range []User{ada, ada, ada, grace} {
		if err := s.CreateSession(ctx, token(byte(i)), u.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	alive := func(b byte) bool {
		_, err := s.GetSession(ctx, token(b), time.Hour, time.Minute)
		return err == nil
	}

	if err := s.DeleteSession(ctx, token(0)); err != nil {
		t.Fatal(err)
	}
	if alive(0) || !alive(1) {
		t.Errorf("after ending session 0: alive(0) = %v, alive(1) = %v; want false, true", alive(0), alive(1))
	}
	if err := s.DeleteUserSessions(ctx, ada.ID); err != nil {
		t.Fatal(err)
	}
	if alive(1) || alive(2) || !alive(3) {
		t.Errorf("after ending Ada's sessions: alive = %v %v %v, want false false true (Grace's)", alive(1), alive(2), alive(3))
	}
}

func TestTouchSession(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	u, err := s.CreateUser(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte("token"))
	if err := s.CreateSession(ctx, hash[:], u.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Pool.Exec(ctx, "UPDATE sessions SET last_seen_at = now() - interval '10 minutes'"); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetSession(ctx, hash[:], time.Hour, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Stale {
		t.Errorf("a session last seen 10 minutes ago is not stale at 5 minutes")
	}

	if err := s.TouchSession(ctx, hash[:]); err != nil {
		t.Fatal(err)
	}

	after, err := s.GetSession(ctx, hash[:], time.Hour, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastSeenAt.After(before.LastSeenAt.Add(9*time.Minute)) || after.Stale {
		t.Errorf("after touching: last seen %v (was %v), stale %v; want about now, not stale", after.LastSeenAt, before.LastSeenAt, after.Stale)
	}
}

// TestDeleteUser deletes a user with a document, a finished import that
// points at it, a highlight, an identity and a session, and checks that all
// of it goes while the shared cache and another user's things stay.
func TestDeleteUser(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	for _, u := range []User{f.ada, f.grace} {
		doc := f.document(t, u)
		imp, err := f.s.CreateImport(ctx, u.ID, "https://example.com/guide", "https://example.com/guide")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.FinishImport(ctx, u.ID, imp.ID, doc); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.CreateAnnotation(ctx, u.ID, doc, NewAnnotation{BlockID: f.paragraph(t, u, doc), Kind: "highlight", Selector: at(0, 2, "Gu")}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.SignIn(ctx, ghIdentity(u.Email, u.DisplayName, u.Email)); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256([]byte(u.Email))
		if err := f.s.CreateSession(ctx, hash[:], u.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.s.DeleteUser(ctx, f.ada.ID); err != nil {
		t.Fatal(err)
	}

	count := func(table string) (ada, grace int) {
		t.Helper()
		owner := "owner_id"
		if table == "user_identities" || table == "sessions" {
			owner = "user_id"
		}
		err := testDB.Pool.QueryRow(ctx,
			"SELECT count(*) FILTER (WHERE "+owner+" = $1), count(*) FILTER (WHERE "+owner+" = $2) FROM "+table,
			f.ada.ID, f.grace.ID).Scan(&ada, &grace)
		if err != nil {
			t.Fatal(err)
		}
		return ada, grace
	}
	for _, table := range []string{"documents", "imports", "annotations", "user_identities", "sessions"} {
		if ada, grace := count(table); ada != 0 || grace != 1 {
			t.Errorf("%s: Ada has %d, Grace %d; want 0 and 1", table, ada, grace)
		}
	}
	for _, table := range []string{"sources", "source_snapshots", "extractions"} {
		var n int
		if err := testDB.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d rows (%v), want the shared one kept", table, n, err)
		}
	}
	if err := f.s.DeleteUser(ctx, f.ada.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting Ada again = %v, want ErrNotFound", err)
	}
	if _, err := f.s.GetAccount(ctx, f.ada.ID, "github"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Ada's account after deletion: %v, want ErrNotFound", err)
	}
}
