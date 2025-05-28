package server

import (
	"encoding/json"
	"net/http"
)

// headerWritten checks if response headers have already been written
func headerWritten(w http.ResponseWriter) bool {
	if ww, ok := w.(interface{ Written() bool }); ok {
		return ww.Written()
	}
	return false
}

// RespondWithError sends a JSON error response.
// It's a convenience wrapper around RespondWithJSON.
func RespondWithError(w http.ResponseWriter, code int, message string) {
	RespondWithJSON(w, code, map[string]string{"error": message})
}

// RespondWithJSON sends a JSON response with the given status code and payload.
// If the payload is nil, no body is sent.
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload != nil {
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			// If encoding fails, log the error.
			// This assumes a logger is accessible, or it might panic.
			// For simplicity here, we're not logging, but in a real app, you would.
			// http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			// The above line is problematic as headers are already written.
		}
	}
}
