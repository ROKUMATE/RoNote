package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// Accounts is where sign-in keeps users and sessions. *store.Store
// implements it; it is an interface so handler tests can run without a
// database.
type Accounts interface {
	SignIn(ctx context.Context, id store.Identity) (store.User, error)
	GetAccount(ctx context.Context, user uuid.UUID, provider string) (store.Account, error)
	CreateSession(ctx context.Context, hash []byte, user uuid.UUID, lifetime time.Duration) error
	GetSession(ctx context.Context, hash []byte, idle, touch time.Duration) (store.Session, error)
	TouchSession(ctx context.Context, hash []byte) error
	DeleteSession(ctx context.Context, hash []byte) error
}

// signInStart sends the browser to GitHub, keeping the sign-in's state, PKCE
// verifier and return_to in a cookie for the callback.
func signInStart(s Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flow := auth.NewFlow(r.URL.Query().Get("return_to"), time.Now())
		http.SetCookie(w, s.Cookies.Flow(flow))
		http.Redirect(w, r, s.GitHub.AuthURL(flow), http.StatusFound) //nolint:gosec // the configured provider; return_to stays in the cookie
	}
}

// signInCallback finishes a sign-in GitHub sent the browser back from, and
// starts a session.
func signInCallback(s Options) http.HandlerFunc {
	logger := s.Logger
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		q := r.URL.Query()
		http.SetCookie(w, s.Cookies.ClearFlow())
		flow, err := s.Cookies.ReadFlow(r, time.Now())
		fail := func(code gen.SignInErrorCode, cause error) {
			logger.LogAttrs(ctx, slog.LevelInfo, "sign-in failed",
				slog.String("code", string(code)), slog.String("error", cause.Error()),
				slog.String("request_id", requestID(ctx)))
			back := url.Values{"error": {string(code)}}
			if flow.ReturnTo != "" && flow.ReturnTo != "/" {
				back.Set("return_to", flow.ReturnTo)
			}
			http.Redirect(w, r, "/login?"+back.Encode(), http.StatusSeeOther)
		}
		switch {
		case err != nil:
			fail(gen.StateMismatch, err)
			return
		case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(flow.State)) != 1:
			fail(gen.StateMismatch, errors.New("the state is not the one this browser started with"))
			return
		case q.Get("error") != "":
			fail(gen.Denied, errors.New("github: "+q.Get("error")))
			return
		}

		token, err := s.GitHub.Exchange(ctx, q.Get("code"), flow)
		var id store.Identity
		if err == nil {
			id, err = s.GitHub.Identity(ctx, token)
		}
		switch {
		case errors.Is(err, auth.ErrDenied):
			fail(gen.Denied, err)
			return
		case errors.Is(err, auth.ErrNoVerifiedEmail):
			fail(gen.NoVerifiedEmail, err)
			return
		case err != nil:
			fail(gen.ProviderUnavailable, err)
			return
		}

		user, err := s.Accounts.SignIn(ctx, id)
		if err != nil {
			internalError(logger, w, r, err)
			return
		}
		// A sign-in always starts a new session, and ends the one this
		// browser came with, so no token from before it stays good.
		if old, ok := s.Cookies.SessionToken(r); ok {
			if hash, ok := auth.HashToken(old); ok {
				if err := s.Accounts.DeleteSession(ctx, hash); err != nil {
					internalError(logger, w, r, err)
					return
				}
			}
		}
		session, hash := auth.NewSessionToken()
		if err := s.Accounts.CreateSession(ctx, hash, user.ID, auth.SessionLifetime); err != nil {
			internalError(logger, w, r, err)
			return
		}
		http.SetCookie(w, s.Cookies.Session(session))
		http.Redirect(w, r, flow.ReturnTo, http.StatusSeeOther)
	}
}

// internalError logs err and answers with the 500 envelope.
func internalError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	logger.LogAttrs(r.Context(), slog.LevelError, "api error",
		slog.String("error", err.Error()), slog.String("request_id", requestID(r.Context())))
	writeError(w, r, http.StatusInternalServerError, gen.ErrorCodeInternalError, "internal server error")
}

func (s apiServer) GetMe(ctx context.Context, _ gen.GetMeRequestObject) (gen.GetMeResponseObject, error) {
	a, err := s.accounts.GetAccount(ctx, userFrom(ctx), auth.Provider)
	if errors.Is(err, store.ErrNotFound) {
		// The user went between the session check and now.
		return gen.GetMe401JSONResponse{UnauthenticatedJSONResponse: gen.UnauthenticatedJSONResponse(apiError(ctx, gen.ErrorCodeUnauthenticated, "sign-in required"))}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.GetMe200JSONResponse{ID: a.ID, DisplayName: a.DisplayName, Email: a.Email, Login: a.Login}, nil
}
