// internal/server/handlers.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"expvar"
	"fmt"
	"infoscope/internal/feed"
	"infoscope/internal/rss"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Metrics variables
var (
	dbQueryCount    = expvar.NewInt("db_query_count")
	dbQueryDuration = expvar.NewFloat("db_query_duration_ms")
)

// validateTrackingCode validates and sanitizes tracking code HTML
// to prevent XSS attacks while allowing common analytics script patterns
func validateTrackingCode(code string) (string, error) {
	if code == "" {
		return "", nil
	}

	// Wrap the code in a temporary container to create valid HTML
	// This prevents the parser from adding implicit html/body wrappers
	wrappedCode := "<div>" + code + "</div>"
	
	// Parse the HTML
	doc, err := html.Parse(strings.NewReader(wrappedCode))
	if err != nil {
		return "", fmt.Errorf("invalid HTML: %v", err)
	}

	// Find the div container we added and process its children
	var divNode *html.Node
	var findDiv func(*html.Node)
	findDiv = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			divNode = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findDiv(c)
		}
	}
	findDiv(doc)

	if divNode == nil {
		return "", errors.New("failed to parse HTML structure")
	}

	// Validate and rebuild only the children of our wrapper div
	var validatedHTML strings.Builder
	for c := divNode.FirstChild; c != nil; c = c.NextSibling {
		if err := validateAndRebuildHTML(c, &validatedHTML); err != nil {
			return "", err
		}
	}

	return validatedHTML.String(), nil
}

// validateAndRebuildHTML recursively validates HTML nodes and rebuilds safe HTML
func validateAndRebuildHTML(n *html.Node, output *strings.Builder) error {
	switch n.Type {
	case html.DocumentNode:
		// Process children for document node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := validateAndRebuildHTML(c, output); err != nil {
				return err
			}
		}
	case html.ElementNode:
		if err := validateElement(n, output); err != nil {
			return err
		}
	case html.TextNode:
		// Escape text content to prevent injection
		output.WriteString(html.EscapeString(n.Data))
	case html.CommentNode:
		// Allow comments but escape them
		output.WriteString("<!--")
		output.WriteString(html.EscapeString(n.Data))
		output.WriteString("-->")
	}
	return nil
}

// validateElement validates and rebuilds HTML elements
func validateElement(n *html.Node, output *strings.Builder) error {
	switch strings.ToLower(n.Data) {
	case "script":
		return validateScriptElement(n, output)
	case "img":
		return validateImgElement(n, output)
	case "meta":
		return validateMetaElement(n, output)
	case "iframe":
		return validateIframeElement(n, output)
	case "div", "span", "noscript":
		return validateGenericElement(n, output)
	default:
		return fmt.Errorf("element '%s' is not allowed in tracking code", n.Data)
	}
}

// validateScriptElement validates script tags, allowing only external scripts
func validateScriptElement(n *html.Node, output *strings.Builder) error {
	var src string
	var safeAttrs []html.Attribute

	// Check attributes
	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "src":
			if err := validateURL(attr.Val); err != nil {
				return fmt.Errorf("invalid script src URL: %v", err)
			}
			src = attr.Val
			safeAttrs = append(safeAttrs, attr)
		case "async", "defer", "crossorigin", "integrity", "type":
			safeAttrs = append(safeAttrs, attr)
		case "id", "class":
			// Allow id and class but sanitize values
			if isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		default:
			if strings.HasPrefix(attr.Key, "data-") && isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		}
	}

	// Require external source for script tags
	if src == "" {
		return errors.New("script tags must have a src attribute (inline JavaScript not allowed)")
	}

	// Check for any text content (inline JavaScript)
	if hasTextContent(n) {
		return errors.New("script tags cannot contain inline JavaScript")
	}

	// Write the validated script tag
	output.WriteString("<script")
	for _, attr := range safeAttrs {
		output.WriteString(fmt.Sprintf(` %s="%s"`, attr.Key, html.EscapeString(attr.Val)))
	}
	output.WriteString("></script>")

	return nil
}

