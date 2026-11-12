package embed

import (
	"encoding/json"
	"net/http"
)

// maxRequest bounds what FakeServer reads.
const maxRequest = 16 << 20

// FakeServer serves Fake over OpenAI's /v1/embeddings, for dev and the
// end-to-end tests (cmd/fakeembed), so they use the real client and need no
// model. It answers for whatever model is asked.
func FakeServer() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []map[string]string{{"id": FakeModel, "object": "model"}}})
	})
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
			Input any    `json:"input"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequest)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": err.Error()}})
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
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": "input must be strings"}})
					return
				}
				texts = append(texts, s)
			}
		default:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"message": "input is required"}})
			return
		}
		data := make([]map[string]any, len(texts))
		for i, text := range texts {
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": FakeVector(text)}
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "model": req.Model, "data": data})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
