package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/rokumate/ronote/internal/httpapi/gen"
)

// writeError writes the error envelope of api/openapi.yaml. Every error
// response goes through it or apiError.
func writeError(w http.ResponseWriter, r *http.Request, status int, code gen.ErrorCode, message string) {
	writeJSON(w, status, gen.Error{Error: gen.ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: requestID(r.Context()),
	}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent, so a failed write leaves nothing to report.
	_ = json.NewEncoder(w).Encode(v)
}
