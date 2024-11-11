// internal/server/handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"expvar"
	"fmt"
	"infoscope/internal/feed"
	"net/http"
	"strconv"
	"time"
)

// Metrics variables
var (
	dbQueryCount    = expvar.NewInt("db_query_count")
	dbQueryDuration = expvar.NewFloat("db_query_duration_ms")
)

// Database helper methods for the Server struct
func (s *Server) getSettings(ctx context.Context) (map[string]string, error) {
	settings := make(map[string]string)
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, rows.Err()
}

func (s *Server) getRecentEntries(ctx context.Context, limit int) ([]EntryView, error) {
	// Add debug logging
	s.logger.Printf("Getting recent entries with limit: %d", limit)

	rows, err := s.db.QueryContext(ctx, `
        SELECT 
            e.id,
            e.title,
            e.url,
            e.favicon_url,
            datetime(e.published_at) as date
        FROM entries e
        JOIN feeds f ON e.feed_id = f.id
        WHERE f.status != 'deleted' 
        ORDER BY e.published_at DESC
        LIMIT ?
    `, limit)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	var entries []EntryView
	for rows.Next() {
		var e EntryView
		var dateStr string
		if err := rows.Scan(&e.ID, &e.Title, &e.URL, &e.FaviconURL, &dateStr); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}
		// Parse the date string
		if date, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
			e.Date = date.Format("Jan 02")
		}
		entries = append(entries, e)
	}

	// Add debug logging
	s.logger.Printf("Found %d entries in query", len(entries))
	if len(entries) > 0 {
		s.logger.Printf("Sample entry: %+v", entries[0])
	}

	return entries, rows.Err()
}

