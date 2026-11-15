package s3test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageMatchesCompose(t *testing.T) {
	t.Parallel()
	compose, err := os.ReadFile(filepath.Join("..", "..", "deploy", "compose.dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compose, []byte("image: "+Image+"\n")) {
		t.Errorf("deploy/compose.dev.yml does not use %s", Image)
	}
	command := `command: ["` + strings.Join(Command, `", "`) + `"]`
	if !bytes.Contains(compose, []byte(command)) {
		t.Errorf("deploy/compose.dev.yml does not run SeaweedFS as %s", command)
	}
}
