package config

import (
	"log/slog"
	"maps"
	"net/netip"
	"reflect"
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
		"RONOTE_RUNNER_URL":           "https://ronote-run.test",
		"RONOTE_TRUSTED_PROXIES":      "10.89.10.0/24",
		"RONOTE_GITHUB_CLIENT_ID":     "client",
		"RONOTE_GITHUB_CLIENT_SECRET": leakMarker,
		"RONOTE_ANTHROPIC_API_KEY":    leakMarker,
		"RONOTE_BLOB_ENDPOINT":        "https://s3.example.com",
		"RONOTE_BLOB_REGION":          "eu-central-1",
		"RONOTE_BLOB_BUCKET":          "ronote-pdfs",
		"RONOTE_BLOB_ACCESS_KEY":      "access",
		"RONOTE_BLOB_SECRET_KEY":      leakMarker,
		"RONOTE_DOCLING_URL":          "http://docling.internal:5001",
		"RONOTE_DOCLING_API_KEY":      leakMarker,
		"RONOTE_PDFTEXT_URL":          "http://pdftext.internal:5002",
		"RONOTE_PDFTEXT_API_KEY":      leakMarker,
		"RONOTE_EMBED_QUERY_URL":      "http://127.0.0.1:11434",
	}
	maps.Copy(env, overrides)
	return env
}

// blobSet and pdfSet are the blob store and converters validEnv names, and
// runnerSet and proxiesSet its runner and proxies; a case that leaves them
// out of its want expects them.
var (
	blobSet = Blob{Endpoint: "https://s3.example.com", Region: "eu-central-1", Bucket: "ronote-pdfs", AccessKey: "access", SecretKey: leakMarker}
	pdfSet  = PDF{DoclingURL: "http://docling.internal:5001", DoclingKey: leakMarker, TextURL: "http://pdftext.internal:5002", TextKey: leakMarker}

	runnerSet  = "https://ronote-run.test"
	proxiesSet = []netip.Prefix{netip.MustParsePrefix("10.89.10.0/24")}
)

// noPDFSettings unsets every blob store and converter variable.
var noPDFSettings = map[string]string{
	"RONOTE_BLOB_ENDPOINT": "", "RONOTE_BLOB_REGION": "", "RONOTE_BLOB_BUCKET": "", "RONOTE_BLOB_ACCESS_KEY": "", "RONOTE_BLOB_SECRET_KEY": "",
	"RONOTE_DOCLING_URL": "", "RONOTE_DOCLING_API_KEY": "", "RONOTE_PDFTEXT_URL": "", "RONOTE_PDFTEXT_API_KEY": "",
}

// github is the OAuth app validEnv configures.
var github = GitHub{
	ClientID: "client", ClientSecret: leakMarker,
	AuthorizeURL: githubAuthorizeURL, TokenURL: githubTokenURL, APIURL: githubAPIURL,
}

// embedDefault is the default embedder: Ollama's, with the pinned model.
var embedDefault = Embed{URL: "http://127.0.0.1:11434", QueryURL: "http://127.0.0.1:11434", Model: "qwen3-embedding:0.6b"}

// limits are the default limits.
var limits = Limits{ImportsPerDay: 60, Documents: 500, APIPerMinute: 120, AuthPerMinute: 20, ChatMessagesPerDay: 50, ChatTokensPerDay: 400_000}

// chatDefault is the default model: Haiku through Anthropic's API, with the
// key validEnv sets.
var chatDefault = Chat{Enabled: true, URL: "https://api.anthropic.com", Model: "claude-haiku-5-5", APIKey: leakMarker, DailySpendMicroUSD: 500_000}

