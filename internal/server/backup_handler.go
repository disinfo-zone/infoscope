// internal/server/backup_handler.go
package server

import (
	"encoding/json"
	"net/http"
	"time"
)

type BackupData struct {
	Version    string            `json:"version"`
	ExportDate time.Time         `json:"exportDate"`
	Settings   map[string]string `json:"settings"`
	Feeds      []Feed            `json:"feeds"`
}

type Feed struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Export data
		backup := BackupData{
			Version:    "1.0",
			ExportDate: time.Now(),
			Settings:   make(map[string]string),
			Feeds:      make([]Feed, 0),
		}

		// Get settings
		rows, err := s.db.Query("SELECT key, value FROM settings")
		if err != nil {
			s.logger.Printf("Error getting settings: %v", err)
			http.Error(w, "Failed to export settings", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				s.logger.Printf("Error scanning setting: %v", err)
				continue
			}
			backup.Settings[key] = value
		}

		// Get feeds
		rows, err = s.db.Query("SELECT url, title FROM feeds")
		if err != nil {
			s.logger.Printf("Error getting feeds: %v", err)
			http.Error(w, "Failed to export feeds", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var feed Feed
			if err := rows.Scan(&feed.URL, &feed.Title); err != nil {
				s.logger.Printf("Error scanning feed: %v", err)
				continue
			}
			backup.Feeds = append(backup.Feeds, feed)
		}

		// Set headers for file download
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition",
			"attachment; filename=infoscope_backup_"+time.Now().Format("2006-01-02")+".json")

		// Write JSON response
		if err := json.NewEncoder(w).Encode(backup); err != nil {
			s.logger.Printf("Error encoding backup: %v", err)
			http.Error(w, "Failed to create backup", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		// Import data
		var backup BackupData
		if err := json.NewDecoder(r.Body).Decode(&backup); err != nil {
			http.Error(w, "Invalid backup file", http.StatusBadRequest)
			return
		}

		// Start transaction
		tx, err := s.db.Begin()
		if err != nil {
			http.Error(w, "Failed to start import", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// Update settings
		stmt, err := tx.Prepare("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)")
		if err != nil {
			http.Error(w, "Failed to prepare settings import", http.StatusInternalServerError)
			return
		}
		defer stmt.Close()

		for key, value := range backup.Settings {
			if _, err := stmt.Exec(key, value); err != nil {
				s.logger.Printf("Error importing setting %s: %v", key, err)
			}
		}

		// Import feeds
		for _, feed := range backup.Feeds {
			if feed.URL == "" {
				continue
			}
			_, err := tx.Exec(
				"INSERT OR IGNORE INTO feeds (url, title) VALUES (?, ?)",
				feed.URL, feed.Title,
			)
			if err != nil {
				s.logger.Printf("Error importing feed %s: %v", feed.URL, err)
			}
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, "Failed to complete import", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	}
}
