package config

import (
	"fmt"
	"io"
	"net/url"
	"strings"
)

// SecureCookies reports whether cookies are Secure and named with the
// __Host- prefix: always in staging and prod, never in dev and test, which
// run over plain http (docs/adr/0010-sign-in-and-sessions.md).
func (c Config) SecureCookies() bool { return c.Env.Deployed() }

// Check loads the configuration through getenv and writes what it found to
// w, starting nothing and never writing a secret, for `api check-config`
// and `worker check-config` (docs/adr/0014-production-topology.md).
func Check(getenv func(string) string, w io.Writer) (Config, error) {
	cfg, err := Load(getenv)
	if err != nil {
		return Config{}, err
	}
	_, err = io.WriteString(w, cfg.Summary())
	return cfg, err
}

// Summary says what the configuration sets, a line each, without secrets:
// no keys, no client secret, no database password.
func (c Config) Summary() string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("config OK for %s", c.Env)
	line("database: %s", hostAndPath(c.DatabaseURL))
	line("app: %s; runner: %s", c.PublicURL, c.RunnerURL)
	if c.SecureCookies() {
		line("cookies: Secure, named __Host-")
	} else {
		line("cookies: plain, for http in %s", c.Env)
	}
	line("sign-in: %s", hostAndPath(c.GitHub.AuthorizeURL))
	if len(c.TrustedProxies) == 0 {
		line("trusted proxies: none")
	} else {
		proxies := make([]string, len(c.TrustedProxies))
		for i, p := range c.TrustedProxies {
			proxies[i] = p.String()
		}
		line("trusted proxies: %s", strings.Join(proxies, ", "))
	}
	if c.Chat.Enabled {
		line("chat: on, %s at %s, $%.2f a day for everyone", c.Chat.Model, c.Chat.URL, float64(c.Chat.DailySpendMicroUSD)/1e6)
	} else {
		line("chat: off")
	}
	line("embedder: %s at %s", c.Embed.Model, c.Embed.URL)
	line("blob store: %s, bucket %s", c.Blob.Endpoint, c.Blob.Bucket)
	line("docling: %s; pdftext: %s", c.PDF.DoclingURL, c.PDF.TextURL)
	return b.String()
}

// hostAndPath is a URL without its user, password or query.
func hostAndPath(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return "(unreadable)"
	}
	return u.Host + u.Path
}
