package embed

import (
	"encoding/json"
	"net/http"
)

// maxRequest bounds what FakeServer reads.
const maxRequest = 16 << 20

// FakeServer serves Fake over Ollama's /api/embed and /api/tags, for dev and
// the end-to-end tests (cmd/fakeembed), so they use the real client and need
// no model. It answers for whatever model is asked; it has no context to
// run over.
func FakeServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /api/tags", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"models": []map[string]string{{"name": FakeModel, "model": FakeModel}}})
	})
	mux.HandleFunc("POST /api/embed", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Input any    `json:"input"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequest)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		var texts []string
		switch in := req.Input.(type) {
		case string:
			texts = []string{in}
		case []any:
			for _, t := range in {
				s, ok := t.(string)
				if !ok {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input must be strings"})
					return
				}
				texts = append(texts, s)
			}
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input is required"})
			return
		}
		vectors := make([][]float32, len(texts))
		for i, text := range texts {
			vectors[i] = FakeVector(text)
		}
		writeJSON(w, http.StatusOK, map[string]any{"model": req.Model, "embeddings": vectors})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
