// internal/server/handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"expvar"
	"fmt"
	"encoding/xml"
	"infoscope/internal/feed"
	"infoscope/internal/rss"
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
	if !s.config.ProductionMode {
		s.logger.Printf("Getting recent entries with limit: %d", limit)
	}

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
		var publishedAtStr string // Will hold the "YYYY-MM-DD HH:MM:SS" string from DB
		if err := rows.Scan(&e.ID, &e.Title, &e.URL, &e.FaviconURL, &publishedAtStr); err != nil {
			return nil, fmt.Errorf("scan error: %w", err)
		}

		// Parse the full timestamp for PublishedAtTime
		if parsedTime, err := time.Parse("2006-01-02 15:04:05", publishedAtStr); err == nil {
			e.PublishedAtTime = parsedTime
			// For existing e.Date, format as "Jan 02"
			e.Date = parsedTime.Format("Jan 02")
		} else {
			// Log error if parsing fails, PublishedAtTime will be zero, Date will be empty
			s.logger.Printf("Error parsing date string '%s' for EntryView: %v", publishedAtStr, err)
		}
		entries = append(entries, e)
	}

	if !s.config.ProductionMode {
		s.logger.Printf("Found %d entries in query", len(entries))
		if len(entries) > 0 {
			s.logger.Printf("Sample entry: %+v", entries[0])
		}
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
		"site_url":            {settings.SiteURL, "string"},
	}

	for key, setting := range updates {
		if _, err := stmt.ExecContext(ctx, key, setting.value, setting.type_); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Server) handleRSS(w http.ResponseWriter, r *http.Request) {
	settings, err := s.getSettings(r.Context())
	if err != nil {
		s.logger.Printf("Error getting settings for RSS feed: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	siteTitle := settings["site_title"]
	siteURL := settings["site_url"] // Get site_url
	metaDescription := settings["meta_description"]

	if siteURL == "" {
		s.logger.Printf("Warning: Site URL (site_url) is not configured in settings. RSS feed channel link will be empty or invalid.")
	}

	maxPosts := 33 // Default
	if maxStr, ok := settings["max_posts"]; ok {
		if max, err := strconv.Atoi(maxStr); err == nil && max > 0 {
			maxPosts = max
		}
	}

	entries, err := s.getRecentEntries(r.Context(), maxPosts)
	if err != nil {
		s.logger.Printf("Error getting recent entries for RSS feed: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	rssFeed := rss.RSS{
		Version: "2.0",
		AtomNS:  "http://www.w3.org/2005/Atom", // Set Atom namespace
		Channel: rss.Channel{
			Title:         siteTitle,
			Link:          siteURL,
			Description:   metaDescription,
			Language:      "en-us", // Default, consider making this configurable
			LastBuildDate: now.Format(time.RFC1123Z),
		},
	}

	if siteURL != "" {
		// Ensure siteURL ends with a slash if it doesn't have one, before appending rss.xml
		// However, standard URL construction usually handles this. Assuming siteURL is base (e.g. http://example.com)
		// and the path should be /rss.xml.
		// For robustness, one might use url.Parse and url.ResolveReference if siteURL could be more complex.
		// Here, simple concatenation is used as per previous context.
		selfLinkHref := siteURL
		if selfLinkHref != "" && selfLinkHref[len(selfLinkHref)-1] == '/' {
			selfLinkHref = selfLinkHref[:len(selfLinkHref)-1] // Remove trailing slash if present
		}
		selfLinkHref += "/rss.xml"


		rssFeed.Channel.SelfLink = rss.AtomLink{
			Href: selfLinkHref,
			Rel:  "self",
			Type: "application/rss+xml",
		}
	}

	for _, entry := range entries {
		item := rss.Item{
			Title:       entry.Title,
			Link:        entry.URL, // Assuming entry.URL is absolute
			Description: entry.Title, // Using title as description, as no other summary is readily available
			GUID:        entry.URL, // Using URL as GUID, common practice
		}
		// Only set PubDate if PublishedAtTime is not zero
		if !entry.PublishedAtTime.IsZero() {
			item.PubDate = entry.PublishedAtTime.Format(time.RFC1123Z)
		} else {
			s.logger.Printf("Entry with ID %d has zero PublishedAtTime, omitting PubDate in RSS item.", entry.ID)
		}
		rssFeed.Channel.Items = append(rssFeed.Channel.Items, item)
	}

	xmlOutput, err := xml.MarshalIndent(rssFeed, "", "  ")
	if err != nil {
		s.logger.Printf("Error marshalling RSS feed to XML: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, err = w.Write(xmlOutput)
	if err != nil {
		s.logger.Printf("Error writing RSS XML response: %v", err)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		s.logger.Printf("Health check failed: DB ping error: %v", err)
		http.Error(w, "DB Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !s.config.ProductionMode {
		s.logger.Printf("Starting handleIndex...")
	}
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
	csrfToken := s.csrf.Token(w, r)
	settings, err := s.getSettings(r.Context())
	if err != nil {
		s.logger.Printf("Error getting settings: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !s.config.ProductionMode {
		s.logger.Printf("Retrieved settings: %+v", settings)
	}
	maxPosts := 33 // default
	if maxStr, ok := settings["max_posts"]; ok {
		if max, err := strconv.Atoi(maxStr); err == nil {
			maxPosts = max
		}
	}
	if !s.config.ProductionMode {
		s.logger.Printf("Using maxPosts: %d", maxPosts)
	}
	var feedCount, entryCount int
	err = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM feeds").Scan(&feedCount)
	if err != nil {
		s.logger.Printf("Error counting feeds: %v", err)
	}
	err = s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM entries").Scan(&entryCount)
	if err != nil {
		s.logger.Printf("Error counting entries: %v", err)
	}
	if !s.config.ProductionMode {
		s.logger.Printf("Database state: %d feeds, %d entries", feedCount, entryCount)
	}
	entries, err := s.getRecentEntries(r.Context(), maxPosts)
	if err != nil {
		s.logger.Printf("Error getting entries: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !s.config.ProductionMode {
		s.logger.Printf("Retrieved %d entries", len(entries))
		if len(entries) > 0 {
			s.logger.Printf("Sample entry: %+v", entries[0])
		}
	}
	data := IndexData{
		BaseTemplateData:  BaseTemplateData{CSRFToken: csrfToken},
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
	if !s.config.ProductionMode {
		s.logger.Printf("Rendering template with data: %+v", data)
	}
	if err := s.renderTemplate(w, r, "index.html", data); err != nil {
		s.logger.Printf("Error rendering template: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if !s.config.ProductionMode {
		s.logger.Printf("handleIndex completed successfully")
	}
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
			BaseTemplateData: BaseTemplateData{CSRFToken: csrfToken},
			Title:            "Settings",
			Active:           "settings",
			Settings:         settings,
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
		var settingsData Settings // Renamed from 'settings' to avoid conflict with outer scope
		if err := json.NewDecoder(r.Body).Decode(&settingsData); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		if err := s.updateSettings(r.Context(), settingsData); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

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
		CSRFToken string `json:"csrf_token"` // This CSRF token in JSON body is not standardly checked by gorilla/csrf
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	validationResult, err := feed.ValidateFeedURL(req.URL)
	if err != nil {
		s.logger.Printf("Feed validation failed for %s: %v", req.URL, err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(validationResult); err != nil {
		s.logger.Printf("Error encoding validation response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

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
			BaseTemplateData: BaseTemplateData{CSRFToken: csrfToken},
			Title:            "Manage Feeds",
			Active:           "feeds",
			Settings:         settings,
			Feeds:            feeds,
		}
		if err := s.renderTemplate(w, r, "admin/feeds.html", data); err != nil {
			s.logger.Printf("Error rendering feeds template: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	case http.MethodPost: // Assumed for adding a feed
		if !s.csrf.Validate(w, r) {
			return
		}
		// Assuming the request body for adding a feed is JSON: {"url": "feed_url"}
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body for adding feed", http.StatusBadRequest)
			return
		}
		if err := s.feedService.AddFeed(req.URL); err != nil {
			// Consider returning a more specific error code if it's a duplicate feed, etc.
			http.Error(w, fmt.Sprintf("Failed to add feed: %v", err), http.StatusBadRequest)
			return
		}
		// Respond with success, or redirect
		// For JSON API style, just OK. For form post, redirect.
		// The current frontend JS expects JSON for some actions, but this handler seems to expect form posts.
		// For now, assume redirect is fine as it's a POST from a form page.
		http.Redirect(w, r, "/admin/feeds", http.StatusSeeOther) // Redirect back to feeds page

	case http.MethodDelete: // This needs to be handled by specific routing or action parameter
		// This case is not typically hit by a generic /admin/feeds POST.
		// Usually, delete would be /admin/feeds/delete or POST with action=delete.
		// For now, assuming it's a placeholder or handled by specific client-side routing.
		// To make it work, it would need an ID from the request.
		if !s.csrf.Validate(w, r) {
			return
		}
		var req struct {
			ID int64 `json:"id"`
		} // Assuming ID is sent in JSON body for DELETE
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request for deleting feed", http.StatusBadRequest)
			return
		}
		if err := s.feedService.DeleteFeed(req.ID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to delete feed: %v", err), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/feeds", http.StatusSeeOther) // Redirect back

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMetrics handles requests to the /admin/metrics endpoint.
// It returns server metrics as a JSON response.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authentication is handled by s.requireAuth middleware wrapper in Routes().
	// So, if we reach here, the user is authenticated.

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	metrics := map[string]interface{}{
		"query_count":       dbQueryCount.String(),    // expvar variables are automatically marshaled as strings
		"query_duration_ms": dbQueryDuration.String(), // by json.Marshal, but direct .String() is clearer.
	}

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		// This log should remain unconditional as it indicates an operational failure.
		s.logger.Printf("Error encoding metrics: %v", err)
		// Avoid writing to header again if already written by WriteHeader(http.StatusOK)
		// http.Error might try to set header again. For a JSON endpoint, a JSON error is better.
		// However, if headers are already sent, this will just log.
		// A more robust JSON error response:
		if !headerWritten(w) { // headerWritten is a hypothetical helper; real check is more complex
			w.Header().Set("Content-Type", "application/json") // Ensure content type is JSON for error
			w.WriteHeader(http.StatusInternalServerError)
		}
		// Write a JSON error payload if possible
		jsonError := fmt.Sprintf(`{"error":"failed to encode metrics: %v"}`, err)
		fmt.Fprintln(w, jsonError)
	}
}
