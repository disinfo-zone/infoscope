// internal/server/backup_handler.go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type BackupData struct {
	Version    string            `json:"version"`
	ExportDate time.Time         `json:"exportDate"`
	Settings   map[string]string `json:"settings"`
	Feeds      []Feed            `json:"feeds"` // Uses the Feed struct from types.go
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	// Ensure user is authenticated
	if _, ok := getUserID(r.Context()); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleExport(w, r)
	case http.MethodPost:
		s.handleImport(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	// Create backup structure
	backup := BackupData{
		Version:    "1.0",
		ExportDate: time.Now(),
		Settings:   make(map[string]string),
		Feeds:      make([]Feed, 0),
	}

	// Get settings
	rows, err := s.db.QueryContext(r.Context(), "SELECT key, value FROM settings")
	if err != nil {
		s.logger.Printf("Error getting settings for backup: %v", err)
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
	rows, err = s.db.QueryContext(r.Context(), "SELECT url, title FROM feeds")
	if err != nil {
		s.logger.Printf("Error getting feeds for backup: %v", err)
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
		fmt.Sprintf("attachment; filename=infoscope_backup_%s.json",
			time.Now().Format("2006-01-02")))

	// Write JSON response
	if err := json.NewEncoder(w).Encode(backup); err != nil {
		s.logger.Printf("Error encoding backup: %v", err)
		http.Error(w, "Failed to create backup", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	// Validate CSRF
	if !s.csrf.Validate(w, r) {
		return
	}

	// Parse backup data
	var backup BackupData
	if err := json.NewDecoder(r.Body).Decode(&backup); err != nil {
		http.Error(w, "Invalid backup file", http.StatusBadRequest)
		return
	}

	// Start transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Printf("Error starting import transaction: %v", err)
		http.Error(w, "Failed to start import", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Update settings
	stmt, err := tx.PrepareContext(r.Context(),
		"INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)")
	if err != nil {
		s.logger.Printf("Error preparing settings statement: %v", err)
		http.Error(w, "Failed to prepare settings import", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for key, value := range backup.Settings {
		if _, err := stmt.ExecContext(r.Context(), key, value); err != nil {
			s.logger.Printf("Error importing setting %s: %v", key, err)
		}
	}

	// Import feeds
	for _, feed := range backup.Feeds {
		if feed.URL == "" {
			continue
		}
		_, err := tx.ExecContext(r.Context(),
			"INSERT OR IGNORE INTO feeds (url, title) VALUES (?, ?)",
			feed.URL, feed.Title)
		if err != nil {
			s.logger.Printf("Error importing feed %s: %v", feed.URL, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		s.logger.Printf("Error committing import: %v", err)
		http.Error(w, "Failed to complete import", http.StatusInternalServerError)
		return
	}

	// Trigger feed fetch for new feeds
	go func() {
		if err := s.feedService.UpdateFeeds(context.Background()); err != nil {
			s.logger.Printf("Error updating feeds after import: %v", err)
		}
	}()

	w.WriteHeader(http.StatusOK)
}