func (s *Server) getFeeds(ctx context.Context) ([]Feed, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, url, title, datetime(last_fetched)
        FROM feeds
        ORDER BY title
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var f Feed
		var lastFetchedStr sql.NullString
		if err := rows.Scan(&f.ID, &f.URL, &f.Title, &lastFetchedStr); err != nil {
			return nil, err
		}
		if lastFetchedStr.Valid {
			if date, err := time.Parse("2006-01-02 15:04:05", lastFetchedStr.String); err == nil {
				f.LastFetched = date
			}
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

func (s *Server) updateSettings(ctx context.Context, settings Settings) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT OR REPLACE INTO settings (key, value, type) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	updates := map[string]struct {
		value string
		type_ string
	}{
		"site_title":          {settings.SiteTitle, "string"},
		"max_posts":           {strconv.Itoa(settings.MaxPosts), "int"},
		"update_interval":     {strconv.Itoa(settings.UpdateInterval), "int"},
		"header_link_text":    {settings.HeaderLinkText, "string"},
		"header_link_url":     {settings.HeaderLinkURL, "string"},
		"footer_link_text":    {settings.FooterLinkText, "string"},
		"footer_link_url":     {settings.FooterLinkURL, "string"},
		"footer_image_height": {settings.FooterImageHeight, "string"},
		"footer_image_url":    {settings.FooterImageURL, "string"},
		"tracking_code":       {settings.TrackingCode, "string"},
		"favicon_url":         {settings.FaviconURL, "string"},
		"timezone":            {settings.Timezone, "string"},
		"meta_description":    {settings.MetaDescription, "string"},
		"meta_image_url":      {settings.MetaImageURL, "string"},
	}

	for key, setting := range updates {
		if _, err := stmt.ExecContext(ctx, key, setting.value, setting.type_); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// HTTP Handlers
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Debug logging
	s.logger.Printf("Starting handleIndex...")

	// Check if setup is needed
	isFirstRun, err := IsFirstRun(s.db)
	if err != nil {
		s.logger.Printf("Error checking first run: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if isFirstRun {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	// Get CSRF token
	csrfToken := s.csrf.Token(w, r)

	// Get settings with debug
	settings, err := s.getSettings(r.Context())
	if err != nil {
		s.logger.Printf("Error getting settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	s.logger.Printf("Retrieved settings: %+v", settings)

	// Get max posts setting
	maxPosts := 33 // default
	if maxStr, ok := settings["max_posts"]; ok {
		if max, err := strconv.Atoi(maxStr); err == nil {
			maxPosts = max
		}
	}
	s.logger.Printf("Using maxPosts: %d", maxPosts)

	// Debug database state
	var feedCount, entryCount int
	err = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM feeds").Scan(&feedCount)
	if err != nil {
		s.logger.Printf("Error counting feeds: %v", err)
	}
	err = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM entries").Scan(&entryCount)
	if err != nil {
		s.logger.Printf("Error counting entries: %v", err)
	}
	s.logger.Printf("Database state: %d feeds, %d entries", feedCount, entryCount)

	// Get entries with debug
	entries, err := s.getRecentEntries(r.Context(), maxPosts)
	if err != nil {
		s.logger.Printf("Error getting entries: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	s.logger.Printf("Retrieved %d entries", len(entries))

	// Sample entry logging
	if len(entries) > 0 {
		s.logger.Printf("Sample entry: %+v", entries[0])
	}

	data := IndexData{
		BaseTemplateData: BaseTemplateData{
			CSRFToken: csrfToken,
		},
		Title:             settings["site_title"],
		Entries:           entries,
		HeaderLinkURL:     settings["header_link_url"],
		HeaderLinkText:    settings["header_link_text"],
		FooterLinkURL:     settings["footer_link_url"],
		FooterLinkText:    settings["footer_link_text"],
		FooterImageURL:    settings["footer_image_url"],
		FooterImageHeight: settings["footer_image_height"],
		TrackingCode:      settings["tracking_code"],
		Settings:          settings,
		SiteURL:           settings["site_url"],
	}

	s.logger.Printf("Rendering template with data: %+v", data)

	if err := s.renderTemplate(w, r, "index.html", data); err != nil {
		s.logger.Printf("Error rendering template: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.logger.Printf("handleIndex completed successfully")
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	csrfToken := s.csrf.Token(w, r)
	switch r.Method {
	case http.MethodGet:
		settings, err := s.getSettings(r.Context())
		if err != nil {
			s.logger.Printf("Error getting settings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		data := SettingsTemplateData{
			BaseTemplateData: BaseTemplateData{
				CSRFToken: csrfToken,
			},
			Title:    "Settings",
			Active:   "settings",
			Settings: settings,
		}

		if err := s.renderTemplate(w, r, "admin/settings.html", data); err != nil {
			s.logger.Printf("Error rendering settings template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		if !s.csrf.Validate(w, r) {
			return
		}

		var settings Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := s.updateSettings(r.Context(), settings); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handles feed validation
func (s *Server) handleFeedValidation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.csrf.Validate(w, r) {
		return
	}

	var req struct {
		URL       string `json:"url"`
		CSRFToken string `json:"csrf_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate the feed URL
	validationResult, err := feed.ValidateFeedURL(req.URL)
	if err != nil {
		s.logger.Printf("Feed validation failed for %s: %v", req.URL, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Return validation result
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(validationResult); err != nil {
		s.logger.Printf("Error encoding validation response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

// handleFeeds handles the feeds management page
func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	csrfToken := s.csrf.Token(w, r)
	switch r.Method {
	case http.MethodGet:
		feeds, err := s.getFeeds(r.Context())
		if err != nil {
			s.logger.Printf("Error getting feeds: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		settings, err := s.getSettings(r.Context())
		if err != nil {
			s.logger.Printf("Error getting settings: %v", err)
			settings = make(map[string]string)
		}

		data := AdminPageData{
			BaseTemplateData: BaseTemplateData{
				CSRFToken: csrfToken,
			},
			Title:    "Manage Feeds",
			Active:   "feeds",
			Settings: settings,
			Feeds:    feeds,
		}

		if err := s.renderTemplate(w, r, "admin/feeds.html", data); err != nil {
			s.logger.Printf("Error rendering feeds template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		if !s.csrf.Validate(w, r) {
			return
		}

		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := s.feedService.AddFeed(req.URL); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		if !s.csrf.Validate(w, r) {
			return
		}

		var req struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := s.feedService.DeleteFeed(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Handle Metrics

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only allow authenticated users
	if _, ok := getUserID(r.Context()); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	metrics := map[string]interface{}{
		"query_count":       dbQueryCount.String(),
		"query_duration_ms": dbQueryDuration.String(),
	}

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		s.logger.Printf("Error encoding metrics: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}
