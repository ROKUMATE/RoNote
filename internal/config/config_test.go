package config

import (
	"log/slog"
	"maps"
	"strings"
	"testing"
)

// leakMarker stands in for a database password; no error may contain it.
const leakMarker = "s3cret"

var testDatabaseURL = dbURL("postgres", "db.internal:5432/ronote?sslmode=disable")

// dbURL returns scheme://ronote:<leakMarker>@rest.
func dbURL(scheme, rest string) string {
	return scheme + "://ronote:" + leakMarker + "@" + rest
}

// validEnv is a complete, valid environment; cases change one thing at a time.
func validEnv(overrides map[string]string) map[string]string {
	env := map[string]string{"RONOTE_ENV": "dev", "RONOTE_DATABASE_URL": testDatabaseURL}
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
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL},
		},
		{
			name: "all set",
			env: validEnv(map[string]string{
				"RONOTE_ENV":          "prod",
				"RONOTE_HTTP_ADDR":    "0.0.0.0:9000",
				"RONOTE_LOG_LEVEL":    "debug",
				"RONOTE_DATABASE_URL": "postgresql://db/ronote",
			}),
			want: Config{Env: EnvProd, HTTPAddr: "0.0.0.0:9000", LogLevel: slog.LevelDebug, DatabaseURL: "postgresql://db/ronote"},
		},
		{
			name: "empty host",
			env:  validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_HTTP_ADDR": ":8080", "RONOTE_LOG_LEVEL": "warn"}),
			want: Config{Env: EnvStaging, HTTPAddr: ":8080", LogLevel: slog.LevelWarn, DatabaseURL: testDatabaseURL},
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
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, FetchAllowPrivateNetworks: true},
		},
		{
			name: "private fetches allowed in test",
			env:  validEnv(map[string]string{"RONOTE_ENV": "test", "RONOTE_FETCH_ALLOW_PRIVATE": "1"}),
			want: Config{Env: EnvTest, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, FetchAllowPrivateNetworks: true},
		},
		{
			name: "private fetches off in prod",
			env:  validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_FETCH_ALLOW_PRIVATE": "false"}),
			want: Config{Env: EnvProd, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL},
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
				t.Errorf("Load() error = %q leaks the database password", err)
			}
		})
	}
}
