// internal/server/middleware.go
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"
)

const (
	csrfTokenLength = 32
	csrfCookieName  = "csrf_token"
	csrfHeaderName  = "X-CSRF-Token"
	csrfMetaName    = "csrf-token"
	// Increase cookie max age to 24 hours
	csrfCookieMaxAge = 86400
)

// CSRFManager handles CSRF token generation and validation
type CSRFManager struct {
	tokens    map[string]time.Time
	mutex     sync.RWMutex
	maxTokens int
}

// NewCSRFManager creates a new CSRF token manager
func NewCSRFManager() *CSRFManager {
	return &CSRFManager{
		tokens:    make(map[string]time.Time),
		maxTokens: 10000, // Increase max tokens
	}
}

// generateToken creates a new CSRF token
func (cm *CSRFManager) generateToken() (string, error) {
	b := make([]byte, csrfTokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// SetCSRFToken generates and sets a new CSRF token
func (cm *CSRFManager) SetCSRFToken(w http.ResponseWriter) (string, error) {
	token, err := cm.generateToken()
	if err != nil {
		return "", err
	}

	// Store token with timestamp
	cm.mutex.Lock()
	cm.tokens[token] = time.Now()
	// Clean old tokens if we exceed maxTokens
	if len(cm.tokens) > cm.maxTokens {
		for t, timestamp := range cm.tokens {
			if time.Since(timestamp) > 24*time.Hour {
				delete(cm.tokens, t)
			}
		}
	}
	cm.mutex.Unlock()

	// Set cookie with proper security flags
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   csrfCookieMaxAge,
	})

	return token, nil
}

// validateToken compares tokens in constant time
func (cm *CSRFManager) validateToken(requestToken, cookieToken string) bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// Log token validation attempt
	fmt.Printf("Validating CSRF tokens - Request: %s, Cookie: %s\n", requestToken, cookieToken)
	fmt.Printf("Token exists in store: %v\n", cm.tokens[cookieToken])

	if requestToken == "" || cookieToken == "" {
		return false
	}

	// Verify token exists in our store
	_, exists := cm.tokens[cookieToken]
	if !exists {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(requestToken), []byte(cookieToken)) == 1
}

// CSRFMiddleware adds CSRF protection to handlers
func (cm *CSRFManager) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add CORS headers if needed
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		// For safe methods, set a new token
		if r.Method == http.MethodGet ||
			r.Method == http.MethodHead ||
			r.Method == http.MethodOptions ||
			r.Method == http.MethodTrace {

			if err := cm.SetCSRFToken(w); err != nil {
				http.Error(w, "Failed to generate CSRF token", http.StatusInternalServerError)
				return
			}

			// Get the cookie we just set
			cookie, err := r.Cookie(csrfCookieName)
			if err == nil {
				// Add CSRF meta to context
				meta := template.HTML(fmt.Sprintf(`<meta name="%s" content="%s">`, csrfMetaName, cookie.Value))
				ctx := context.WithValue(r.Context(), contextKeyCSRFMeta, meta)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
			return
		}

		// For unsafe methods, validate the token
		requestToken := r.Header.Get("X-CSRF-Token")
		cookie, err := r.Cookie(csrfCookieName)

		fmt.Printf("Request method: %s\n", r.Method)
		fmt.Printf("Request token: %s\n", requestToken)
		fmt.Printf("Cookie present: %v\n", err == nil)
		if err == nil {
			fmt.Printf("Cookie token: %s\n", cookie.Value)
		}

		if err != nil || !cm.validateToken(requestToken, cookie.Value) {
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
