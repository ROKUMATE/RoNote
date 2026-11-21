package config

import (
	"os"
	"strings"
	"testing"
)

// exampleSecrets stand in for the podman secrets a deployment adds to
// deploy/ronote.env.example's settings; none may appear in what Check says.
var exampleSecrets = map[string]string{
	"RONOTE_DATABASE_URL":         "postgres://ronote:" + leakMarker + "-db@ronote-postgres:5432/ronote?sslmode=disable",
	"RONOTE_GITHUB_CLIENT_SECRET": leakMarker + "-github",
	"RONOTE_ANTHROPIC_API_KEY":    leakMarker + "-anthropic",
	"RONOTE_BLOB_ACCESS_KEY":      leakMarker + "-blob-access",
	"RONOTE_BLOB_SECRET_KEY":      leakMarker + "-blob-secret",
	"RONOTE_DOCLING_API_KEY":      leakMarker + "-docling",
	"RONOTE_PDFTEXT_API_KEY":      leakMarker + "-pdftext",
}

// exampleEnv is deploy/ronote.env.example's settings, read as podman's
// --env-file and systemd's EnvironmentFile read KEY=VALUE lines, skipping
// blanks and # comments.
func exampleEnv(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("../../deploy/ronote.env.example")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("%q is not KEY=VALUE", line)
		}
		env[k] = v
	}
	return env
}

func TestCheckTheExampleProductionConfig(t *testing.T) {
	t.Parallel()
	env := with(exampleEnv(t), exampleSecrets)
	var out strings.Builder
	if _, err := Check(func(k string) string { return env[k] }, &out); err != nil {
		t.Fatalf("Check(the example) = %v", err)
	}
	said := out.String()
	for _, want := range []string{
		"config OK for prod",
		"app: https://ronote.example.me; runner: https://ronote-runner.pages.dev",
		"cookies: Secure, named __Host-",
		"sign-in: github.com/login/oauth/authorize",
		"trusted proxies: 10.89.10.0/24",
		"chat: on, claude-haiku-5-5 at https://api.anthropic.com, $0.20 a day for everyone",
		"blob store: https://examplenamespace.compat.objectstorage.eu-frankfurt-1.oraclecloud.com, bucket ronote-pdfs",
		"database: ronote-postgres:5432/ronote",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("Check said %q, want it to say %q", said, want)
		}
	}
	if strings.Contains(said, leakMarker) {
		t.Errorf("Check said a secret: %q", said)
	}

	// The same deployment with the runner on the app's own site.
	sameSite := with(env, map[string]string{"RONOTE_RUNNER_URL": "https://run.ronote.example.me"})
	out.Reset()
	_, err := Check(func(k string) string { return sameSite[k] }, &out)
	if err == nil || !strings.Contains(err.Error(), "same site as RONOTE_PUBLIC_URL (example.me)") || out.Len() != 0 {
		t.Errorf("Check with a same-site runner = %v, said %q; want it refused, saying nothing else", err, out.String())
	}
}

func TestSummaryInDev(t *testing.T) {
	t.Parallel()
	env := validEnv(map[string]string{"RONOTE_RUNNER_URL": "", "RONOTE_TRUSTED_PROXIES": "", "RONOTE_CHAT_ENABLED": "false"})
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	said := cfg.Summary()
	for _, want := range []string{"config OK for dev", "runner: http://localhost:5174", "cookies: plain, for http in dev", "trusted proxies: none", "chat: off"} {
		if !strings.Contains(said, want) {
			t.Errorf("Summary = %q, want %q", said, want)
		}
	}
}
