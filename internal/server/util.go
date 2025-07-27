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

// stripHTML removes HTML tags from a string and normalizes whitespace
func stripHTML(input string) string {
	if input == "" {
		return ""
	}
	
	// Remove HTML tags
	htmlTagRegex := regexp.MustCompile(`<[^>]*>`)
	text := htmlTagRegex.ReplaceAllString(input, "")
	
	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	
	// Clean up extra whitespace
	text = strings.TrimSpace(text)
	text = regexp.MustCompile(`\s+`).ReplaceAllString(text, " ")
	
	return text
}

// truncateText truncates text to the specified length, avoiding word breaks
func truncateText(input string, maxLength int) string {
	if input == "" || maxLength <= 0 {
		return ""
	}
	
	if len(input) <= maxLength {
		return input
	}
	
	// Account for the "..." suffix
	actualLength := maxLength - 3
	if actualLength <= 0 {
		return "..."
	}
	
	text := input[:actualLength]
	// Find the last space to avoid cutting words, but only if we have reasonable space
	if lastSpace := strings.LastIndex(text, " "); lastSpace > actualLength/2 {
		text = text[:lastSpace]
	}
	text += "..."
	
	return text
}

// ProcessBodyText strips HTML tags and truncates text to the specified length
func ProcessBodyText(input string, maxLength int) string {
	if input == "" {
		return ""
	}
	
	// Remove HTML tags
	text := stripHTML(input)
	
	// Truncate if necessary
	if maxLength > 0 {
		text = truncateText(text, maxLength)
	}
	
	return text
}
