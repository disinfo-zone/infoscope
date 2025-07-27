package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
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
			// If encoding fails, we can't send an error response since headers are already written
			// In a real application, you would log this error
			_ = err
		}
	}
}

// ProcessBodyText strips HTML tags and truncates text to the specified length
func ProcessBodyText(input string, maxLength int) string {
	if input == "" {
		return ""
	}
	
	// Remove HTML tags
	htmlTagRegex := regexp.MustCompile(`<[^>]*>`)
	text := htmlTagRegex.ReplaceAllString(input, "")
	
	// Clean up extra whitespace
	text = strings.TrimSpace(text)
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	
	// Truncate if necessary
	if maxLength > 0 && len(text) > maxLength {
		text = text[:maxLength]
		// Find the last space to avoid cutting words
		if lastSpace := strings.LastIndex(text, " "); lastSpace > maxLength/2 {
			text = text[:lastSpace]
		}
		text += "..."
	}
	
	return text
}
