// internal/server/auth_handlers.go
package server

import (
	"database/sql"
	"html/template"
	"net/http"
	"time"
)

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	userID, ok := getUserID(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get feed count
	var feedCount int
	err := s.db.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&feedCount)
	if err != nil {
		s.logger.Printf("Error getting feed count (user %d): %v", userID, err)
		feedCount = 0
	}

	// Get entry count
	var entryCount int
	err = s.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&entryCount)
	if err != nil {
		s.logger.Printf("Error getting entry count (user %d): %v", userID, err)
		entryCount = 0
	}

	// Get last update time using properly formatted datetime
	var lastUpdate sql.NullString
	err = s.db.QueryRow("SELECT DATETIME(MAX(last_fetched)) FROM feeds").Scan(&lastUpdate)
	if err != nil {
		s.logger.Printf("Error getting last update (user %d): %v", userID, err)
	}

	// Format the last update time
	var lastUpdateStr string
	if lastUpdate.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", lastUpdate.String); err == nil {
			lastUpdateStr = t.Format("January 2, 2006 15:04:05")
		} else {
			lastUpdateStr = "Never"
			s.logger.Printf("Error parsing last update time: %v", err)
		}
	} else {
		lastUpdateStr = "Never"
	}

	// Get click statistics
	clickStats, err := s.getClickStats()
	if err != nil {
		s.logger.Printf("Error getting click stats (user %d): %v", userID, err)
		clickStats = &DashboardStats{} // Empty dashboard stats if there's an error
	}

	data := AdminPageData{
		Title:      "Dashboard",
		Active:     "dashboard",
		FeedCount:  feedCount,
		EntryCount: entryCount,
		LastUpdate: lastUpdateStr,
		UserID:     userID,
		ClickStats: clickStats,
	}

	// Set CSRF Meta
	if csrfMeta, ok := getCSRFMeta(r.Context()); ok {
		data.CSRFMeta = csrfMeta
	}

	tmpl, err := template.ParseFiles(
		"web/templates/admin/layout.html",
		"web/templates/admin/dashboard.html",
	)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Printf("Error executing template (user %d): %v", userID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
