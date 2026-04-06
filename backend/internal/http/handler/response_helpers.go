package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/api"
)

func writeDataJSON(w http.ResponseWriter, status int, payload any) {
	writeJSON(w, status, api.DataResponse{Data: payload})
}

func writeErrorJSON(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, api.ErrorResponse{Error: api.ErrorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeSSEComment(w http.ResponseWriter, comment string) {
	_, _ = fmt.Fprintf(w, ": %s\n\n", comment)
}

func writeSSEEvent(w http.ResponseWriter, event string, id int64, payload any) {
	if id > 0 {
		_, _ = fmt.Fprintf(w, "id: %d\n", id)
	}
	if strings.TrimSpace(event) != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintf(w, "data: {\"message\":\"marshal error\"}\n\n")
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
}
