package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/httpapi/gen"
)

type userKey struct{}

// requireUser makes user the signed-in user of every request, or answers
// 401 when there is none. Real sign-in replaces it in P16.
func requireUser(user uuid.UUID, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user == uuid.Nil {
			writeError(w, r, http.StatusUnauthorized, gen.ErrorCodeUnauthenticated, "sign-in required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}

// userFrom returns the signed-in user requireUser stored in ctx.
func userFrom(ctx context.Context) uuid.UUID {
	user, _ := ctx.Value(userKey{}).(uuid.UUID)
	return user
}
