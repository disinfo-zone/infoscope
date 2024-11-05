// internal/server/csrf.go
package server

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"
)

var (
	ErrTokenMissing = errors.New("CSRF token missing")
	ErrTokenInvalid = errors.New("CSRF token invalid")
)

// CSRFConfig holds configuration for CSRF protection
type CSRFConfig struct {
	Cookie    string
	Header    string
	Secure    bool
	Expiry    time.Duration
	FieldName string
}

// DefaultConfig returns the default CSRF configuration
func DefaultConfig() CSRFConfig {
	return CSRFConfig{
		Cookie:    "csrf_token",
		Header:    "X-CSRF-Token",
		Secure:    true, // Will be overridden by server config
		Expiry:    24 * time.Hour,
		FieldName: "csrf_token",
	}
}

// CSRF manages CSRF token generation and validation
type CSRF struct {
	config CSRFConfig
	tokens sync.Map
}

// NewCSRF creates a new CSRF instance
func NewCSRF(config CSRFConfig) *CSRF {
	c := &CSRF{
		config: config,
		tokens: sync.Map{},
	}
	go c.startCleanupLoop()
	return c
}

// generateToken creates a new random token
func (c *CSRF) generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// getOrCreateToken gets an existing token or creates a new one
func (c *CSRF) getOrCreateToken(w http.ResponseWriter, r *http.Request) (string, error) {
	// Check for existing cookie
	cookie, err := r.Cookie(c.config.Cookie)
	if err == nil && cookie.Value != "" {
		// Verify token exists in store
		if _, ok := c.tokens.Load(cookie.Value); ok {
			return cookie.Value, nil
		}
	}

	// Generate new token
	token, err := c.generateToken()
	if err != nil {
		return "", err
	}

	// Store token
	c.tokens.Store(token, time.Now().Add(c.config.Expiry))

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     c.config.Cookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.config.Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(c.config.Expiry.Seconds()),
	})

	return token, nil
}

// Middleware provides CSRF protection
func (c *CSRF) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for safe methods
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Validate token for unsafe methods
		if err := c.validateRequest(r); err != nil {
			http.Error(w, "CSRF validation failed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Token gets or creates a CSRF token and returns it
func (c *CSRF) Token(w http.ResponseWriter, r *http.Request) string {
	token, _ := c.getOrCreateToken(w, r)
	return token
}

// validateRequest checks for a valid CSRF token in the request
func (c *CSRF) validateRequest(r *http.Request) error {
	// Get token from header or form
	token := r.Header.Get(c.config.Header)
	if token == "" {
		if err := r.ParseForm(); err == nil {
			token = r.FormValue(c.config.FieldName)
		}
	}

	if token == "" {
		return ErrTokenMissing
	}

	// Get cookie token
	cookie, err := r.Cookie(c.config.Cookie)
	if err != nil {
		return ErrTokenMissing
	}

	// Validate token matches cookie
	if token != cookie.Value {
		return ErrTokenInvalid
	}

	// Check token exists and hasn't expired
	if expiry, ok := c.tokens.Load(token); !ok {
		return ErrTokenInvalid
	} else if expiry.(time.Time).Before(time.Now()) {
		c.tokens.Delete(token)
		return ErrTokenInvalid
	}

	return nil
}

// Helper functions for handlers
func (c *CSRF) GetMeta(token string) template.HTML {
	return template.HTML(fmt.Sprintf(`<meta name="csrf-token" content="%s">`, token))
}

func (c *CSRF) Validate(w http.ResponseWriter, r *http.Request) bool {
	if err := c.validateRequest(r); err != nil {
		http.Error(w, "CSRF validation failed", http.StatusForbidden)
		return false
	}
	return true
}

// cleanup removes expired tokens
func (c *CSRF) cleanup() {
	c.tokens.Range(func(key, value interface{}) bool {
		if expiry := value.(time.Time); expiry.Before(time.Now()) {
			c.tokens.Delete(key)
		}
		return true
	})
}

func (c *CSRF) startCleanupLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	for range ticker.C {
		c.cleanup()
	}
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func (c *CSRF) MiddlewareExceptPaths(next http.Handler, excludePaths []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for excluded paths
		for _, path := range excludePaths {
			if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Apply CSRF middleware for all other paths
		c.Middleware(next).ServeHTTP(w, r)
	})
}
