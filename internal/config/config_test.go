package config

import (
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"
)

const day = 24 * time.Hour

// leakMarker stands in for a database password and the OAuth client secret;
// no error may contain it.
const leakMarker = "s3cret"

var testDatabaseURL = dbURL("postgres", "db.internal:5432/ronote?sslmode=disable")

// dbURL returns scheme://ronote:<leakMarker>@rest.
func dbURL(scheme, rest string) string {
	return scheme + "://ronote:" + leakMarker + "@" + rest
}

// validEnv is a complete, valid environment; cases change one thing at a time.
func validEnv(overrides map[string]string) map[string]string {
	env := map[string]string{
		"RONOTE_ENV":                  "dev",
		"RONOTE_DATABASE_URL":         testDatabaseURL,
		"RONOTE_PUBLIC_URL":           "https://ronote.test",
		"RONOTE_GITHUB_CLIENT_ID":     "client",
		"RONOTE_GITHUB_CLIENT_SECRET": leakMarker,
	}
	maps.Copy(env, overrides)
	return env
}

// github is the OAuth app validEnv configures.
var github = GitHub{
	ClientID: "client", ClientSecret: leakMarker,
	AuthorizeURL: githubAuthorizeURL, TokenURL: githubTokenURL, APIURL: githubAPIURL,
}

// fakeProvider points every provider URL at a local fake provider.
var fakeProvider = map[string]string{ //nolint:gosec // URLs, not credentials
	"RONOTE_GITHUB_AUTHORIZE_URL": "http://127.0.0.1:8090/login/oauth/authorize",
	"RONOTE_GITHUB_TOKEN_URL":     "http://127.0.0.1:8090/login/oauth/access_token",
	"RONOTE_GITHUB_API_URL":       "http://127.0.0.1:8090/",
}

