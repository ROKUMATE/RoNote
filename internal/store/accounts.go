package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// Identity is who a provider says signed in.
type Identity struct {
	Provider       string
	ProviderUserID string
	Login          string
	// Email is the user's primary email, which the provider reports
	// verified.
	Email       string
	DisplayName string
}

// Account is a user with their login at one provider.
type Account = sqlcdb.GetAccountRow

// Session is a signed-in browser's user, and when it was last seen.
type Session = sqlcdb.GetSessionRow

// SignIn returns the user of id, refreshing its login. An identity seen for
// the first time joins the user whose email is id's verified email, or
// creates one (docs/adr/0010-sign-in-and-sessions.md).
func (s *Store) SignIn(ctx context.Context, id Identity) (User, error) {
	var u User
	err := s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		var err error
		u, err = tx.signIn(ctx, id)
		return err
	})
	return u, err
}

func (s *Store) signIn(ctx context.Context, id Identity) (User, error) {
	known, err := s.q.GetIdentity(ctx, sqlcdb.GetIdentityParams{Provider: id.Provider, ProviderUserID: id.ProviderUserID})
	switch {
	case err == nil:
		if err := s.q.SetIdentityLogin(ctx, sqlcdb.SetIdentityLoginParams{
			Provider: id.Provider, ProviderUserID: id.ProviderUserID, Login: id.Login,
		}); err != nil {
			return User{}, mapErr("set login", err)
		}
		u, err := s.q.GetUser(ctx, known.UserID)
		if err != nil {
			return User{}, mapErr("get user", err)
		}
		return u, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return User{}, mapErr("get identity", err)
	}

	u, err := s.q.GetUserByEmail(ctx, id.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		u, err = s.q.CreateUser(ctx, sqlcdb.CreateUserParams{Email: id.Email, DisplayName: id.DisplayName})
	}
	if err != nil {
		return User{}, mapErr("sign in", err)
	}
	if err := s.q.CreateIdentity(ctx, sqlcdb.CreateIdentityParams{
		Provider: id.Provider, ProviderUserID: id.ProviderUserID, UserID: u.ID, Login: id.Login,
	}); err != nil {
		return User{}, mapErr("create identity", err)
	}
	return u, nil
}

// GetAccount returns user with their login at provider, or ErrNotFound.
func (s *Store) GetAccount(ctx context.Context, user uuid.UUID, provider string) (Account, error) {
	a, err := s.q.GetAccount(ctx, sqlcdb.GetAccountParams{ID: user, Provider: provider})
	if err != nil {
		return Account{}, mapErr("get account", err)
	}
	return a, nil
}

// DeleteUser deletes user and everything they own: documents, imports,
// annotations, identities and sessions. Sources, snapshots and extractions
// are shared and stay. It returns ErrNotFound when there is no such user.
func (s *Store) DeleteUser(ctx context.Context, user uuid.UUID) error {
	n, err := s.q.DeleteUser(ctx, user)
	if err != nil {
		return mapErr("delete user", err)
	}
	if n == 0 {
		return fmt.Errorf("delete user: %w", ErrNotFound)
	}
	return nil
}

// CreateSession starts a session for user under the token whose SHA-256 is
// hash, ending lifetime from now at the latest.
func (s *Store) CreateSession(ctx context.Context, hash []byte, user uuid.UUID, lifetime time.Duration) error {
	err := s.q.CreateSession(ctx, sqlcdb.CreateSessionParams{TokenHash: hash, UserID: user, LifetimeSeconds: lifetime.Seconds()})
	if err != nil {
		return mapErr("create session", err)
	}
	return nil
}

// GetSession returns the session whose token hashes to hash, or ErrNotFound
// when there is none, it has expired, or it went unused for longer than
// idle.
func (s *Store) GetSession(ctx context.Context, hash []byte, idle time.Duration) (Session, error) {
	sess, err := s.q.GetSession(ctx, sqlcdb.GetSessionParams{TokenHash: hash, IdleSeconds: idle.Seconds()})
	if err != nil {
		return Session{}, mapErr("get session", err)
	}
	return sess, nil
}

// TouchSession records that the session was used now.
func (s *Store) TouchSession(ctx context.Context, hash []byte) error {
	if err := s.q.TouchSession(ctx, hash); err != nil {
		return mapErr("touch session", err)
	}
	return nil
}

// DeleteSession ends one session. Ending one that doesn't exist is no error.
func (s *Store) DeleteSession(ctx context.Context, hash []byte) error {
	if err := s.q.DeleteSession(ctx, hash); err != nil {
		return mapErr("delete session", err)
	}
	return nil
}

// DeleteUserSessions ends every session of user.
func (s *Store) DeleteUserSessions(ctx context.Context, user uuid.UUID) error {
	if err := s.q.DeleteUserSessions(ctx, user); err != nil {
		return mapErr("delete sessions", err)
	}
	return nil
}
