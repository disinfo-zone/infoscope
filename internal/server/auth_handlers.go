// internal/server/auth_handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
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
	tmpl, ok := s.templateCache[name]
	if !ok {
		s.logger.Printf("Error: Template %s not found in cache", name)
		return fmt.Errorf("template %s not found in cache", name)
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

	// Choose the appropriate execution method based on template type (admin vs non-admin)
	// This logic is based on how templates were parsed and named in LoadTemplates.
	// Admin templates are parsed with layout and are expected to be executed via "layout" definition.
	// Non-admin templates are parsed standalone and executed directly.
	if strings.HasPrefix(name, "admin/") {
		// Ensure the "layout" template is defined within the cached admin template set
		// This is typically true if adminLayoutPath defines `{{define "layout"}}...{{end}}`
		// or if the layout file was the primary file parsed in a specific way.
		// Given LoadTemplates structure: `template.New(templateName).ParseFiles(path, adminLayoutPath)`
		// and `ExecuteTemplate(w, "layout", ...)`, "layout" must be a defined template name
		// within the set, usually from adminLayoutPath.
		return tmpl.ExecuteTemplate(w, "layout.html", wrappedData) // Assuming layout defines "layout.html"
	}
	// For non-admin templates, tmpl.Execute will execute the template named `templateName`
	// which was used in `template.New(templateName)` during LoadTemplates.
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
	if !s.config.ProductionMode {
		s.logger.Printf("Login request received: %s", r.Method)
	}

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

		// Use the refactored renderTemplate
		if err := s.renderTemplate(w, r, "login.html", data); err != nil {
			s.logger.Printf("Error rendering login template: %v", err)
			// Ensure a response is written if renderTemplate fails before writing headers
			if !headerWritten(w) {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
			return
		}

	case http.MethodPost:
		if !s.config.ProductionMode {
			s.logger.Printf("Login attempt received")
		}
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
		if !s.config.ProductionMode {
			s.logger.Printf("Authentication successful, setting session cookie")
		}
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
	if !s.csrf.Validate(w, r) {
		s.logger.Printf("CSRF validation failed for password change request") // Optional: log the failure
		return
	}

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

	if !s.config.ProductionMode {
		s.logger.Printf("Password updated successfully for user %d", session.UserID)
	}
	RespondWithJSON(w, http.StatusOK, map[string]string{"message": "Password updated successfully"})
}
