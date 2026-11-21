package checkconfig

import (
	"strings"
	"testing"
)

// dev is a configuration that loads, with its blob store at a port nothing
// listens on, so a check reaches no store and fails at once.
func dev(k string) string {
	return map[string]string{
		"RONOTE_ENV":                  "dev",
		"RONOTE_DATABASE_URL":         "postgres://127.0.0.1:5432/ronote",
		"RONOTE_PUBLIC_URL":           "http://127.0.0.1:5173",
		"RONOTE_CHAT_ENABLED":         "false",
		"RONOTE_GITHUB_CLIENT_ID":     "id",
		"RONOTE_GITHUB_CLIENT_SECRET": "secret",
		"RONOTE_BLOB_ENDPOINT":        "http://127.0.0.1:1",
		"RONOTE_BLOB_BUCKET":          "ronote-pdfs",
	}[k]
}

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		said    string
		wantErr string
	}{
		{name: "the bucket is checked, and its failure names it", wantErr: "bucket ronote-pdfs"},
		{name: "--offline skips it, and says so", args: []string{"--offline"}, said: "blob store: not checked (--offline)"},
		{name: "other arguments are refused", args: []string{"--online"}, wantErr: `takes nothing or --offline, got "--online"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			err := Run(t.Context(), tt.args, dev, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Run(%q) = %v, want an error saying %q", tt.args, err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(out.String(), "config OK for dev") || !strings.Contains(out.String(), tt.said) {
				t.Errorf("Run(%q) = %v, said %q; want it to say %q", tt.args, err, out.String(), tt.said)
			}
		})
	}
}
