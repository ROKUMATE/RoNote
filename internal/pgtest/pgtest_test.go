//go:build integration

package pgtest

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestImageMatchesCompose(t *testing.T) {
	t.Parallel()

	compose, err := os.ReadFile(filepath.Join(repoRoot(), "deploy", "compose.dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compose, []byte("image: "+Image+"\n")) {
		t.Errorf("deploy/compose.dev.yml does not use %s", Image)
	}
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}
