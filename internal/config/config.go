// Package config loads and validates the service configuration from
// environment variables.
package config

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/publicsuffix"
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
	// RunnerURL is the origin code from documents runs on, such as
	// https://ronote-run.dev: in staging and prod a registrable domain of
	// its own (docs/adr/0008-in-browser-code-execution.md).
	RunnerURL string
	// GitHub is the OAuth app people sign in with.
	GitHub GitHub
	// TrustedProxies are the networks whose X-Forwarded-For says which
	// client a request is from: Caddy's in production, none by default
	// (docs/adr/0014-production-topology.md).
	TrustedProxies []netip.Prefix
	// Limits bound what one user, or one IP, may do.
	Limits Limits
	// Embed is the embedder search uses.
	Embed Embed
	// Chat is the model chat answers with.
	Chat Chat
	// Blob is where uploaded PDFs are kept, and PDF what converts them.
	Blob Blob
	PDF  PDF
}

// Blob is an S3-compatible store (docs/adr/0013-pdf-import.md).
type Blob struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

// PDF is docling-serve, which converts PDFs, and pdftext, which reads their
// code from the text layer, each with its API key
// (docs/adr/0013-pdf-import.md).
type PDF struct {
	DoclingURL string
	DoclingKey string
	TextURL    string
	TextKey    string
}

// The dev stack's blob store, docling and pdftext (deploy/compose.dev.yml):
// dev and test fall back to them, staging and prod must say where theirs are.
var (
	devBlob = Blob{Endpoint: "http://127.0.0.1:8333", Region: "us-east-1", Bucket: "ronote", AccessKey: "ronote", SecretKey: "ronote-dev-secret"} //nolint:gosec // the dev stack's own key, never a deployment's
	devPDF  = PDF{DoclingURL: "http://127.0.0.1:5001", DoclingKey: "ronote-dev-docling", TextURL: "http://127.0.0.1:5002", TextKey: "ronote-dev-pdftext"}
)

// The embedder by default: Ollama on this machine, with the pinned model
// (docs/adr/0011-retrieval.md).
const (
	DefaultEmbedURL   = "http://127.0.0.1:11434"
	DefaultEmbedModel = "qwen3-embedding:0.6b"
)

// Embed is Ollama's embeddings API and the model to ask it for
// (docs/adr/0011-retrieval.md).
type Embed struct {
	// URL is Ollama's address, such as http://127.0.0.1:11434: the one that
	// indexes documents.
	URL string
	// QueryURL is the Ollama that embeds searches' queries: one of its own
	// in a deployment, so a query never waits behind indexing; dev and test
	// may share URL's.
	QueryURL string
	Model    string
}

// The model chat answers with by default (docs/adr/0012-chat.md).
const (
	DefaultChatURL   = "https://api.anthropic.com"
	DefaultChatModel = "claude-haiku-5-5"
	chatHost         = "api.anthropic.com"
)

// Chat is the Messages API chat calls: Anthropic's, or in dev and test the
// fake model (cmd/fakellm), which needs no key.
type Chat struct {
	// Enabled is the kill switch: false turns chat off for everyone.
	Enabled bool
	URL     string
	Model   string
	APIKey  string
	// DailySpendMicroUSD caps what every user's answers may cost together
	// in a UTC day (docs/adr/0014-production-topology.md).
	DailySpendMicroUSD int64
}

