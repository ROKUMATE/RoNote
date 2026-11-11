package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rokumate/ronote/internal/store"
)

// Errors from GitHub that sign-in tells apart.
var (
	// ErrDenied means GitHub refused: the user said no, or the code was
	// bad or used.
	ErrDenied = errors.New("github refused sign-in")
	// ErrUnavailable means GitHub didn't answer usefully: it timed out,
	// failed, or sent too much.
	ErrUnavailable = errors.New("github unavailable")
	// ErrNoVerifiedEmail means the user has no primary, verified email.
	ErrNoVerifiedEmail = errors.New("no primary verified email")
)

const (
	// Provider is the name identities from GitHub are stored under.
	Provider = "github"
	// scopes are what sign-in asks GitHub for.
	scopes = "read:user user:email"
	// callTimeout bounds each call to GitHub.
	callTimeout = 5 * time.Second
	// maxResponse bounds what a call to GitHub may send back.
	maxResponse = 64 << 10
)

// GitHub signs people in through a GitHub OAuth app.
type GitHub struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	// APIURL is GitHub's REST API root, https://api.github.com.
	APIURL string
	// RedirectURL is the callback, under RoNote's public URL.
	RedirectURL string
	// Client makes the calls; nil means http.DefaultClient.
	Client *http.Client
}

// AuthURL is where a browser starting flow f goes to sign in.
func (g GitHub) AuthURL(f Flow) string {
	q := url.Values{
		"client_id":             {g.ClientID},
		"redirect_uri":          {g.RedirectURL},
		"scope":                 {scopes},
		"state":                 {f.State},
		"code_challenge":        {f.Challenge()},
		"code_challenge_method": {"S256"},
	}
	return g.AuthorizeURL + "?" + q.Encode()
}

// Exchange trades the code GitHub sent back for an access token, proving
// with f's verifier that this server started the sign-in.
func (g GitHub) Exchange(ctx context.Context, code string, f Flow) (string, error) {
	form := url.Values{
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"code":          {code},
		"redirect_uri":  {g.RedirectURL},
		"code_verifier": {f.Verifier},
	}
	var res struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	err := g.call(ctx, http.MethodPost, g.TokenURL, strings.NewReader(form.Encode()), "", &res)
	switch {
	case err != nil:
		return "", err
	case res.Error != "":
		// GitHub answers a refused code with 200 and an error field.
		return "", fmt.Errorf("%w: %s", ErrDenied, res.Error)
	case res.AccessToken == "":
		return "", fmt.Errorf("%w: no access token", ErrUnavailable)
	}
	return res.AccessToken, nil
}

// Identity returns who token belongs to, with their primary, verified email.
func (g GitHub) Identity(ctx context.Context, token string) (store.Identity, error) {
	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := g.call(ctx, http.MethodGet, g.APIURL+"/user", nil, token, &user); err != nil {
		return store.Identity{}, err
	}
	if user.ID == 0 || user.Login == "" {
		return store.Identity{}, fmt.Errorf("%w: no user id or login", ErrUnavailable)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := g.call(ctx, http.MethodGet, g.APIURL+"/user/emails", nil, token, &emails); err != nil {
		return store.Identity{}, err
	}
	name := user.Name
	if name == "" {
		name = user.Login
	}
	for _, e := range emails {
		if e.Primary && e.Verified && e.Email != "" {
			return store.Identity{
				Provider: Provider, ProviderUserID: strconv.FormatInt(user.ID, 10),
				Login: user.Login, Email: e.Email, DisplayName: name,
			}, nil
		}
	}
	return store.Identity{}, ErrNoVerifiedEmail
}

// call sends one request to GitHub and decodes its JSON answer into v. Any
// failure to get a 200 with at most maxResponse bytes of JSON within
// callTimeout is ErrUnavailable.
func (g GitHub) call(ctx context.Context, method, target string, body io.Reader, token string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s %s answered %d", ErrUnavailable, method, req.URL.Path, res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if len(data) > maxResponse {
		return fmt.Errorf("%w: %s %s sent more than %d bytes", ErrUnavailable, method, req.URL.Path, maxResponse)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}
