package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

type userKey struct{}

// requireSession makes the user of the request's session cookie the
// signed-in user, or answers 401 when there is no live session. It records
// the session's use at most once per auth.TouchInterval.
func requireSession(o Options, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		token, ok := o.Cookies.SessionToken(r)
		hash, valid := auth.HashToken(token)
		if !ok || !valid {
			writeError(w, r, http.StatusUnauthorized, gen.ErrorCodeUnauthenticated, "sign-in required")
			return
		}
		sess, err := o.Accounts.GetSession(ctx, hash, auth.SessionIdle, auth.TouchInterval)
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusUnauthorized, gen.ErrorCodeUnauthenticated, "sign-in required")
			return
		case err != nil:
			internalError(o.Logger, w, r, err)
			return
		}
		if sess.Stale {
			// A missed touch costs at most an early idle expiry; the
			// request itself is fine.
			if err := o.Accounts.TouchSession(ctx, hash); err != nil {
				o.Logger.LogAttrs(ctx, slog.LevelWarn, "touch session",
					slog.String("error", err.Error()), slog.String("request_id", requestID(ctx)))
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userKey{}, sess.UserID)))
	})
}

// userFrom returns the signed-in user requireSession stored in ctx.
func userFrom(ctx context.Context) uuid.UUID {
	user, _ := ctx.Value(userKey{}).(uuid.UUID)
	return user
}
