// Package config loads and validates the service configuration from
// environment variables.
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Env is the deployment environment the service runs in.
type Env string

// Supported environments.
const (
	EnvDev     Env = "dev"
	EnvTest    Env = "test"
	EnvStaging Env = "staging"
	EnvProd    Env = "prod"
)

// Config is the validated service configuration.
type Config struct {
	Env         Env
	HTTPAddr    string
	LogLevel    slog.Level
	DatabaseURL string
	// FetchAllowPrivateNetworks lets the page fetcher reach loopback and
	// private addresses on any port. Only dev and test may set it.
	FetchAllowPrivateNetworks bool
	// SourceTTL is how long a fetched page stays fresh: imports within it
	// reuse the stored copy without asking the site.
	SourceTTL time.Duration
	// PublicURL is the origin people reach RoNote at, such as
	// https://ronote.app: sign-in's callback is under it.
	PublicURL string
	// GitHub is the OAuth app people sign in with.
	GitHub GitHub
}

// GitHub is an OAuth app at GitHub, or, in dev and test, at the fake
// provider (cmd/fakeidp).
type GitHub struct {
	ClientID     string
	ClientSecret string
	AuthorizeURL string
	TokenURL     string
	APIURL       string
}

// The provider URLs staging and prod must use (docs/adr/0010-sign-in-and-sessions.md).
const (
	githubAuthorizeURL = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token" //nolint:gosec // a URL, not a credential
	githubAPIURL       = "https://api.github.com"
)

const (
	defaultHTTPAddr  = "127.0.0.1:8080"
	defaultLogLevel  = "info"
	defaultSourceTTL = "24h"
)

var (
	envs      = []Env{EnvDev, EnvTest, EnvStaging, EnvProd}
	logLevels = []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
)

// Load reads the configuration through getenv, normally os.Getenv. An empty
// variable counts as unset. The returned error names every invalid variable.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	check := func(name string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}

	env, err := parseEnv(getenv("RONOTE_ENV"))
	check("RONOTE_ENV", err)
	addr := withDefault(getenv("RONOTE_HTTP_ADDR"), defaultHTTPAddr)
	check("RONOTE_HTTP_ADDR", validateAddr(addr))
	level, err := parseLogLevel(withDefault(getenv("RONOTE_LOG_LEVEL"), defaultLogLevel))
	check("RONOTE_LOG_LEVEL", err)
	dbURL := getenv("RONOTE_DATABASE_URL")
	check("RONOTE_DATABASE_URL", validateDatabaseURL(dbURL))
	allowPrivate, err := parseAllowPrivate(getenv("RONOTE_FETCH_ALLOW_PRIVATE"), env)
	check("RONOTE_FETCH_ALLOW_PRIVATE", err)
	ttl, err := parseTTL(withDefault(getenv("RONOTE_SOURCE_TTL"), defaultSourceTTL))
	check("RONOTE_SOURCE_TTL", err)
	publicURL, err := parsePublicURL(getenv("RONOTE_PUBLIC_URL"), env)
	check("RONOTE_PUBLIC_URL", err)
	gh := GitHub{
		ClientID:     getenv("RONOTE_GITHUB_CLIENT_ID"),
		ClientSecret: getenv("RONOTE_GITHUB_CLIENT_SECRET"),
		AuthorizeURL: withDefault(getenv("RONOTE_GITHUB_AUTHORIZE_URL"), githubAuthorizeURL),
		TokenURL:     withDefault(getenv("RONOTE_GITHUB_TOKEN_URL"), githubTokenURL),
		APIURL:       strings.TrimSuffix(withDefault(getenv("RONOTE_GITHUB_API_URL"), githubAPIURL), "/"),
	}
	check("RONOTE_GITHUB_CLIENT_ID", required(gh.ClientID))
	check("RONOTE_GITHUB_CLIENT_SECRET", required(gh.ClientSecret))
	check("RONOTE_GITHUB_AUTHORIZE_URL", validateProviderURL(gh.AuthorizeURL, "github.com", env))
	check("RONOTE_GITHUB_TOKEN_URL", validateProviderURL(gh.TokenURL, "github.com", env))
	check("RONOTE_GITHUB_API_URL", validateProviderURL(gh.APIURL, "api.github.com", env))

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid config:\n%w", errors.Join(errs...))
	}
	return Config{
		Env:                       env,
		HTTPAddr:                  addr,
		LogLevel:                  level,
		DatabaseURL:               dbURL,
		FetchAllowPrivateNetworks: allowPrivate,
		SourceTTL:                 ttl,
		PublicURL:                 publicURL,
		GitHub:                    gh,
	}, nil
}