// chatOffDefault is what staging and prod get unless chat is turned on by
// name: the same model, off until it has run against the real one.
var chatOffDefault = Chat{URL: "https://api.anthropic.com", Model: "claude-haiku-5-5", APIKey: leakMarker, DailySpendMicroUSD: 500_000}

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
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault},
		},
		{
			name: "all set",
			env: validEnv(map[string]string{
				"RONOTE_ENV":          "prod",
				"RONOTE_HTTP_ADDR":    "0.0.0.0:9000",
				"RONOTE_LOG_LEVEL":    "debug",
				"RONOTE_DATABASE_URL": "postgresql://db/ronote",
			}),
			want: Config{Env: EnvProd, HTTPAddr: "0.0.0.0:9000", LogLevel: slog.LevelDebug, DatabaseURL: "postgresql://db/ronote", SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatOffDefault},
		},
		{
			name: "empty host",
			env:  validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_HTTP_ADDR": ":8080", "RONOTE_LOG_LEVEL": "warn"}),
			want: Config{Env: EnvStaging, HTTPAddr: ":8080", LogLevel: slog.LevelWarn, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatOffDefault},
		},
		{
			name: "trusted proxies",
			env:  validEnv(map[string]string{"RONOTE_TRUSTED_PROXIES": "10.89.10.0/24, 192.0.2.7 fd00:89::/64"}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault,
				TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.89.10.0/24"), netip.MustParsePrefix("192.0.2.7/32"), netip.MustParsePrefix("fd00:89::/64")},
			},
		},
		{
			name:    "a proxy that is no address",
			env:     validEnv(map[string]string{"RONOTE_TRUSTED_PROXIES": "10.89.10.0/33,caddy"}),
			wantErr: []string{`RONOTE_TRUSTED_PROXIES: "10.89.10.0/33" is not a CIDR or an IP`, `RONOTE_TRUSTED_PROXIES: "caddy" is not a CIDR or an IP`},
		},
		{
			name:    "a proxy network with host bits",
			env:     validEnv(map[string]string{"RONOTE_TRUSTED_PROXIES": "10.89.10.5/24"}),
			wantErr: []string{`RONOTE_TRUSTED_PROXIES: "10.89.10.5/24" has host bits set; want 10.89.10.0/24`},
		},
		{
			name: "a daily chat budget",
			env:  validEnv(map[string]string{"RONOTE_CHAT_DAILY_USD": "2.25"}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault,
				Chat: Chat{Enabled: true, URL: "https://api.anthropic.com", Model: "claude-haiku-5-5", APIKey: leakMarker, DailySpendMicroUSD: 2_250_000},
			},
		},
		{
			name:    "a daily chat budget that isn't one",
			env:     validEnv(map[string]string{"RONOTE_CHAT_DAILY_USD": "0"}),
			wantErr: []string{`RONOTE_CHAT_DAILY_USD: "0" is not an amount of dollars above 0, such as 0.50`},
		},
		{
			name:    "a daily chat budget in other words",
			env:     validEnv(map[string]string{"RONOTE_CHAT_DAILY_USD": "$1"}),
			wantErr: []string{`RONOTE_CHAT_DAILY_USD: "$1" is not an amount of dollars above 0, such as 0.50`},
		},
		{
			name: "dev's runner by default",
			env:  validEnv(map[string]string{"RONOTE_RUNNER_URL": "", "RONOTE_TRUSTED_PROXIES": ""}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault,
				RunnerURL: "http://localhost:5174", TrustedProxies: []netip.Prefix{},
			},
		},
		{
			name:    "prod names its runner and its proxies",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_RUNNER_URL": "", "RONOTE_TRUSTED_PROXIES": ""}),
			wantErr: []string{"RONOTE_RUNNER_URL: not set; want the origin code from documents runs on, such as https://ronote-run.dev", "RONOTE_TRUSTED_PROXIES: not set; staging and prod run behind Caddy, whose network it names"},
		},
		{
			name:    "prod's runner over https",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_RUNNER_URL": "http://ronote-run.test"}),
			wantErr: []string{`RONOTE_RUNNER_URL: "http://ronote-run.test" must be https when RONOTE_ENV is staging or prod`},
		},
		{
			name:    "prod's runner on a subdomain of the app's site",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_PUBLIC_URL": "https://app.ronote.co.uk", "RONOTE_RUNNER_URL": "https://run.ronote.co.uk"}),
			wantErr: []string{`RONOTE_RUNNER_URL: https://run.ronote.co.uk is on the same site as RONOTE_PUBLIC_URL (ronote.co.uk); the runner needs a registrable domain of its own (docs/adr/0008-in-browser-code-execution.md)`},
		},
		{
			name: "staging's runner on another site under a shared suffix",
			env:  validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_PUBLIC_URL": "https://ronote.github.io", "RONOTE_RUNNER_URL": "https://ronote-run.github.io"}),
			want: Config{Env: EnvStaging, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.github.io", RunnerURL: "https://ronote-run.github.io", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatOffDefault},
		},
		{
			// docs/adr/0015-operations.md: pages.dev is on the Public Suffix
			// List, so each Pages project is a site of its own.
			name: "staging's app on a .me domain and its runner on Cloudflare Pages",
			env:  validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_PUBLIC_URL": "https://ronote.example.me", "RONOTE_RUNNER_URL": "https://ronote-runner.pages.dev"}),
			want: Config{Env: EnvStaging, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.example.me", RunnerURL: "https://ronote-runner.pages.dev", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatOffDefault},
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
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, FetchAllowPrivateNetworks: true, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault},
		},
		{
			name: "private fetches allowed in test",
			env:  validEnv(map[string]string{"RONOTE_ENV": "test", "RONOTE_FETCH_ALLOW_PRIVATE": "1"}),
			want: Config{Env: EnvTest, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, FetchAllowPrivateNetworks: true, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault},
		},
		{
			name: "private fetches off in prod",
			env:  validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_FETCH_ALLOW_PRIVATE": "false"}),
			want: Config{Env: EnvProd, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatOffDefault},
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
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: 90 * time.Minute, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault},
		},
		{
			name: "source TTL of zero always revalidates",
			env:  validEnv(map[string]string{"RONOTE_SOURCE_TTL": "0s"}),
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault},
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
				Limits: limits, Embed: embedDefault, Chat: chatDefault,
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
			want: Config{Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day, PublicURL: "http://127.0.0.1:5173", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault},
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
			name: "limits",
			env: validEnv(map[string]string{
				"RONOTE_LIMIT_IMPORTS_PER_DAY": "10", "RONOTE_LIMIT_DOCUMENTS": "20",
				"RONOTE_LIMIT_API_PER_MINUTE": "30", "RONOTE_LIMIT_AUTH_PER_MINUTE": "40",
				"RONOTE_LIMIT_CHAT_MESSAGES_PER_DAY": "5", "RONOTE_LIMIT_CHAT_TOKENS_PER_DAY": "6000",
			}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github,
				Limits: Limits{ImportsPerDay: 10, Documents: 20, APIPerMinute: 30, AuthPerMinute: 40, ChatMessagesPerDay: 5, ChatTokensPerDay: 6000},
				Embed:  embedDefault, Chat: chatDefault,
			},
		},
		{
			name:    "a limit of zero",
			env:     validEnv(map[string]string{"RONOTE_LIMIT_DOCUMENTS": "0"}),
			wantErr: []string{`RONOTE_LIMIT_DOCUMENTS: "0" is not a whole number of at least 1`},
		},
		{
			name:    "a limit that isn't a number",
			env:     validEnv(map[string]string{"RONOTE_LIMIT_API_PER_MINUTE": "lots"}),
			wantErr: []string{`RONOTE_LIMIT_API_PER_MINUTE: "lots" is not a whole number of at least 1`},
		},
		{
			name: "another embedder",
			env:  validEnv(map[string]string{"RONOTE_EMBED_URL": "http://127.0.0.1:18093/", "RONOTE_EMBED_QUERY_URL": "", "RONOTE_EMBED_MODEL": "fake-words"}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits,
				Embed: Embed{URL: "http://127.0.0.1:18093", QueryURL: "http://127.0.0.1:18093", Model: "fake-words"}, Chat: chatDefault,
			},
		},
		{
			name: "the fake model needs no key",
			env:  validEnv(map[string]string{"RONOTE_CHAT_URL": "http://127.0.0.1:8094/", "RONOTE_ANTHROPIC_API_KEY": ""}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault,
				Chat: Chat{Enabled: true, URL: "http://127.0.0.1:8094", Model: "claude-haiku-5-5", DailySpendMicroUSD: 500_000},
			},
		},
		{
			name: "chat turned off needs no key",
			env:  validEnv(map[string]string{"RONOTE_CHAT_ENABLED": "false", "RONOTE_ANTHROPIC_API_KEY": "", "RONOTE_CHAT_MODEL": "claude-other"}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault,
				Chat: Chat{URL: "https://api.anthropic.com", Model: "claude-other", DailySpendMicroUSD: 500_000},
			},
		},
		{
			name: "chat is on by default in test",
			env:  validEnv(map[string]string{"RONOTE_ENV": "test"}),
			want: Config{
				Env: EnvTest, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault,
			},
		},
		{
			name: "chat is off by default in staging, even with a key",
			env:  validEnv(map[string]string{"RONOTE_ENV": "staging"}),
			want: Config{
				Env: EnvStaging, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatOffDefault,
			},
		},
		{
			name: "chat is off by default in prod without a key",
			env:  validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_ANTHROPIC_API_KEY": ""}),
			want: Config{
				Env: EnvProd, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault,
				Chat: Chat{URL: "https://api.anthropic.com", Model: "claude-haiku-5-5", DailySpendMicroUSD: 500_000},
			},
		},
		{
			name: "chat turned on by name in prod",
			env:  validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_CHAT_ENABLED": "true"}),
			want: Config{
				Env: EnvProd, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault,
			},
		},
		{
			name:    "chat turned on by name in staging needs the key",
			env:     validEnv(map[string]string{"RONOTE_ENV": "staging", "RONOTE_CHAT_ENABLED": "true", "RONOTE_ANTHROPIC_API_KEY": ""}),
			wantErr: []string{"RONOTE_ANTHROPIC_API_KEY: not set; https://api.anthropic.com needs one"},
		},
		{
			name:    "Anthropic's API needs a key",
			env:     validEnv(map[string]string{"RONOTE_ANTHROPIC_API_KEY": ""}),
			wantErr: []string{"RONOTE_ANTHROPIC_API_KEY: not set; https://api.anthropic.com needs one"},
		},
		{
			name:    "the fake model refused in prod",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_CHAT_URL": "http://127.0.0.1:8094"}),
			wantErr: []string{`RONOTE_CHAT_URL: "http://127.0.0.1:8094" must be https://api.anthropic.com when RONOTE_ENV is staging or prod`},
		},
		{
			name:    "a kill switch that isn't a boolean",
			env:     validEnv(map[string]string{"RONOTE_CHAT_ENABLED": "maybe"}),
			wantErr: []string{`RONOTE_CHAT_ENABLED: "maybe" is not true or false`},
		},
		{
			// Ollama's threads go with every request; the host's 2 cores
			// are shared by the two embedders and docling.
			name: "the embedders' threads",
			env:  validEnv(map[string]string{"RONOTE_EMBED_QUERY_URL": "http://127.0.0.1:11435", "RONOTE_EMBED_THREADS": "1", "RONOTE_EMBED_QUERY_THREADS": "2"}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits,
				Embed: Embed{URL: embedDefault.URL, QueryURL: "http://127.0.0.1:11435", Model: embedDefault.Model, Threads: 1, QueryThreads: 2}, Chat: chatDefault,
			},
		},
		{
			// Ollama loads the model again for a request with other options,
			// so two clients of one Ollama must send the same.
			name:    "one Ollama for both, with two thread counts",
			env:     validEnv(map[string]string{"RONOTE_EMBED_URL": "http://127.0.0.1:11434", "RONOTE_EMBED_QUERY_URL": "http://127.0.0.1:11434", "RONOTE_EMBED_THREADS": "1", "RONOTE_EMBED_QUERY_THREADS": "2"}),
			wantErr: []string{"RONOTE_EMBED_QUERY_THREADS: 2, but RONOTE_EMBED_THREADS is 1, and both embedders are the Ollama at http://127.0.0.1:11434"},
		},
		{
			name: "one Ollama for both, with one thread count",
			env:  validEnv(map[string]string{"RONOTE_EMBED_THREADS": "2", "RONOTE_EMBED_QUERY_THREADS": "2"}),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits,
				Embed: Embed{URL: embedDefault.URL, QueryURL: embedDefault.QueryURL, Model: embedDefault.Model, Threads: 2, QueryThreads: 2}, Chat: chatDefault,
			},
		},
		{
			name:    "threads that aren't a number",
			env:     validEnv(map[string]string{"RONOTE_EMBED_QUERY_THREADS": "two"}),
			wantErr: []string{`RONOTE_EMBED_QUERY_THREADS: "two" is not a whole number of at least 1`},
		},
		{
			// Searches embed their queries on an Ollama of their own, which a
			// deployment names; dev may share the indexing one.
			name:    "prod without a query embedder",
			env:     validEnv(map[string]string{"RONOTE_ENV": "prod", "RONOTE_EMBED_QUERY_URL": ""}),
			wantErr: []string{"RONOTE_EMBED_QUERY_URL: not set; staging and prod set it"},
		},
		{
			name:    "a query embedder URL of the OpenAI-compatible API",
			env:     validEnv(map[string]string{"RONOTE_EMBED_QUERY_URL": "http://127.0.0.1:11435/v1"}),
			wantErr: []string{`RONOTE_EMBED_QUERY_URL: "http://127.0.0.1:11435/v1" ends in /v1`},
		},
		{
			// The client calls Ollama's native /api/embed, which can refuse
			// an input over the context; /v1 would silently cut it.
			name:    "an embedder URL of the OpenAI-compatible API",
			env:     validEnv(map[string]string{"RONOTE_EMBED_URL": "http://127.0.0.1:11434/v1"}),
			wantErr: []string{`RONOTE_EMBED_URL: "http://127.0.0.1:11434/v1" ends in /v1; give Ollama's own address, such as http://127.0.0.1:11434: RoNote calls its native /api/embed`},
		},
		{
			name:    "an embedder URL that isn't one",
			env:     validEnv(map[string]string{"RONOTE_EMBED_URL": "127.0.0.1:11434"}),
			wantErr: []string{`RONOTE_EMBED_URL: "127.0.0.1:11434" is not an http or https URL`},
		},
		{
			name: "dev falls back to the dev stack's store and converters",
			env:  with(validEnv(nil), noPDFSettings),
			want: Config{
				Env: EnvDev, HTTPAddr: "127.0.0.1:8080", LogLevel: slog.LevelInfo, DatabaseURL: testDatabaseURL, SourceTTL: day,
				PublicURL: "https://ronote.test", GitHub: github, Limits: limits, Embed: embedDefault, Chat: chatDefault,
				Blob: devBlob, PDF: devPDF,
			},
		},
		{
			name: "prod names its own store and converters",
			env:  with(validEnv(map[string]string{"RONOTE_ENV": "prod"}), noPDFSettings),
			wantErr: []string{
				"RONOTE_BLOB_ENDPOINT: not set; staging and prod set it",
				"RONOTE_BLOB_SECRET_KEY: not set",
				"RONOTE_DOCLING_URL: not set",
				"RONOTE_DOCLING_API_KEY: not set",
				"RONOTE_PDFTEXT_API_KEY: not set",
			},
		},
		{
			name:    "a converter URL that isn't one",
			env:     validEnv(map[string]string{"RONOTE_DOCLING_URL": "docling:5001"}),
			wantErr: []string{`RONOTE_DOCLING_URL: "docling:5001" is not an http or https URL`},
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
				if tt.want.Blob == (Blob{}) && tt.want.PDF == (PDF{}) {
					tt.want.Blob, tt.want.PDF = blobSet, pdfSet
				}
				if tt.want.RunnerURL == "" {
					tt.want.RunnerURL = runnerSet
				}
				if tt.want.TrustedProxies == nil {
					tt.want.TrustedProxies = proxiesSet
				}
				if !reflect.DeepEqual(got, tt.want) {
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

// `api migrate` reads the database's URL alone, checked as Load checks it.
func TestDatabaseURL(t *testing.T) {
	t.Parallel()
	good := "postgres://ronote:" + leakMarker + "@db:5432/ronote"
	tests := []struct {
		url     string
		wantErr string
	}{
		{url: good},
		{url: "", wantErr: "RONOTE_DATABASE_URL: not set"},
		{url: "mysql://ronote:" + leakMarker + "@db/ronote", wantErr: "RONOTE_DATABASE_URL: not a postgres://"},
	}
	for _, tt := range tests {
		got, err := DatabaseURL(func(name string) string {
			if name == "RONOTE_DATABASE_URL" {
				return tt.url
			}
			return ""
		})
		if tt.wantErr == "" {
			if err != nil || got != good {
				t.Errorf("DatabaseURL = %v; want the URL", err)
			}
			continue
		}
		if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) || strings.Contains(err.Error(), leakMarker) {
			t.Errorf("DatabaseURL(%q) error = %v, want %q and no secret", tt.url, err, tt.wantErr)
		}
	}
}
