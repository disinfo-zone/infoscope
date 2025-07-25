package server

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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

// stripHTML removes HTML tags from a string and unescapes HTML entities
func stripHTML(content string) string {
	if content == "" {
		return ""
	}

	// Remove HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	content = re.ReplaceAllString(content, "")

	// Unescape HTML entities
	content = html.UnescapeString(content)

	// Replace multiple whitespace with single space
	re = regexp.MustCompile(`\s+`)
	content = re.ReplaceAllString(content, " ")

	// Trim whitespace
	return strings.TrimSpace(content)
}

// truncateText truncates text to the specified length, breaking at word boundaries when possible
func truncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	
	// If the text is longer than maxLength, find a good break point
	if maxLength <= 0 {
		return ""
	}
	
	// Handle edge case for very short maxLength
	if maxLength < 10 {
		// For very short limits, just cut at the character boundary
		if maxLength <= 3 {
			return "..."
		}
		cutPoint := maxLength - 3
		if utf8.ValidString(text[:cutPoint]) {
			return text[:cutPoint] + "..."
		}
		// Find valid UTF-8 boundary
		for i := cutPoint; i > 0; i-- {
			if utf8.ValidString(text[:i]) {
				return text[:i] + "..."
			}
		}
		return "..."
	}
	
	// Try to break at word boundary
	truncated := text[:maxLength-3] // Leave space for "..."
	
	// Find the last space within our limit
	lastSpace := strings.LastIndexFunc(truncated, unicode.IsSpace)
	
	if lastSpace > maxLength/2 { // Only use word boundary if it's not too far back
		truncated = truncated[:lastSpace]
	}
	
	return strings.TrimSpace(truncated) + "..."
}

// ProcessBodyText strips HTML and truncates content for display
func ProcessBodyText(content string, maxLength int) string {
	if content == "" {
		return ""
	}

	// Strip HTML tags and clean up text
	cleanText := stripHTML(content)

	// Truncate to specified length
	return truncateText(cleanText, maxLength)
}
