// internal/server/setup.go
package server

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"

	"infoscope/internal/auth"
)

// IsFirstRun checks if there are any admin users in the system
func IsFirstRun(db *sql.DB) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

type setupRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
	SiteTitle       string `json:"siteTitle"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	// Check if setup is needed
	isFirstRun, err := IsFirstRun(s.db)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// If setup is already complete, redirect to login
	if !isFirstRun {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Serve setup page
		tmpl, err := template.ParseFiles("web/templates/setup.html")
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, nil)

	case http.MethodPost:
		// Handle setup submission
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
				// Log error but don't fail setup
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
