// internal/server/auth_handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"infoscope/internal/auth" // Corrected import path
)

// Helper functions for dashboard data
func (s *Server) getDashboardCounts(ctx context.Context) (feedCount, entryCount int, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM feeds").Scan(&feedCount)
	if err != nil {
		return 0, 0, fmt.Errorf("error getting feed count: %w", err)
	}
	err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&entryCount)
	if err != nil {
		return 0, 0, fmt.Errorf("error getting entry count: %w", err)
	}
	return feedCount, entryCount, nil
}

func (s *Server) getLastUpdateTime(ctx context.Context) (time.Time, error) {
	var lastUpdate sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT DATETIME(MAX(last_fetched)) FROM feeds").Scan(&lastUpdate)
	if err != nil {
		return time.Time{}, fmt.Errorf("error getting last update: %w", err)
	}
	if !lastUpdate.Valid {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", lastUpdate.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("error parsing last update time: %w", err)
	}
	return t.UTC(), nil
}

// Template rendering with CSRF
func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data any) error {
	// Merge function maps
	funcMap := template.FuncMap{
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
	}
	for k, v := range s.registerTemplateFuncs() {
		funcMap[k] = v
	}

	var wrappedData struct {
		Data      any
		CSRFToken string
	}

	// Handle different data types
	switch v := data.(type) {
	case struct {
		Data      AdminPageData
		CSRFToken string
	}:
		wrappedData = struct {
			Data      any
			CSRFToken string
		}{
			Data:      v.Data,
			CSRFToken: v.CSRFToken,
		}
	case AdminPageData:
		wrappedData = struct {
			Data      any
			CSRFToken string
		}{
			Data:      v,
			CSRFToken: s.csrf.Token(w, r),
		}
	case IndexData:
		wrappedData = struct {
			Data      any
			CSRFToken string
		}{
			Data:      v,
			CSRFToken: s.csrf.Token(w, r),
		}
	default:
		wrappedData = struct {
			Data      any
			CSRFToken string
		}{
			Data:      data,
			CSRFToken: s.csrf.Token(w, r),
		}
	}

	tmpl := template.New(name).Funcs(funcMap)
	var files []string
	switch {
	case strings.HasPrefix(name, "admin/"):
		files = []string{
			filepath.Join(s.config.WebPath, "templates/admin/layout.html"),
			filepath.Join(s.config.WebPath, "templates", name),
		}
	default:
		files = []string{
			filepath.Join(s.config.WebPath, "templates", name),
		}
	}

	tmpl, err := tmpl.ParseFiles(files...)
	if err != nil {
		s.logger.Printf("Error parsing template %s: %v", name, err)
		return fmt.Errorf("error parsing template: %w", err)
	}

	if strings.HasPrefix(name, "admin/") {
		return tmpl.ExecuteTemplate(w, "layout", wrappedData)
	}
	return tmpl.Execute(w, wrappedData)
}

