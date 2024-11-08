// internal/server/auth_handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"
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

func (s *Server) getLastUpdateTime(ctx context.Context) (string, error) {
	var lastUpdate sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT DATETIME(MAX(last_fetched)) FROM feeds").Scan(&lastUpdate)
	if err != nil {
		return "Never", fmt.Errorf("error getting last update: %w", err)
	}
	if !lastUpdate.Valid {
		return "Never", nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", lastUpdate.String)
	if err != nil {
		return "Never", fmt.Errorf("error parsing last update time: %w", err)
	}
	return t.Format("January 2, 2006 15:04:05"), nil
}

// Template rendering with CSRF
func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data any) error {
	funcMap := template.FuncMap{
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
	}

	wrappedData := struct {
		CSRFToken string
		Data      any
	}{
		CSRFToken: s.csrf.Token(w, r),
		Data:      data,
	}

	tmpl := template.New(name)
	tmpl = tmpl.Funcs(funcMap)

	var files []string
	switch {
	case strings.HasPrefix(name, "admin/"):
		files = []string{
			filepath.Join(s.config.WebPath, "templates/admin/layout.html"),
			filepath.Join(s.config.WebPath, "templates", name),
		}
	case name == "404.html":
		files = []string{
			filepath.Join(s.config.WebPath, "templates/404.html"),
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

	baseName := path.Base(name)
	if strings.HasPrefix(name, "admin/") {
		return tmpl.ExecuteTemplate(w, "layout", wrappedData)
	}
	return tmpl.ExecuteTemplate(w, baseName, wrappedData)
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

	// Proceed to render the admin dashboard
	userID := session.UserID

	// Get dashboard counts
	feedCount, entryCount, err := s.getDashboardCounts(r.Context())
	if err != nil {
		s.logger.Printf("Error getting counts (user %d): %v", userID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Get last update time
	lastUpdateStr, err := s.getLastUpdateTime(r.Context())
	if err != nil {
		s.logger.Printf("Error getting last update (user %d): %v", userID, err)
		lastUpdateStr = "Never"
	}

	// Get click statistics
	clickStats, err := s.getClickStats()
	if err != nil {
		s.logger.Printf("Error getting click stats (user %d): %v", userID, err)
		clickStats = &DashboardStats{} // Initialize with empty stats
	}

	// Prepare data for the template
	data := struct {
		CSRFToken  string
		Title      string
		Active     string
		FeedCount  int
		EntryCount int
		LastUpdate string
		UserID     int64
		ClickStats *DashboardStats
	}{
		CSRFToken:  s.csrf.Token(w, r),
		Title:      "Dashboard",
		Active:     "dashboard",
		FeedCount:  feedCount,
		EntryCount: entryCount,
		LastUpdate: lastUpdateStr,
		UserID:     userID,
		ClickStats: clickStats,
	}

	// Render the template
	if err := s.renderTemplate(w, r, "admin/dashboard.html", data); err != nil {
		s.logger.Printf("Error rendering template (user %d): %v", userID, err)
		if !headerWritten(w) {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Login request received: %s", r.Method)

	switch r.Method {
	case http.MethodGet:
		// Get CSRF token
		csrfToken := s.csrf.Token(w, r)

		data := struct {
			CSRFToken string
			Data      struct {
				Error string
			}
		}{
			CSRFToken: csrfToken,
		}

		// Parse and execute template
		tmpl, err := template.ParseFiles("web/templates/login.html")
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
			Name:     "session",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   s.csrf.config.Secure,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
	}

	// Redirect to login page after logout
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