// Deployed reports whether env is one people use, staging or prod, where
// cookies are Secure and only GitHub signs people in.
func (e Env) Deployed() bool {
	return e == EnvStaging || e == EnvProd
}

// parsePublicURL reads the origin RoNote is served at, http or https with
// no path; staging and prod need https, for Secure cookies.
func parsePublicURL(s string, env Env) (string, error) {
	if s == "" {
		return "", errors.New("not set; want the origin people reach RoNote at, such as https://ronote.app")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("%q is not an http or https URL", s)
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%q is not an origin: want scheme://host[:port] alone", s)
	}
	if env.Deployed() && u.Scheme != "https" {
		return "", fmt.Errorf("%q must be https when RONOTE_ENV is staging or prod", s)
	}
	return u.Scheme + "://" + u.Host, nil
}

// validateProviderURL allows any http(s) URL in dev and test, where the fake
// provider stands in, and only https on host in staging and prod.
func validateProviderURL(s, host string, env Env) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http or https URL", s)
	}
	if env.Deployed() && (u.Scheme != "https" || u.Host != host) {
		return fmt.Errorf("%q must be https://%s when RONOTE_ENV is staging or prod", s, host)
	}
	return nil
}

// required never puts the value in its error: it may be a secret.
func required(s string) error {
	if s == "" {
		return errors.New("not set")
	}
	return nil
}

// Logger returns the service logger writing to w: text in dev, JSON
// elsewhere, at the configured level.
func (c Config) Logger(w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.Env == EnvDev {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// parseTTL reads a non-negative Go duration such as "24h" or "90m".
func parseTTL(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration such as 24h or 90m", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("%q is negative", s)
	}
	return d, nil
}

// parseAllowPrivate reads a boolean that may only be true in dev and test:
// anywhere else, letting user-supplied URLs reach private networks is SSRF.
func parseAllowPrivate(s string, env Env) (bool, error) {
	if s == "" {
		return false, nil
	}
	allow, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("%q is not true or false", s)
	}
	if allow && env != EnvDev && env != EnvTest {
		return false, errors.New("may only be true when RONOTE_ENV is dev or test")
	}
	return allow, nil
}

func parseEnv(s string) (Env, error) {
	if s == "" {
		return "", fmt.Errorf("not set; want one of %s", join(envs))
	}
	if !slices.Contains(envs, Env(s)) {
		return "", notOneOf(s, envs)
	}
	return Env(s), nil
}

func validateAddr(s string) error {
	_, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("%q is not host:port", s)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%q has port %q; want 1-65535", s, port)
	}
	return nil
}

// validateDatabaseURL never puts the value in its errors: it holds the
// password.
func validateDatabaseURL(s string) error {
	if s == "" {
		return errors.New("not set; want postgres://user:password@host:port/dbname")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return errors.New("not a postgres:// or postgresql:// URL")
	}
	if u.Hostname() == "" {
		return errors.New("URL has no host")
	}
	if strings.TrimPrefix(u.Path, "/") == "" {
		return errors.New("URL has no database name")
	}
	return nil
}

func parseLogLevel(s string) (slog.Level, error) {
	names := make([]string, len(logLevels))
	for i, level := range logLevels {
		names[i] = strings.ToLower(level.String())
		if names[i] == s {
			return level, nil
		}
	}
	return 0, notOneOf(s, names)
}

func withDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func notOneOf[T ~string](value string, allowed []T) error {
	return fmt.Errorf("%q is not one of %s", value, join(allowed))
}

func join[T ~string](values []T) string {
	s := make([]string, len(values))
	for i, v := range values {
		s[i] = string(v)
	}
	return strings.Join(s, ", ")
}