// validateImgElement validates img tags for tracking pixels
func validateImgElement(n *html.Node, output *strings.Builder) error {
	var safeAttrs []html.Attribute

	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "src":
			if err := validateURL(attr.Val); err != nil {
				return fmt.Errorf("invalid img src URL: %v", err)
			}
			safeAttrs = append(safeAttrs, attr)
		case "width", "height", "alt", "loading":
			if isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		case "style":
			if isValidStyleValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		}
	}

	output.WriteString("<img")
	for _, attr := range safeAttrs {
		output.WriteString(fmt.Sprintf(` %s="%s"`, attr.Key, html.EscapeString(attr.Val)))
	}
	output.WriteString(">")

	return nil
}

// validateMetaElement validates meta tags
func validateMetaElement(n *html.Node, output *strings.Builder) error {
	var safeAttrs []html.Attribute

	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "name", "content", "property":
			if isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		}
	}

	output.WriteString("<meta")
	for _, attr := range safeAttrs {
		output.WriteString(fmt.Sprintf(` %s="%s"`, attr.Key, html.EscapeString(attr.Val)))
	}
	output.WriteString(">")

	return nil
}

// validateIframeElement validates iframe tags with restrictions
func validateIframeElement(n *html.Node, output *strings.Builder) error {
	var safeAttrs []html.Attribute

	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "src":
			if err := validateURL(attr.Val); err != nil {
				return fmt.Errorf("invalid iframe src URL: %v", err)
			}
			// Allow iframes from any valid HTTP/HTTPS URL for self-hosted analytics
			// Only validate the URL format and basic security, not domain restrictions
			safeAttrs = append(safeAttrs, attr)
		case "width", "height", "frameborder", "title":
			if isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		case "style":
			if isValidStyleValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		}
	}

	output.WriteString("<iframe")
	for _, attr := range safeAttrs {
		output.WriteString(fmt.Sprintf(` %s="%s"`, attr.Key, html.EscapeString(attr.Val)))
	}
	output.WriteString(">")

	// Process children
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := validateAndRebuildHTML(c, output); err != nil {
			return err
		}
	}

	output.WriteString("</iframe>")
	return nil
}

// validateGenericElement validates div, span, noscript elements
func validateGenericElement(n *html.Node, output *strings.Builder) error {
	var safeAttrs []html.Attribute

	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "id", "class":
			if isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		case "style":
			if isValidStyleValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		default:
			if strings.HasPrefix(attr.Key, "data-") && isValidAttributeValue(attr.Val) {
				safeAttrs = append(safeAttrs, attr)
			}
		}
	}

	output.WriteString(fmt.Sprintf("<%s", n.Data))
	for _, attr := range safeAttrs {
		output.WriteString(fmt.Sprintf(` %s="%s"`, attr.Key, html.EscapeString(attr.Val)))
	}
	output.WriteString(">")

	// Process children
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := validateAndRebuildHTML(c, output); err != nil {
			return err
		}
	}

	output.WriteString(fmt.Sprintf("</%s>", n.Data))
	return nil
}

// Helper functions

// validateURL checks if a URL is safe for use in tracking code
func validateURL(urlStr string) error {
	if urlStr == "" {
		return errors.New("URL cannot be empty")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %v", err)
	}

	// Only allow HTTP/HTTPS URLs
	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return fmt.Errorf("only HTTP/HTTPS URLs are allowed, got: %s", parsedURL.Scheme)
	}

	// Block obviously dangerous patterns but allow self-hosted analytics
	// Only block localhost on standard web ports to prevent local attacks
	// but allow custom domains and ports for self-hosted analytics
	if (strings.Contains(parsedURL.Host, "localhost:80") ||
		strings.Contains(parsedURL.Host, "localhost:443") ||
		parsedURL.Host == "localhost" ||
		strings.Contains(parsedURL.Host, "127.0.0.1:80") ||
		strings.Contains(parsedURL.Host, "127.0.0.1:443") ||
		parsedURL.Host == "127.0.0.1") {
		return errors.New("URLs pointing to localhost on standard web ports are not allowed")
	}

	return nil
}

// isValidAttributeValue checks if an attribute value is safe
func isValidAttributeValue(value string) bool {
	// Reject values with potential script injection
	lowerValue := strings.ToLower(value)
	dangerous := []string{"javascript:", "data:", "vbscript:", "onload", "onerror", "onclick", "onmouseover"}
	for _, danger := range dangerous {
		if strings.Contains(lowerValue, danger) {
			return false
		}
	}
	return true
}

