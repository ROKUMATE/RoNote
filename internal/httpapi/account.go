package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// logout ends this session.
func logout(o Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := o.Accounts.DeleteSession(r.Context(), sessionFrom(r.Context())); err != nil {
			internalError(o.Logger, w, r, err)
			return
		}
		signedOut(w, o)
	}
}

// logoutAll ends every session of the signed-in user.
func logoutAll(o Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := o.Accounts.DeleteUserSessions(r.Context(), userFrom(r.Context())); err != nil {
			internalError(o.Logger, w, r, err)
			return
		}
		signedOut(w, o)
	}
}

// deleteMe deletes the signed-in user, once they confirm with their GitHub
// login, and with them everything they own and every session.
func deleteMe(o Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var req gen.DeleteAccountRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			badBody(w, r, err, "a JSON body with confirm is required")
			return
		}
		user := userFrom(ctx)
		a, err := o.Accounts.GetAccount(ctx, user, auth.Provider)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			internalError(o.Logger, w, r, err)
			return
		}
		// GitHub logins ignore case.
		if err != nil || a.Login == "" || !strings.EqualFold(strings.TrimSpace(req.Confirm), a.Login) {
			writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidRequest, "confirm is not this account's GitHub login")
			return
		}
		if err := o.Accounts.DeleteUser(ctx, user); err != nil && !errors.Is(err, store.ErrNotFound) {
			internalError(o.Logger, w, r, err)
			return
		}
		signedOut(w, o)
	}
}

// signedOut removes the session cookie and answers 204.
func signedOut(w http.ResponseWriter, o Options) {
	http.SetCookie(w, o.Cookies.ClearSession())
	w.WriteHeader(http.StatusNoContent)
}
