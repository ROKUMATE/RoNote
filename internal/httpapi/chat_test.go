package httpapi

import (
	"net/http"
	"testing"

	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/httpapi/gen"
)

// TestChatTurnedOff checks every conversations route answers 503 while the
// kill switch has chat off, or there is no chat.
func TestChatTurnedOff(t *testing.T) {
	t.Parallel()
	off, err := chat.NewService(chat.Options{ModelName: "claude-haiku-5-5", Logger: discardLogger})
	if err != nil {
		t.Fatal(err)
	}
	id := "0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10"
	for name, c := range map[string]*chat.Service{"no chat": nil, "turned off": off} {
		h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, Chat: c})
		for _, r := range []struct{ method, target, body string }{
			{http.MethodGet, "/api/v1/conversations", ""},
			{http.MethodPost, "/api/v1/conversations", "{}"},
			{http.MethodGet, "/api/v1/conversations/" + id, ""},
			{http.MethodDelete, "/api/v1/conversations/" + id, ""},
			{http.MethodPost, "/api/v1/conversations/" + id + "/messages", `{"text":"q"}`},
			{http.MethodPost, "/api/v1/conversations/" + id + "/stop", ""},
		} {
			rec := apiRequest(t, h, r.method, r.target, r.body)
			if rec.Code != http.StatusServiceUnavailable || decodeError(t, rec).Code != gen.ErrorCodeChatDisabled {
				t.Errorf("%s: %s %s = %d %s, want 503 chat_disabled", name, r.method, r.target, rec.Code, rec.Body)
			}
		}
		// The rest of the API is unaffected.
		if rec := apiRequest(t, h, http.MethodGet, "/api/v1/documents", ""); rec.Code != http.StatusOK {
			t.Errorf("%s: GET /api/v1/documents = %d, want 200", name, rec.Code)
		}
	}
}
