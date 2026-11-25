package docling

import (
	"os"
	"testing"

	"github.com/rokumate/ronote/internal/httpguard"
)

// Every request goes through a client of the test's own: the shared default
// transport refuses (internal/httpguard).
func TestMain(m *testing.M) {
	httpguard.Off()
	os.Exit(m.Run())
}
