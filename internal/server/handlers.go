// internal/server/handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"expvar"
	"fmt"
	"html/template"
	"infoscope/internal/feed"
	"net/http"
	"strconv"
	"time"
)

var (
	dbQueryCount    = expvar.NewInt("db_query_count")
	dbQueryDuration = expvar.NewFloat("db_query_duration_ms")
)

func (s *Server) dbMetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start).Milliseconds()
		dbQueryDuration.Add(float64(duration))
		dbQueryCount.Add(1)
	})
}

// Handler functions
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// create a context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

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

	// Get all settings
	settings := make(map[string]string)
	start := time.Now()
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	dbQueryCount.Add(1)
	dbQueryDuration.Add(float64(time.Since(start).Milliseconds()))
	if err != nil {
		s.logger.Printf("Error querying settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			s.logger.Printf("Error scanning setting: %v", err)
			continue
		}
		settings[key] = value
	}

	// Get max posts setting
	maxPosts := 33 // default
	if maxStr, ok := settings["max_posts"]; ok {
		if max, err := strconv.Atoi(maxStr); err == nil {
			maxPosts = max
		}
	}

	// Get entries with ID field
	rows, err = s.db.QueryContext(ctx, `
	WITH recent_entries AS (
		SELECT id, title, url, favicon_url, published_at,
			   ROW_NUMBER() OVER (ORDER BY published_at DESC) as rn
		FROM entries 
		WHERE published_at >= datetime('now', '-30 days')
	)
	SELECT id, title, url, favicon_url, published_at
	FROM recent_entries 
	WHERE rn <= ?
	ORDER BY published_at DESC
`, maxPosts)
	if err != nil {
		s.logger.Printf("Error querying entries: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []EntryView
	for rows.Next() {
		var entry EntryView
		var publishedAt time.Time
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.URL, &entry.FaviconURL, &publishedAt); err != nil {
			s.logger.Printf("Error scanning entry: %v", err)
			continue
		}
		entry.Date = publishedAt.Format("January 2, 2006")
		entries = append(entries, entry)
	}

	s.logger.Printf("Rendering index with %d entries", len(entries))

	data := IndexData{
		Title:             settings["site_title"],
		Entries:           entries,
		HeaderLinkURL:     settings["header_link_url"],
		HeaderLinkText:    settings["header_link_text"],
		FooterLinkURL:     settings["footer_link_url"],
		FooterLinkText:    settings["footer_link_text"],
		FooterImageURL:    settings["footer_image_url"],
		FooterImageHeight: settings["footer_image_height"],
		TrackingCode:      settings["tracking_code"],
	}

	// Create template with functions
	funcMap := template.FuncMap{
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
	}

	// Create and parse template with function map
	tmpl, err := template.New("index.html").Funcs(funcMap).ParseFiles("web/templates/index.html")
	if err != nil {
		s.logger.Printf("Error parsing template: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, data); err != nil {
		s.logger.Printf("Error executing template: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tmpl, err := template.ParseFiles(
			"web/templates/admin/layout.html",
			"web/templates/admin/settings.html",
		)
		if err != nil {
			s.logger.Printf("Error parsing settings templates: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		settings := make(map[string]string)
		rows, err := s.db.Query("SELECT key, value FROM settings")
		if err != nil {
			s.logger.Printf("Error querying settings: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				s.logger.Printf("Error scanning setting: %v", err)
				continue
			}
			settings[key] = value
		}

		data := SettingsTemplateData{
			Title:    "Settings",
			Active:   "settings",
			Settings: settings,
		}

		// Add CSRF token
		if csrfMeta, ok := getCSRFMeta(r.Context()); ok {
			data.CSRFMeta = csrfMeta
		}
		// Get CSRF token from cookie
		if cookie, err := r.Cookie("csrf_token"); err == nil {
			data.CSRFToken = cookie.Value
		}

		if err := tmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error executing template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}

	case http.MethodPost:
		var settings Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Add this right after the MethodPost case
		s.logger.Printf("Received POST request to /admin/feeds")
		s.logger.Printf("CSRF Token from header: %s", r.Header.Get("X-CSRF-Token"))
		s.logger.Printf("Content-Type: %s", r.Header.Get("Content-Type"))

		// Validate settings
		if settings.MaxPosts < 1 {
			http.Error(w, "Maximum posts must be at least 1", http.StatusBadRequest)
			return
		}
		if settings.UpdateInterval < 60 {
			http.Error(w, "Update interval must be at least 60 seconds", http.StatusBadRequest)
			return
		}

		// Update settings in database
		tx, err := s.db.Begin()
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		stmt, err := tx.Prepare("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)")
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer stmt.Close()

		// Update all settings
		updates := []struct {
			key, value string
		}{
			{"site_title", settings.SiteTitle},
			{"max_posts", strconv.Itoa(settings.MaxPosts)},
			{"update_interval", strconv.Itoa(settings.UpdateInterval)},
			{"header_link_text", settings.HeaderLinkText},
			{"header_link_url", settings.HeaderLinkURL},
			{"footer_link_text", settings.FooterLinkText},
			{"footer_link_url", settings.FooterLinkURL},
			{"footer_image_height", settings.FooterImageHeight},
			{"tracking_code", settings.TrackingCode},
		}

		// Only update footer_image_url if it's provided
		if settings.FooterImageURL != "" {
			updates = append(updates, struct{ key, value string }{
				"footer_image_url", settings.FooterImageURL,
			})
		}

		for _, update := range updates {
			if _, err := stmt.Exec(update.key, update.value); err != nil {
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLogin handles the login form submission
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// Serve login page
		tmpl, err := template.ParseFiles("web/templates/login.html")
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Create template data with CSRF meta
		data := struct {
			CSRFMeta template.HTML
		}{}

		// Get CSRF meta from context
		if csrfMeta, ok := getCSRFMeta(r.Context()); ok {
			data.CSRFMeta = csrfMeta
		}

		// Execute template with data
		if err := tmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error executing template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		session, err := s.auth.Authenticate(s.db, req.Username, req.Password)
		if err != nil {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Set session cookie
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			Expires:  session.ExpiresAt,
		})

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLogout handles user logout
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie("session")
	if err != nil {
		http.Error(w, "No session found", http.StatusUnauthorized)
		return
	}

	if err := s.auth.InvalidateSession(s.db, cookie.Value); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusOK)
}

// handles feed validation
func (s *Server) handleFeedValidation(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Feed validation request received: %s %s", r.Method, r.URL)

	if r.Method != http.MethodPost {
		s.logger.Printf("Invalid method: %s", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Verify Content-Type
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Printf("Error decoding request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	s.logger.Printf("Validating feed URL: %s", req.URL)
	result, err := feed.ValidateFeedURL(req.URL)
	if err != nil {
		s.logger.Printf("Feed validation error: %v", err)
		var status int
		var message string

		switch {
		case errors.Is(err, feed.ErrInvalidURL):
			status = http.StatusBadRequest
			message = "Invalid URL format"
		case errors.Is(err, feed.ErrTimeout):
			status = http.StatusGatewayTimeout
			message = "Feed took too long to respond"
		case errors.Is(err, feed.ErrNotAFeed):
			status = http.StatusBadRequest
			message = "URL does not point to a valid RSS/Atom feed"
		default:
			status = http.StatusInternalServerError
			message = "Failed to validate feed"
		}

		http.Error(w, message, status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		s.logger.Printf("Error encoding response: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// handleFeeds handles the feeds management page
func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Feeds request: %s %s", r.Method, r.URL.Path)
	s.logger.Printf("CSRF Token from header: %s", r.Header.Get("X-CSRF-Token"))
	s.logger.Printf("Content-Type: %s", r.Header.Get("Content-Type"))

	switch r.Method {
	case http.MethodGet:
		tmpl, err := template.ParseFiles(
			"web/templates/admin/layout.html",
			"web/templates/admin/feeds.html",
		)
		if err != nil {
			s.logger.Printf("Error parsing feeds templates: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Get all feeds from database
		rows, err := s.db.Query(`
            SELECT id, url, title, last_fetched 
            FROM feeds 
            ORDER BY title
        `)
		if err != nil {
			s.logger.Printf("Error querying feeds: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var feeds []struct {
			ID          int64
			URL         string
			Title       string
			LastFetched string
		}

		for rows.Next() {
			var feed struct {
				ID          int64
				URL         string
				Title       string
				LastFetched sql.NullTime
			}
			if err := rows.Scan(&feed.ID, &feed.URL, &feed.Title, &feed.LastFetched); err != nil {
				s.logger.Printf("Error scanning feed row: %v", err)
				continue
			}

			lastFetchedStr := "Never"
			if feed.LastFetched.Valid {
				lastFetchedStr = feed.LastFetched.Time.Format("January 2, 2006 15:04:05")
			}

			feeds = append(feeds, struct {
				ID          int64
				URL         string
				Title       string
				LastFetched string
			}{
				ID:          feed.ID,
				URL:         feed.URL,
				Title:       feed.Title,
				LastFetched: lastFetchedStr,
			})
		}

		data := struct {
			BaseTemplateData
			Title  string
			Active string
			Feeds  []struct {
				ID          int64
				URL         string
				Title       string
				LastFetched string
			}
		}{
			Title:  "Manage Feeds",
			Active: "feeds",
			Feeds:  feeds,
		}

		// Set CSRF Meta from context
		if csrfMeta, ok := getCSRFMeta(r.Context()); ok {
			data.CSRFMeta = csrfMeta
		}

		if err := tmpl.Execute(w, data); err != nil {
			s.logger.Printf("Error executing template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}

	case http.MethodPost:
		// Manual CSRF check for POST
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			s.logger.Printf("Missing CSRF token in POST request")
			http.Error(w, "Missing CSRF token", http.StatusForbidden)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil {
			s.logger.Printf("No CSRF cookie found: %v", err)
			http.Error(w, "Missing CSRF cookie", http.StatusForbidden)
			return
		}

		if !s.csrfManager.validateToken(token, cookie.Value) {
			s.logger.Printf("Invalid CSRF token. Header: %s, Cookie: %s", token, cookie.Value)
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		// Verify Content-Type
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			s.logger.Printf("Invalid Content-Type: %s", ct)
			http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
			return
		}

		// Decode request body
		var feedData struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&feedData); err != nil {
			s.logger.Printf("Error decoding feed data: %v", err)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate URL
		if feedData.URL == "" {
			http.Error(w, "Feed URL is required", http.StatusBadRequest)
			return
		}

		// Add feed
		if err := s.feedService.AddFeed(feedData.URL); err != nil {
			s.logger.Printf("Error adding feed: %v", err)
			http.Error(w, fmt.Sprintf("Failed to add feed: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case http.MethodDelete:
		// Manual CSRF check for DELETE
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			s.logger.Printf("Missing CSRF token in DELETE request")
			http.Error(w, "Missing CSRF token", http.StatusForbidden)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil {
			s.logger.Printf("No CSRF cookie found: %v", err)
			http.Error(w, "Missing CSRF cookie", http.StatusForbidden)
			return
		}

		if !s.csrfManager.validateToken(token, cookie.Value) {
			s.logger.Printf("Invalid CSRF token. Header: %s, Cookie: %s", token, cookie.Value)
			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
			return
		}

		// Get feed ID from query parameter
		feedID := r.URL.Query().Get("id")
		if feedID == "" {
			http.Error(w, "Missing feed ID", http.StatusBadRequest)
			return
		}

		// Parse and validate feed ID
		id, err := strconv.ParseInt(feedID, 10, 64)
		if err != nil {
			s.logger.Printf("Invalid feed ID: %v", err)
			http.Error(w, "Invalid feed ID", http.StatusBadRequest)
			return
		}

		// Delete feed
		if err := s.feedService.DeleteFeed(id); err != nil {
			s.logger.Printf("Error deleting feed: %v", err)
			http.Error(w, "Failed to delete feed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Handle Metrics

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	metrics := map[string]interface{}{
		"query_count":       dbQueryCount.String(),
		"query_duration_ms": dbQueryDuration.String(),
	}

	json.NewEncoder(w).Encode(metrics)
}