// isValidStyleValue checks if a style attribute value is safe
func isValidStyleValue(value string) bool {
	// Basic style validation - reject dangerous CSS
	lowerValue := strings.ToLower(value)
	dangerous := []string{"javascript:", "expression(", "behavior:", "binding:", "url(javascript"}
	for _, danger := range dangerous {
		if strings.Contains(lowerValue, danger) {
			return false
		}
	}
	
	// Only allow basic style properties
	allowedProps := regexp.MustCompile(`^[\s\w\-:;.#%(),]+$`)
	return allowedProps.MatchString(value)
}

// hasTextContent checks if a node has any text content (for detecting inline scripts)
func hasTextContent(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode && strings.TrimSpace(c.Data) != "" {
			return true
		}
		if hasTextContent(c) {
			return true
		}
	}
	return false
}

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

	// Get settings to check if we should show blog names and body text
	settings, err := s.getSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting settings: %w", err)
	}

	showBlogName := settings["show_blog_name"] == "true"
	showBodyText := settings["show_body_text"] == "true"
	
	bodyTextLength := 200 // default
	if lengthStr, ok := settings["body_text_length"]; ok {
		if length, err := strconv.Atoi(lengthStr); err == nil && length > 0 {
			bodyTextLength = length
		}
	}

	rows, err := s.db.QueryContext(ctx, `
        SELECT 
            e.id,
            e.title,
            e.url,
            e.favicon_url,
            datetime(e.published_at) as date,
            f.title as feed_title,
            e.content
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
		var feedTitle sql.NullString
		var content sql.NullString
		
		if err := rows.Scan(&e.ID, &e.Title, &e.URL, &e.FaviconURL, &publishedAtStr, &feedTitle, &content); err != nil {
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

		// Set feed title if enabled and available
		if showBlogName && feedTitle.Valid {
			e.FeedTitle = feedTitle.String
		}

		// Process body text if enabled and available
		if showBodyText && content.Valid {
			e.BodyText = ProcessBodyText(content.String, bodyTextLength)
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
	// Validate and sanitize the tracking code before saving
	validatedTrackingCode, err := validateTrackingCode(settings.TrackingCode)
	if err != nil {
		return fmt.Errorf("invalid tracking code: %w", err)
	}
	
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
	
	// Convert boolean to string for storage
	showBlogNameStr := "false"
	if settings.ShowBlogName {
		showBlogNameStr = "true"
	}
	
	showBodyTextStr := "false"
	if settings.ShowBodyText {
		showBodyTextStr = "true"
	}
	
	updates := map[string]struct {
		value string
		type_ string
	}{
		"site_title":          {settings.SiteTitle, "string"},
		"site_url":            {settings.SiteURL, "string"},
		"max_posts":           {strconv.Itoa(settings.MaxPosts), "int"},
		"update_interval":     {strconv.Itoa(settings.UpdateInterval), "int"},
		"header_link_text":    {settings.HeaderLinkText, "string"},
		"header_link_url":     {settings.HeaderLinkURL, "string"},
		"footer_link_text":    {settings.FooterLinkText, "string"},
		"footer_link_url":     {settings.FooterLinkURL, "string"},
		"footer_image_height": {settings.FooterImageHeight, "string"},
		"footer_image_url":    {settings.FooterImageURL, "string"},
		"tracking_code":       {validatedTrackingCode, "string"},
		"favicon_url":         {settings.FaviconURL, "string"},
		"timezone":            {settings.Timezone, "string"},
		"meta_description":    {settings.MetaDescription, "string"},
		"meta_image_url":      {settings.MetaImageURL, "string"},
		"show_blog_name":      {showBlogNameStr, "bool"},
		"show_body_text":      {showBodyTextStr, "bool"},
		"body_text_length":    {strconv.Itoa(settings.BodyTextLength), "int"},
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

	// If site_url is not configured, construct it from the request
	if siteURL == "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		siteURL = scheme + "://" + r.Host
		s.logger.Printf("Warning: Site URL (site_url) is not configured in settings. Using constructed URL: %s", siteURL)
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
	// Generate atom:link with rel="self" - required for RSS validation
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
	for _, entry := range entries {
		item := rss.Item{
			Title:       entry.Title,
			Link:        entry.URL,   // Assuming entry.URL is absolute
			Description: entry.Title, // Using title as description, as no other summary is readily available
			GUID: rss.GUID{
				Value:       entry.URL,
				IsPermaLink: true,
			}, // Using URL as GUID, common practice
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