// Limits are RoNote's quotas and rate limits
// (docs/adr/0010-sign-in-and-sessions.md, docs/adr/0012-chat.md).
type Limits struct {
	ImportsPerDay int
	Documents     int
	// APIPerMinute counts one user's API requests, the import event
	// stream excepted; AuthPerMinute counts one IP's sign-in requests.
	APIPerMinute  int
	AuthPerMinute int
	// ChatMessagesPerDay and ChatTokensPerDay count one user's questions,
	// and the input tokens their answers read, in any 24 hours.
	ChatMessagesPerDay int
	ChatTokensPerDay   int
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
	// devRunnerURL is where make web-dev serves the runner.
	devRunnerURL = "http://localhost:5174"
	// defaultChatDailyUSD is chat's budget for a UTC day, for all users
	// together.
	defaultChatDailyUSD = "0.50"
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
	proxies, proxyErrs := parseProxies(getenv("RONOTE_TRUSTED_PROXIES"))
	for _, err := range proxyErrs {
		check("RONOTE_TRUSTED_PROXIES", err)
	}
	allowPrivate, err := parseAllowPrivate(getenv("RONOTE_FETCH_ALLOW_PRIVATE"), env)
	check("RONOTE_FETCH_ALLOW_PRIVATE", err)
	ttl, err := parseTTL(withDefault(getenv("RONOTE_SOURCE_TTL"), defaultSourceTTL))
	check("RONOTE_SOURCE_TTL", err)
	publicURL, err := parseOrigin(getenv("RONOTE_PUBLIC_URL"), env, "the origin people reach RoNote at, such as https://ronote.app")
	check("RONOTE_PUBLIC_URL", err)
	runnerURL := getenv("RONOTE_RUNNER_URL")
	if runnerURL == "" && !env.Deployed() {
		runnerURL = devRunnerURL
	}
	runnerURL, err = parseOrigin(runnerURL, env, "the origin code from documents runs on, such as https://ronote-run.dev")
	check("RONOTE_RUNNER_URL", err)
	if env.Deployed() && publicURL != "" && runnerURL != "" {
		check("RONOTE_RUNNER_URL", otherSite(runnerURL, publicURL))
	}
	if env.Deployed() && len(proxies) == 0 && len(proxyErrs) == 0 {
		check("RONOTE_TRUSTED_PROXIES", errors.New("not set; staging and prod run behind Caddy, whose network it names"))
	}
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
	embed := Embed{
		URL:   strings.TrimSuffix(withDefault(getenv("RONOTE_EMBED_URL"), DefaultEmbedURL), "/"),
		Model: withDefault(getenv("RONOTE_EMBED_MODEL"), DefaultEmbedModel),
	}
	check("RONOTE_EMBED_URL", validateEmbedURL(embed.URL))
	embed.QueryURL = strings.TrimSuffix(getenv("RONOTE_EMBED_QUERY_URL"), "/")
	switch {
	case embed.QueryURL == "" && env.Deployed():
		errs = append(errs, errors.New("RONOTE_EMBED_QUERY_URL: not set; staging and prod set it"))
	case embed.QueryURL == "":
		embed.QueryURL = embed.URL
	default:
		check("RONOTE_EMBED_QUERY_URL", validateEmbedURL(embed.QueryURL))
	}
	chat, chatErrs := parseChat(getenv, env)
	errs = append(errs, chatErrs...)
	blobStore, pdf, pdfErrs := parsePDF(getenv, env)
	errs = append(errs, pdfErrs...)
	var limits Limits
	for _, l := range []struct {
		name     string
		fallback int
		into     *int
	}{
		{"RONOTE_LIMIT_IMPORTS_PER_DAY", 60, &limits.ImportsPerDay},
		{"RONOTE_LIMIT_DOCUMENTS", 500, &limits.Documents},
		{"RONOTE_LIMIT_API_PER_MINUTE", 120, &limits.APIPerMinute},
		{"RONOTE_LIMIT_AUTH_PER_MINUTE", 20, &limits.AuthPerMinute},
		{"RONOTE_LIMIT_CHAT_MESSAGES_PER_DAY", 50, &limits.ChatMessagesPerDay},
		{"RONOTE_LIMIT_CHAT_TOKENS_PER_DAY", 400_000, &limits.ChatTokensPerDay},
	} {
		n, err := parseLimit(getenv(l.name), l.fallback)
		check(l.name, err)
		*l.into = n
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid config:\n%w", errors.Join(errs...))
	}
	return Config{
		Env:                       env,
		HTTPAddr:                  addr,
		LogLevel:                  level,
		DatabaseURL:               dbURL,
		TrustedProxies:            proxies,
		FetchAllowPrivateNetworks: allowPrivate,
		SourceTTL:                 ttl,
		PublicURL:                 publicURL,
		RunnerURL:                 runnerURL,
		GitHub:                    gh,
		Limits:                    limits,
		Embed:                     embed,
		Chat:                      chat,
		Blob:                      blobStore,
		PDF:                       pdf,
	}, nil
}

