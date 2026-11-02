// Package config loads and validates the service configuration from
// environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
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
}

const (
	defaultHTTPAddr = "127.0.0.1:8080"
	defaultLogLevel = "info"
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

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid config:\n%w", errors.Join(errs...))
	}
	return Config{
		Env:                       env,
		HTTPAddr:                  addr,
		LogLevel:                  level,
		DatabaseURL:               dbURL,
		FetchAllowPrivateNetworks: allowPrivate,
	}, nil
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
