package config

import (
	"log/slog"
	"strings"
	"testing"
)

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
			env:  map[string]string{"RONOTE_ENV": "dev"},
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo},
		},
		{
			name: "all set",
			env: map[string]string{
				"RONOTE_ENV":       "prod",
				"RONOTE_HTTP_ADDR": "0.0.0.0:9000",
				"RONOTE_LOG_LEVEL": "debug",
			},
			want: Config{Env: EnvProd, HTTPAddr: "0.0.0.0:9000", LogLevel: slog.LevelDebug},
		},
		{
			name: "empty host",
			env:  map[string]string{"RONOTE_ENV": "staging", "RONOTE_HTTP_ADDR": ":8080", "RONOTE_LOG_LEVEL": "warn"},
			want: Config{Env: EnvStaging, HTTPAddr: ":8080", LogLevel: slog.LevelWarn},
		},
		{
			name:    "missing env",
			env:     map[string]string{},
			wantErr: []string{"RONOTE_ENV: not set; want one of dev, test, staging, prod"},
		},
		{
			name:    "bad env",
			env:     map[string]string{"RONOTE_ENV": "qa"},
			wantErr: []string{`RONOTE_ENV: "qa" is not one of dev, test, staging, prod`},
		},
		{
			name:    "addr without port",
			env:     map[string]string{"RONOTE_ENV": "test", "RONOTE_HTTP_ADDR": "localhost"},
			wantErr: []string{`RONOTE_HTTP_ADDR: "localhost" is not host:port`},
		},
		{
			name:    "non-numeric port",
			env:     map[string]string{"RONOTE_ENV": "test", "RONOTE_HTTP_ADDR": "localhost:http"},
			wantErr: []string{`RONOTE_HTTP_ADDR: "localhost:http" has port "http"; want 1-65535`},
		},
		{
			name:    "port out of range",
			env:     map[string]string{"RONOTE_ENV": "test", "RONOTE_HTTP_ADDR": "localhost:65536"},
			wantErr: []string{`RONOTE_HTTP_ADDR: "localhost:65536" has port "65536"; want 1-65535`},
		},
		{
			name:    "port zero",
			env:     map[string]string{"RONOTE_ENV": "test", "RONOTE_HTTP_ADDR": ":0"},
			wantErr: []string{`RONOTE_HTTP_ADDR: ":0" has port "0"; want 1-65535`},
		},
		{
			name:    "bad log level",
			env:     map[string]string{"RONOTE_ENV": "test", "RONOTE_LOG_LEVEL": "loud"},
			wantErr: []string{`RONOTE_LOG_LEVEL: "loud" is not one of debug, info, warn, error`},
		},
		{
			name: "every bad variable reported",
			env:  map[string]string{"RONOTE_ENV": "qa", "RONOTE_HTTP_ADDR": "nope", "RONOTE_LOG_LEVEL": "loud"},
			wantErr: []string{
				"RONOTE_ENV:",
				"RONOTE_HTTP_ADDR:",
				"RONOTE_LOG_LEVEL:",
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
		})
	}
}
