// internal/server/setup.go

package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
)

func IsFirstRun(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("error checking admin users: %w", err)
	}
	return count == 0, nil
}

const (
	WebRoot = "web"
)

type setupRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
	SiteTitle       string `json:"siteTitle"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Setup handler called: %s %s", r.Method, r.URL.Path)
	switch r.Method {
	case http.MethodGet:
		// Get CSRF token
		csrfToken := s.csrf.Token(w, r)

		// Get settings
		settings, err := s.getSettings(r.Context())
		if err != nil {
			s.logger.Printf("Error getting settings: %v", err)
			settings = make(map[string]string)
		}

		// Copy the exact structure that works in login handler
		data := SetupTemplateData{
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

		// Parse and execute template directly like login handler does
		tmplPath := filepath.Join(s.config.WebPath, "templates", "setup.html")
		tmpl, err := template.ParseFiles(tmplPath)
		if err != nil {
			s.logger.Printf("Error parsing setup template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error executing setup template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		if !s.csrf.Validate(w, r) {
			return
		}

		var req setupRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Validate input
		if req.Username == "" || req.Password == "" {
			http.Error(w, "Username and password are required", http.StatusBadRequest)
			return
		}
		if req.Password != req.ConfirmPassword {
			http.Error(w, "Passwords do not match", http.StatusBadRequest)
			return
		}
		if len(req.Password) < 8 {
			http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
			return
		}

		// Create admin user
		if err := s.auth.CreateUser(s.db, req.Username, req.Password); err != nil {
			s.logger.Printf("Failed to create user: %v", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}

		// Set site title if provided
		if req.SiteTitle != "" {
			_, err := s.db.Exec(
				"INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
				"site_title", req.SiteTitle,
			)
			if err != nil {
				s.logger.Printf("Failed to set site title: %v", err)
			}
		}

		// Return success
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