// parsePDF reads where PDFs are kept and what converts them. Dev and test
// default to the dev stack; staging and prod set every one, so a deployment
// never talks to a store or a converter with a known key.
func parsePDF(getenv func(string) string, env Env) (Blob, PDF, []error) {
	var errs []error
	read := func(name, dev string) string {
		v := getenv(name)
		if v == "" && !env.Deployed() {
			v = dev
		}
		if err := required(v); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w; staging and prod set it", name, err))
		}
		return v
	}
	b := Blob{
		Endpoint:  strings.TrimSuffix(read("RONOTE_BLOB_ENDPOINT", devBlob.Endpoint), "/"),
		Region:    read("RONOTE_BLOB_REGION", devBlob.Region),
		Bucket:    read("RONOTE_BLOB_BUCKET", devBlob.Bucket),
		AccessKey: read("RONOTE_BLOB_ACCESS_KEY", devBlob.AccessKey),
		SecretKey: read("RONOTE_BLOB_SECRET_KEY", devBlob.SecretKey),
	}
	p := PDF{
		DoclingURL: strings.TrimSuffix(read("RONOTE_DOCLING_URL", devPDF.DoclingURL), "/"),
		DoclingKey: read("RONOTE_DOCLING_API_KEY", devPDF.DoclingKey),
		TextURL:    strings.TrimSuffix(read("RONOTE_PDFTEXT_URL", devPDF.TextURL), "/"),
		TextKey:    read("RONOTE_PDFTEXT_API_KEY", devPDF.TextKey),
	}
	for name, u := range map[string]string{"RONOTE_BLOB_ENDPOINT": b.Endpoint, "RONOTE_DOCLING_URL": p.DoclingURL, "RONOTE_PDFTEXT_URL": p.TextURL} {
		if u == "" {
			continue
		}
		if err := validateHTTPURL(u); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
	return b, p, errs
}

// parseChat reads the chat model's settings. Anthropic's API needs a key;
// any other URL is the fake model, which only dev and test may use. Chat is
// on by default in dev and test, and off in staging and prod until
// RONOTE_CHAT_ENABLED=true says otherwise (docs/adr/0012-chat.md).
func parseChat(getenv func(string) string, env Env) (Chat, []error) {
	var errs []error
	c := Chat{
		Enabled: !env.Deployed(),
		URL:     strings.TrimSuffix(withDefault(getenv("RONOTE_CHAT_URL"), DefaultChatURL), "/"),
		Model:   withDefault(getenv("RONOTE_CHAT_MODEL"), DefaultChatModel),
		APIKey:  getenv("RONOTE_ANTHROPIC_API_KEY"),
	}
	if s := getenv("RONOTE_CHAT_ENABLED"); s != "" {
		enabled, err := strconv.ParseBool(s)
		if err != nil {
			errs = append(errs, fmt.Errorf("RONOTE_CHAT_ENABLED: %q is not true or false", s))
		}
		c.Enabled = enabled
	}
	if err := validateProviderURL(c.URL, chatHost, env); err != nil {
		errs = append(errs, fmt.Errorf("RONOTE_CHAT_URL: %w", err))
	}
	daily, err := parseDollars(withDefault(getenv("RONOTE_CHAT_DAILY_USD"), defaultChatDailyUSD))
	if err != nil {
		errs = append(errs, fmt.Errorf("RONOTE_CHAT_DAILY_USD: %w", err))
	}
	c.DailySpendMicroUSD = daily
	if u, err := url.Parse(c.URL); err == nil && c.Enabled && u.Host == chatHost && c.APIKey == "" {
		errs = append(errs, fmt.Errorf("RONOTE_ANTHROPIC_API_KEY: not set; %s needs one. Set it, point RONOTE_CHAT_URL at the fake model, or set RONOTE_CHAT_ENABLED=false", c.URL))
	}
	return c, errs
}

// parseDollars reads an amount of dollars above 0, such as 0.50, as
// micro-dollars, the unit answers' cost is recorded in.
func parseDollars(s string) (int64, error) {
	usd, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(usd) || math.IsInf(usd, 0) || usd <= 0 {
		return 0, fmt.Errorf("%q is not an amount of dollars above 0, such as 0.50", s)
	}
	return int64(math.Round(usd * 1e6)), nil
}

func validateHTTPURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http or https URL", s)
	}
	return nil
}

// validateEmbedURL accepts Ollama's own address. One ending in /v1 is its
// OpenAI-compatible API, which cuts an input over the model's context
// without a word; the client calls the native /api/embed, which refuses it.
func validateEmbedURL(s string) error {
	if err := validateHTTPURL(s); err != nil {
		return err
	}
	if strings.HasSuffix(s, "/v1") {
		return fmt.Errorf("%q ends in /v1; give Ollama's own address, such as %s: RoNote calls its native /api/embed", s, DefaultEmbedURL)
	}
	return nil
}

// parseLimit reads a positive whole number, fallback when unset.
func parseLimit(s string, fallback int) (int, error) {
	if s == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a whole number of at least 1", s)
	}
	return n, nil
}

// Deployed reports whether env is one people use, staging or prod, where
// cookies are Secure and only GitHub signs people in.
func (e Env) Deployed() bool {
	return e == EnvStaging || e == EnvProd
}

// parsePublicURL reads the origin RoNote is served at, http or https with
// no path; staging and prod need https, for Secure cookies.
// parseOrigin reads an origin, scheme://host[:port], which in staging and
// prod is https; want says what it is, for when it is missing.
func parseOrigin(s string, env Env, want string) (string, error) {
	if s == "" {
		return "", errors.New("not set; want " + want)
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

// otherSite fails when runner is on the app's site: the same registrable
// domain, by the public suffix list, which is what makes two hosts one site
// to a browser's cookies (docs/adr/0008-in-browser-code-execution.md).
func otherSite(runner, app string) error {
	runnerSite, err := site(runner)
	if err != nil {
		return err
	}
	appSite, err := site(app)
	if err != nil {
		return fmt.Errorf("RONOTE_PUBLIC_URL: %w", err)
	}
	if runnerSite == appSite {
		return fmt.Errorf("%s is on the same site as RONOTE_PUBLIC_URL (%s); the runner needs a registrable domain of its own (docs/adr/0008-in-browser-code-execution.md)", runner, appSite)
	}
	return nil
}

// site is an origin's registrable domain.
func site(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return "", err
	}
	if _, err := netip.ParseAddr(u.Hostname()); err == nil {
		return "", fmt.Errorf("%s is an IP address; want a domain name", origin)
	}
	s, err := publicsuffix.EffectiveTLDPlusOne(u.Hostname())
	if err != nil {
		return "", fmt.Errorf("%s has no registrable domain", origin)
	}
	return s, nil
}

// validateProviderURL allows any http(s) URL in dev and test, where the fake
// provider stands in, and only https on host in staging and prod.
func validateProviderURL(s, host string, env Env) error {
	if err := validateHTTPURL(s); err != nil {
		return err
	}
	if u, _ := url.Parse(s); env.Deployed() && (u.Scheme != "https" || u.Host != host) {
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

// parseProxies reads networks and addresses, separated by commas or
// spaces; an address is a network of one. Each bad one is its own error.
func parseProxies(s string) ([]netip.Prefix, []error) {
	nets := []netip.Prefix{}
	var errs []error
	for _, field := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		if a, err := netip.ParseAddr(field); err == nil {
			nets = append(nets, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		n, err := netip.ParsePrefix(field)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%q is not a CIDR or an IP", field))
		case n != n.Masked():
			errs = append(errs, fmt.Errorf("%q has host bits set; want %s", field, n.Masked()))
		default:
			nets = append(nets, n)
		}
	}
	return nets, errs
}

// DatabaseURL is RONOTE_DATABASE_URL, checked as Load checks it, for a
// command that needs only the database, such as `api migrate`.
func DatabaseURL(getenv func(string) string) (string, error) {
	url := getenv("RONOTE_DATABASE_URL")
	if err := validateDatabaseURL(url); err != nil {
		return "", fmt.Errorf("RONOTE_DATABASE_URL: %w", err)
	}
	return url, nil
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
