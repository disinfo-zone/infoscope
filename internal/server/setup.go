// internal/server/setup.go

package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"infoscope/internal/auth"
	"net/http"
	"os"
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
	// Add debug logging
	s.logger.Printf("Setup handler called: %s %s", r.Method, r.URL.Path)

	isFirstRun, err := IsFirstRun(s.db)
	if err != nil {
		s.logger.Printf("Error checking first run: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if !isFirstRun {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Get CSRF token
		csrfToken := s.csrf.Token(w, r)

		// Check working directory and template path
		wd, _ := os.Getwd()
		templatePath := filepath.Join(WebRoot, "templates", "setup.html")
		s.logger.Printf("Working directory: %s, looking for template: %s", wd, templatePath)

		if _, err := os.Stat(templatePath); os.IsNotExist(err) {
			s.logger.Printf("Template not found: %s", templatePath)
			http.Error(w, "Template file not found", http.StatusInternalServerError)
			return
		}

		data := struct {
			CSRFToken string
		}{
			CSRFToken: csrfToken,
		}

		if err := s.renderTemplate(w, r, "setup.html", data); err != nil {
			s.logger.Printf("Error rendering setup template: %v", err)
			http.Error(w, "Error rendering template", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		// Validate CSRF token
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
		if err := auth.CreateUser(s.db, req.Username, req.Password); err != nil {
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
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