// with returns env with overrides on top.
func with(env, overrides map[string]string) map[string]string {
	env = maps.Clone(env)
	maps.Copy(env, overrides)
	return env
}

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr []string
	}{
		{
			name: "defaults",
			env:  validEnv(nil),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name: "all set",
			env: validEnv(map[string]string{
				"RONOTE_ENV":          "prod",
				"RONOTE_HTTP_ADDR":    "0.0.0.0:9000",
				"RONOTE_LOG_LEVEL":    "debug",
				"RONOTE_DATABASE_URL": "postgresql://db/ronote",
			}),
			want: Config{Env: EnvProd, HTTPAddr: "0.0.0.0:9000", LogLevel: slog.LevelDebug, DatabaseURL: "postgresql://db/ronote", SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name: "empty host",
			env:  validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_HTTP_ADDR": ":8080", "RONOTE_LOG_LEVEL": "warn"}),
			want: Config{Env: EnvStaging, HTTPAddr: ":8080", LogLevel: slog.LevelWarn, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name:    "missing env",
			env:     validEnv(map[string]string{"RONOTE_ENV": ""}),
			wantErr: []string{"RONOTE_ENV: not set; want one of dev, test, staging, prod"},
		},
		{
			name:    "bad env",
			env:     validEnv(map[string]string{"RONOTE_ENV": "qa"}),
			wantErr: []string{`RONOTE_ENV: "qa" is not one of dev, test, staging, prod`},
		},
		{
			name:    "addr without port",
			env:     validEnv(map[string]string{"RONOTE_HTTP_ADDR": "localhost"}),
			wantErr: []string{`RONOTE_HTTP_ADDR: "localhost" is not host:port`},
		},
		{
			name:    "non-numeric port",
			env:     validEnv(map[string]string{"RONOTE_HTTP_ADDR": "localhost:http"}),
			wantErr: []string{`RONOTE_HTTP_ADDR: "localhost:http" has port "http"; want 1-65535`},
		},
		{
			name:    "port out of range",
			env:     validEnv(map[string]string{"RONOTE_HTTP_ADDR": "localhost:65536"}),
			wantErr: []string{`RONOTE_HTTP_ADDR: "localhost:65536" has port "65536"; want 1-65535`},
		},
		{
			name:    "port zero",
			env:     validEnv(map[string]string{"RONOTE_HTTP_ADDR": ":0"}),
			wantErr: []string{`RONOTE_HTTP_ADDR: ":0" has port "0"; want 1-65535`},
		},
		{
			name:    "bad log level",
			env:     validEnv(map[string]string{"RONOTE_LOG_LEVEL": "loud"}),
			wantErr: []string{`RONOTE_LOG_LEVEL: "loud" is not one of debug, info, warn, error`},
		},
		{
			name:    "missing database URL",
			env:     validEnv(map[string]string{"RONOTE_DATABASE_URL": ""}),
			wantErr: []string{"RONOTE_DATABASE_URL: not set; want postgres://user:password@host:port/dbname"},
		},
		{
			name:    "database URL with another scheme",
			env:     validEnv(map[string]string{"RONOTE_DATABASE_URL": dbURL("mysql", "db/ronote")}),
			wantErr: []string{"RONOTE_DATABASE_URL: not a postgres:// or postgresql:// URL"},
		},
		{
			name:    "database URL in keyword/value form",
			env:     validEnv(map[string]string{"RONOTE_DATABASE_URL": "host=db user=ronote password=" + leakMarker + " dbname=ronote"}),
			wantErr: []string{"RONOTE_DATABASE_URL: not a postgres:// or postgresql:// URL"},
		},
		{
			name:    "database URL without host",
			env:     validEnv(map[string]string{"RONOTE_DATABASE_URL": dbURL("postgres", "/ronote")}),
			wantErr: []string{"RONOTE_DATABASE_URL: URL has no host"},
		},
		{
			name:    "database URL without database name",
			env:     validEnv(map[string]string{"RONOTE_DATABASE_URL": dbURL("postgres", "db:5432/")}),
			wantErr: []string{"RONOTE_DATABASE_URL: URL has no database name"},
		},
		{
			name: "private fetches allowed in dev",
			env:  validEnv(map[string]string{"RONOTE_FETCH_ALLOW_PRIVATE": "true"}),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, FetchAllowPrivateNetworks: true, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name: "private fetches allowed in test",
			env:  validEnv(map[string]string{"RONOTE_ENV": "test", "RONOTE_FETCH_ALLOW_PRIVATE": "1"}),
			want: Config{Env: EnvTest, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, FetchAllowPrivateNetworks: true, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name: "private fetches off in prod",
			env:  validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_FETCH_ALLOW_PRIVATE": "false"}),
			want: Config{Env: EnvProd, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name:    "private fetches refused in prod",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_FETCH_ALLOW_PRIVATE": "true"}),
			wantErr: []string{"RONOTE_FETCH_ALLOW_PRIVATE: may only be true when RONOTE_ENV is dev or test"},
		},
		{
			name:    "private fetches refused in staging",
			env:     validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_FETCH_ALLOW_PRIVATE": "true"}),
			wantErr: []string{"RONOTE_FETCH_ALLOW_PRIVATE: may only be true when RONOTE_ENV is dev or test"},
		},
		{
			name:    "private fetches not a boolean",
			env:     validEnv(map[string]string{"RONOTE_FETCH_ALLOW_PRIVATE": "yes"}),
			wantErr: []string{`RONOTE_FETCH_ALLOW_PRIVATE: "yes" is not true or false`},
		},
		{
			name: "source TTL",
			env:  validEnv(map[string]string{"RONOTE_SOURCE_TTL": "90m"}),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: 90 * time.Minute, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name: "source TTL of zero always revalidates",
			env:  validEnv(map[string]string{"RONOTE_SOURCE_TTL": "0s"}),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name:    "source TTL not a duration",
			env:     validEnv(map[string]string{"RONOTE_SOURCE_TTL": "1 day"}),
			wantErr: []string{`RONOTE_SOURCE_TTL: "1 day" is not a duration such as 24h or 90m`},
		},
		{
			name:    "negative source TTL",
			env:     validEnv(map[string]string{"RONOTE_SOURCE_TTL": "-1h"}),
			wantErr: []string{`RONOTE_SOURCE_TTL: "-1h" is negative`},
		},
		{
			name: "dev user",
			env:  validEnv(map[string]string{"RONOTE_DEV_USER_EMAIL": " dev@ronote.local "}),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, DevUserEmail: "dev@ronote.local", PublicURL: "https://ronote.test", GitHub: github},
		},
		{
			name:    "dev user refused in prod",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_DEV_USER_EMAIL": "dev@ronote.local"}),
			wantErr: []string{"RONOTE_DEV_USER_EMAIL: may only be set when RONOTE_ENV is dev or test"},
		},
		{
			name:    "dev user refused in staging",
			env:     validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_DEV_USER_EMAIL": "dev@ronote.local"}),
			wantErr: []string{"RONOTE_DEV_USER_EMAIL: may only be set when RONOTE_ENV is dev or test"},
		},
		{
			name:    "dev user not an email",
			env:     validEnv(map[string]string{"RONOTE_DEV_USER_EMAIL": "dev"}),
			wantErr: []string{`RONOTE_DEV_USER_EMAIL: "dev" is not an email address`},
		},
		{
			name: "the fake provider in dev",
			env:  validEnv(fakeProvider),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test",
				GitHub: GitHub{
					ClientID: "client", ClientSecret: leakMarker,
					AuthorizeURL: fakeProvider["RONOTE_GITHUB_AUTHORIZE_URL"], TokenURL: fakeProvider["RONOTE_GITHUB_TOKEN_URL"],
					APIURL: "http://127.0.0.1:8090",
				},
			},
		},
		{
			name: "the fake provider refused in prod",
			env:  validEnv(with(fakeProvider, map[string]string{"RONOTE_ENV": "prod"})),
			wantErr: []string{
				`RONOTE_GITHUB_AUTHORIZE_URL: "http://127.0.0.1:8090/login/oauth/authorize" must be https://github.com when RONOTE_ENV is staging or prod`,
				`RONOTE_GITHUB_TOKEN_URL: "http://127.0.0.1:8090/login/oauth/access_token" must be https://github.com`,
				`RONOTE_GITHUB_API_URL: "http://127.0.0.1:8090" must be https://api.github.com`,
			},
		},
		{
			name:    "another host refused in staging, even over https",
			env:     validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_GITHUB_TOKEN_URL": "https://github.com.evil.test/login/oauth/access_token"}), //nolint:gosec // a URL, not a credential
			wantErr: []string{"RONOTE_GITHUB_TOKEN_URL:", "must be https://github.com"},
		},
		{
			name:    "missing OAuth app",
			env:     validEnv(map[string]string{"RONOTE_GITHUB_CLIENT_ID": "", "RONOTE_GITHUB_CLIENT_SECRET": ""}),
			wantErr: []string{"RONOTE_GITHUB_CLIENT_ID: not set", "RONOTE_GITHUB_CLIENT_SECRET: not set"},
		},
		{
			name: "public URL with a trailing slash",
			env:  validEnv(map[string]string{"RONOTE_PUBLIC_URL": "http://127.0.0.1:5173/"}),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "http://127.0.0.1:5173", GitHub: github},
		},
		{
			name:    "missing public URL",
			env:     validEnv(map[string]string{"RONOTE_PUBLIC_URL": ""}),
			wantErr: []string{"RONOTE_PUBLIC_URL: not set"},
		},
		{
			name:    "public URL with a path",
			env:     validEnv(map[string]string{"RONOTE_PUBLIC_URL": "https://ronote.test/app"}),
			wantErr: []string{`RONOTE_PUBLIC_URL: "https://ronote.test/app" is not an origin`},
		},
		{
			name:    "plain http public URL refused in prod",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_PUBLIC_URL": "http://ronote.test"}),
			wantErr: []string{`RONOTE_PUBLIC_URL: "http://ronote.test" must be https`},
		},
		{
			name: "every bad variable reported",
			env: map[string]string{
				"RONOTE_ENV":          "qa",
				"RONOTE_HTTP_ADDR":    "nope",
				"RONOTE_LOG_LEVEL":    "loud",
				"RONOTE_DATABASE_URL": dbURL("mysql", "db/ronote"),
			},
			wantErr: []string{
				"RONOTE_ENV:",
				"RONOTE_HTTP_ADDR:",
				"RONOTE_LOG_LEVEL:",
				"RONOTE_DATABASE_URL:",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(func(key string) string { return tt.env[key] })

			if len(tt.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Load() error = %v, want nil", err)
				}
				if got != tt.want {
					t.Errorf("Load() = %+v, want %+v", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("Load() = %+v, want error containing %q", got, tt.wantErr)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Load() error = %q, want it to contain %q", err, want)
				}
			}
			if strings.Contains(err.Error(), leakMarker) {
				t.Errorf("Load() error = %q leaks a secret", err)
			}
		})
	}
}

func TestLogger(t *testing.T) {
	t.Parallel()
	var text, js strings.Builder
	Config{Env: EnvDev, LogLevel: slog.LevelInfo}.Logger(&text).Info("hello", "n", 1)
	Config{Env: EnvProd, LogLevel: slog.LevelWarn}.Logger(&js).Info("hidden")
	Config{Env: EnvProd, LogLevel: slog.LevelWarn}.Logger(&js).Warn("shown")
	if !strings.Contains(text.String(), "msg=hello n=1") {
		t.Errorf("dev log = %q, want text", text.String())
	}
	if strings.Contains(js.String(), "hidden") || !strings.Contains(js.String(), `"msg":"shown"`) {
		t.Errorf("prod log = %q, want JSON at warn and above", js.String())
	}
}