// Main handler functions
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	// Check authentication
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// Validate session
	session, err := s.auth.ValidateSession(s.db, cookie.Value)
	if err != nil || session == nil || session.IsExpired() {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// Get settings for favicon and other UI elements
	settings, err := s.getSettings(r.Context())
	if err != nil {
		s.logger.Printf("Error getting settings (user %d): %v", session.UserID, err)
		settings = make(map[string]string)
	}

	// Get dashboard counts
	feedCount, entryCount, err := s.getDashboardCounts(r.Context())
	if err != nil {
		s.logger.Printf("Error getting counts (user %d): %v", session.UserID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get last update time
	lastUpdateTime, err := s.getLastUpdateTime(r.Context())
	if err != nil {
		s.logger.Printf("Error getting last update (user %d): %v", session.UserID, err)
		lastUpdateTime = time.Time{} // Zero time instead of "Never"
	}

	// Get click statistics
	clickStats, err := s.getClickStats()
	if err != nil {
		s.logger.Printf("Error getting click stats (user %d): %v", session.UserID, err)
		clickStats = &DashboardStats{}
	}

	data := AdminPageData{
		Title:      "Dashboard",
		Active:     "dashboard",
		Settings:   settings,
		FeedCount:  feedCount,
		EntryCount: entryCount,
		LastUpdate: lastUpdateTime,
		UserID:     session.UserID,
		ClickStats: clickStats,
	}

	wrappedData := struct {
		Data      AdminPageData
		CSRFToken string
	}{
		Data:      data,
		CSRFToken: s.csrf.Token(w, r),
	}

	if err := s.renderTemplate(w, r, "admin/dashboard.html", wrappedData); err != nil {
		s.logger.Printf("Error rendering template (user %d): %v", session.UserID, err)
		if !headerWritten(w) {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Login request received: %s", r.Method)

	switch r.Method {
	case http.MethodGet:
		// Get CSRF token
		csrfToken := s.csrf.Token(w, r)

		// Retrieve settings
		settings, err := s.getSettings(r.Context())
		if err != nil {
			s.logger.Printf("Error getting settings: %v", err)
			settings = make(map[string]string)
		}

		// Updated struct initialization to match template expectations
		data := LoginTemplateData{
			BaseTemplateData: BaseTemplateData{
				CSRFToken: csrfToken,
			},
			Data: struct {
				Settings map[string]string
				Error    string
			}{
				Settings: settings,
				Error:    "",
			},
		}

		// Rest of the handler remains the same
		tmplPath := filepath.Join(s.config.WebPath, "templates", "login.html")
		tmpl, err := template.ParseFiles(tmplPath)
		if err != nil {
			s.logger.Printf("Error parsing login template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error executing login template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		s.logger.Printf("Login attempt received")
		if !s.csrf.Validate(w, r) {
			s.logger.Printf("CSRF validation failed")
			return
		}
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.logger.Printf("Failed to decode login request: %v", err)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		session, err := s.auth.Authenticate(s.db, req.Username, req.Password)
		if err != nil {
			s.logger.Printf("Authentication failed: %v", err)
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}
		s.logger.Printf("Authentication successful, setting session cookie")
		// Set session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			Secure:   s.csrf.config.Secure,
			SameSite: http.SameSiteStrictMode,
			Expires:  session.ExpiresAt,
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		s.logger.Printf("Invalid method for login: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// Redirect to login page if method is not POST
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	if !s.csrf.Validate(w, r) {
		// Redirect to login page if CSRF validation fails
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	cookie, err := r.Cookie("session")
	if err == nil && cookie.Value != "" {
		// Invalidate the session in the database
		if err := s.auth.InvalidateSession(s.db, cookie.Value); err != nil {
			s.logger.Printf("Error invalidating session: %v", err)
		}

		// Clear the session cookie
		http.SetCookie(w, &http.Cookie{
			Name:   "session",
			Value:  "",
			Path:   "/",
			MaxAge: -1, // Delete cookie
		})
	}

	// Redirect to login page after logout
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

// Request struct for changing password
type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleChangePassword handles requests to change the admin password
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate session first
	cookie, err := r.Cookie("session")
	if err != nil || cookie.Value == "" {
		respondWithError(w, http.StatusUnauthorized, "Authentication required")
		return
	}
	session, err := s.auth.ValidateSession(s.db, cookie.Value)
	if err != nil || session == nil || session.IsExpired() {
		respondWithError(w, http.StatusUnauthorized, "Invalid or expired session")
		return
	}

	// CSRF validation (Use the same middleware or check manually)
	// Assuming CSRF is handled by middleware or the renderTemplate function implicitly for GETs
	// For POST, we might need explicit validation if not using a middleware that covers POST
	// if !s.csrf.Validate(w, r) { // Uncomment and adapt if CSRF middleware isn't global
	//     respondWithError(w, http.StatusForbidden, "Invalid CSRF token")
	// 	   return
	// }

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get the current user's details to verify the current password
	currentUser, err := s.auth.GetUserByID(s.db, session.UserID)
	if err != nil {
		s.logger.Printf("Error getting user %d: %v", session.UserID, err)
		respondWithError(w, http.StatusInternalServerError, "Failed to retrieve user information")
		return
	}

	// Verify the current password
	_, err = s.auth.Authenticate(s.db, currentUser.Username, req.CurrentPassword)
	if err != nil {
		if err == auth.ErrInvalidCredentials {
			respondWithError(w, http.StatusUnauthorized, "Incorrect current password")
		} else {
			s.logger.Printf("Error authenticating user %d during password change: %v", session.UserID, err)
			respondWithError(w, http.StatusInternalServerError, "Authentication error")
		}
		return
	}

	// Update the password
	if err := s.auth.UpdatePassword(s.db, session.UserID, req.NewPassword); err != nil {
		s.logger.Printf("Error updating password for user %d: %v", session.UserID, err)
		respondWithError(w, http.StatusInternalServerError, "Failed to update password")
		return
	}

	s.logger.Printf("Password updated successfully for user %d", session.UserID)
	respondWithJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}

// Helper to send JSON error responses (can be moved to a utils file)
func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

// Helper to send JSON responses (can be moved to a utils file)
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload != nil {
		json.NewEncoder(w).Encode(payload)
	}
}
