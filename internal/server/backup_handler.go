// internal/server/backup_handler.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BackupData represents the complete backup structure
type BackupData struct {
	Version      string              `json:"version"`
	ExportDate   time.Time           `json:"exportDate"`
	Settings     map[string]string   `json:"settings"`
	Feeds        json.RawMessage     `json:"feeds"` // Use RawMessage to handle both v1.0 and v2.0 formats
	Filters      []BackupFilter      `json:"filters,omitempty"`
	FilterGroups []BackupFilterGroup `json:"filterGroups,omitempty"`
	Tags         []BackupTag         `json:"tags,omitempty"`
	FeedTags     []BackupFeedTag     `json:"feedTags,omitempty"`
	ClickStats   map[string]int      `json:"clickStats,omitempty"`
}

// BackupFeed represents a feed with all its metadata
type BackupFeed struct {
	ID           int64      `json:"id"`
	URL          string     `json:"url"`
	Title        string     `json:"title"`
	Category     string     `json:"category,omitempty"`
	Status       string     `json:"status,omitempty"`
	ErrorCount   int        `json:"errorCount,omitempty"`
	LastError    string     `json:"lastError,omitempty"`
	LastFetched  *time.Time `json:"lastFetched,omitempty"`
	LastModified string     `json:"lastModified,omitempty"`
	Etag         string     `json:"etag,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// BackupFilter represents an entry filter
type BackupFilter struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Pattern       string    `json:"pattern"`
	PatternType   string    `json:"patternType"`
	TargetType    string    `json:"targetType"`
	CaseSensitive bool      `json:"caseSensitive"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// BackupFilterGroup represents a filter group
type BackupFilterGroup struct {
	ID              int64                   `json:"id"`
	Name            string                  `json:"name"`
	Action          string                  `json:"action"`
	IsActive        bool                    `json:"isActive"`
	Priority        int                     `json:"priority"`
	ApplyToCategory string                  `json:"applyToCategory,omitempty"`
	Rules           []BackupFilterGroupRule `json:"rules"`
	CreatedAt       time.Time               `json:"createdAt"`
	UpdatedAt       time.Time               `json:"updatedAt"`
}

// BackupFilterGroupRule represents a filter group rule
type BackupFilterGroupRule struct {
	ID       int64  `json:"id"`
	FilterID int64  `json:"filterId"`
	Operator string `json:"operator"`
	Position int    `json:"position"`
}

// BackupTag represents a tag
type BackupTag struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// BackupFeedTag represents a feed-tag relationship
type BackupFeedTag struct {
	ID        int64     `json:"id"`
	FeedID    int64     `json:"feedId"`
	TagID     int64     `json:"tagId"`
	CreatedAt time.Time `json:"createdAt"`
}

// LegacyFeed represents the old v1.0 backup format (for backwards compatibility)
type LegacyFeed struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	LastFetched time.Time `json:"lastFetched,omitempty"`
	Category    string    `json:"category,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
}

// ImportResults collects import counts and errors for user feedback.
type ImportResults struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
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
		Version:      "2.0",
		ExportDate:   time.Now(),
		Settings:     make(map[string]string),
		Filters:      make([]BackupFilter, 0),
		FilterGroups: make([]BackupFilterGroup, 0),
		Tags:         make([]BackupTag, 0),
		FeedTags:     make([]BackupFeedTag, 0),
		ClickStats:   make(map[string]int),
	}

	// Export feeds and marshal separately to handle the structure properly
	var feeds []BackupFeed

	// Export settings with error handling
	if err := s.exportSettings(r.Context(), &backup); err != nil {
		s.logger.Printf("Error exporting settings: %v", err)
		// Continue with partial backup rather than failing completely
	}

	// Export feeds with error handling
	if err := s.exportFeedsToSlice(r.Context(), &feeds); err != nil {
		s.logger.Printf("Error exporting feeds: %v", err)
		// Continue with partial backup
	}

	// Export filters with error handling
	if err := s.exportFilters(r.Context(), &backup); err != nil {
		s.logger.Printf("Error exporting filters: %v", err)
		// Continue with partial backup
	}

	// Export filter groups with error handling
	if err := s.exportFilterGroups(r.Context(), &backup); err != nil {
		s.logger.Printf("Error exporting filter groups: %v", err)
		// Continue with partial backup
	}

	// Export tags with error handling
	if err := s.exportTags(r.Context(), &backup); err != nil {
		s.logger.Printf("Error exporting tags: %v", err)
		// Continue with partial backup
	}

	// Export feed-tag relationships with error handling
	if err := s.exportFeedTags(r.Context(), &backup); err != nil {
		s.logger.Printf("Error exporting feed tags: %v", err)
		// Continue with partial backup
	}

	// Export click statistics with error handling
	if err := s.exportClickStats(r.Context(), &backup); err != nil {
		s.logger.Printf("Error exporting click stats: %v", err)
		// Continue with partial backup
	}

	// Marshal feeds to JSON and store in backup
	if feedsJSON, err := json.Marshal(feeds); err != nil {
		s.logger.Printf("Error marshaling feeds: %v", err)
		backup.Feeds = json.RawMessage("[]") // Empty array as fallback
	} else {
		backup.Feeds = json.RawMessage(feedsJSON)
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

	// Parse multipart form data
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10 MB limit
		s.logger.Printf("Error parsing multipart form: %v", err)
		http.Error(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	// Get backup file from form
	file, _, err := r.FormFile("backup")
	if err != nil {
		s.logger.Printf("Error getting backup file: %v", err)
		http.Error(w, "No backup file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Parse backup data from uploaded file
	var backup BackupData
	if err := json.NewDecoder(file).Decode(&backup); err != nil {
		s.logger.Printf("Error parsing backup data: %v", err)
		http.Error(w, "Invalid backup file format", http.StatusBadRequest)
		return
	}

	// Check backup version and handle backwards compatibility
	backupVersion := backup.Version
	if backupVersion == "" {
		backupVersion = "1.0" // Default to 1.0 for old backups
	}

	s.logger.Printf("Importing backup version %s", backupVersion)

	// Start transaction
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.logger.Printf("Error starting import transaction: %v", err)
		http.Error(w, "Failed to start import", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Track import results for user feedback
	importResults := ImportResults{Errors: make([]string, 0)}

	if err := s.importBackupOrchestrated(r.Context(), tx, &backup, &importResults, backupVersion); err != nil {
		s.logger.Printf("Error importing backup: %v", err)
		importResults.Errors = append(importResults.Errors, err.Error())
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		s.logger.Printf("Error committing import: %v", err)
		http.Error(w, "Failed to complete import", http.StatusInternalServerError)
		return
	}

	// Log import summary
	s.logger.Printf("Import completed: %d settings, %d feeds, %d filters, %d filter groups, %d tags, %d feed tags, %d click stats",
		importResults.Settings, importResults.Feeds, importResults.Filters,
		importResults.FilterGroups, importResults.Tags, importResults.FeedTags, importResults.ClickStats)

	if len(importResults.Errors) > 0 {
		s.logger.Printf("Import had %d errors: %v", len(importResults.Errors), importResults.Errors)
	}

	// Trigger feed fetch for new feeds
	go func() {
		if err := s.feedService.UpdateFeeds(context.Background()); err != nil {
			s.logger.Printf("Error updating feeds after import: %v", err)
		}
	}()

	// Return success response with import summary
	response := map[string]interface{}{
		"success": true,
		"message": "Backup imported successfully",
		"stats": map[string]int{
			"settings":     importResults.Settings,
			"feeds":        importResults.Feeds,
			"filters":      importResults.Filters,
			"filterGroups": importResults.FilterGroups,
			"tags":         importResults.Tags,
			"feedTags":     importResults.FeedTags,
			"clickStats":   importResults.ClickStats,
		},
		"version": backupVersion,
	}

	if len(importResults.Errors) > 0 {
		response["warnings"] = importResults.Errors
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Export helper functions for better error handling and modularity

func (s *Server) exportSettings(ctx context.Context, backup *BackupData) error {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return fmt.Errorf("failed to query settings: %w", err)
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

	return rows.Err()
}

func (s *Server) exportFeedsToSlice(ctx context.Context, feeds *[]BackupFeed) error {
	query := `SELECT id, url, title, COALESCE(category, ''), COALESCE(status, ''), 
	                 COALESCE(error_count, 0), COALESCE(last_error, ''), 
	                 last_fetched, COALESCE(last_modified, ''), COALESCE(etag, ''),
	                 created_at, updated_at
	          FROM feeds ORDER BY id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query feeds: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var feed BackupFeed
		var lastFetched *time.Time

		err := rows.Scan(&feed.ID, &feed.URL, &feed.Title, &feed.Category,
			&feed.Status, &feed.ErrorCount, &feed.LastError,
			&lastFetched, &feed.LastModified, &feed.Etag,
			&feed.CreatedAt, &feed.UpdatedAt)
		if err != nil {
			s.logger.Printf("Error scanning feed: %v", err)
			continue
		}

		feed.LastFetched = lastFetched
		*feeds = append(*feeds, feed)
	}

	return rows.Err()
}

func (s *Server) exportFilters(ctx context.Context, backup *BackupData) error {
	query := `SELECT id, name, pattern, pattern_type, COALESCE(target_type, 'title'), 
	                 COALESCE(case_sensitive, 0), created_at, updated_at
	          FROM entry_filters ORDER BY id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query filters: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var filter BackupFilter
		var caseSensitiveInt int

		err := rows.Scan(&filter.ID, &filter.Name, &filter.Pattern, &filter.PatternType,
			&filter.TargetType, &caseSensitiveInt, &filter.CreatedAt, &filter.UpdatedAt)
		if err != nil {
			s.logger.Printf("Error scanning filter: %v", err)
			continue
		}

		filter.CaseSensitive = caseSensitiveInt == 1
		backup.Filters = append(backup.Filters, filter)
	}

	return rows.Err()
}

func (s *Server) exportFilterGroups(ctx context.Context, backup *BackupData) error {
	query := `SELECT id, name, action, COALESCE(is_active, 1), COALESCE(priority, 0),
	                 COALESCE(apply_to_category, ''), created_at, updated_at
	          FROM filter_groups ORDER BY priority, id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query filter groups: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var group BackupFilterGroup
		var isActiveInt int

		err := rows.Scan(&group.ID, &group.Name, &group.Action, &isActiveInt,
			&group.Priority, &group.ApplyToCategory, &group.CreatedAt, &group.UpdatedAt)
		if err != nil {
			s.logger.Printf("Error scanning filter group: %v", err)
			continue
		}

		group.IsActive = isActiveInt == 1
		group.Rules = make([]BackupFilterGroupRule, 0)

		// Get rules for this group
		ruleQuery := `SELECT id, filter_id, COALESCE(operator, 'AND'), COALESCE(position, 0)
		              FROM filter_group_rules WHERE group_id = ? ORDER BY position`

		ruleRows, err := s.db.QueryContext(ctx, ruleQuery, group.ID)
		if err != nil {
			s.logger.Printf("Error querying rules for group %d: %v", group.ID, err)
		} else {
			for ruleRows.Next() {
				var rule BackupFilterGroupRule
				if err := ruleRows.Scan(&rule.ID, &rule.FilterID, &rule.Operator, &rule.Position); err != nil {
					s.logger.Printf("Error scanning filter rule: %v", err)
					continue
				}
				group.Rules = append(group.Rules, rule)
			}
			ruleRows.Close()
		}

		backup.FilterGroups = append(backup.FilterGroups, group)
	}

	return rows.Err()
}

func (s *Server) exportTags(ctx context.Context, backup *BackupData) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at FROM tags ORDER BY name")
	if err != nil {
		return fmt.Errorf("failed to query tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tag BackupTag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.CreatedAt); err != nil {
			s.logger.Printf("Error scanning tag: %v", err)
			continue
		}
		backup.Tags = append(backup.Tags, tag)
	}

	return rows.Err()
}

func (s *Server) exportFeedTags(ctx context.Context, backup *BackupData) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, feed_id, tag_id, created_at FROM feed_tags ORDER BY feed_id, tag_id")
	if err != nil {
		return fmt.Errorf("failed to query feed tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var feedTag BackupFeedTag
		if err := rows.Scan(&feedTag.ID, &feedTag.FeedID, &feedTag.TagID, &feedTag.CreatedAt); err != nil {
			s.logger.Printf("Error scanning feed tag: %v", err)
			continue
		}
		backup.FeedTags = append(backup.FeedTags, feedTag)
	}

	return rows.Err()
}

func (s *Server) exportClickStats(ctx context.Context, backup *BackupData) error {
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM click_stats")
	if err != nil {
		return fmt.Errorf("failed to query click stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var value int
		if err := rows.Scan(&key, &value); err != nil {
			s.logger.Printf("Error scanning click stat: %v", err)
			continue
		}
		backup.ClickStats[key] = value
	}

	return rows.Err()
}

// Import helper functions for better error handling and backwards compatibility

func (s *Server) importSettings(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}) error {
	if len(backup.Settings) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare settings statement: %w", err)
	}
	defer stmt.Close()

	// Optional: whitelist allowed setting keys to avoid arbitrary keys being imported
	allowed := map[string]bool{
		"site_title": true, "site_url": true, "max_posts": true, "update_interval": true,
		"header_link_text": true, "header_link_url": true, "footer_link_text": true, "footer_link_url": true,
		"footer_image_height": true, "footer_image_url": true, "tracking_code": true, "favicon_url": true,
		"timezone": true, "meta_description": true, "meta_image_url": true, "theme": true,
		"public_theme": true, "admin_theme": true,
		"show_blog_name": true, "show_body_text": true, "body_text_length": true,
		// Auto backup settings
		"backup_enabled": true, "backup_interval_hours": true, "backup_retention_days": true, "backup_last_run": true,
	}

	for key, rawValue := range backup.Settings {
		if !allowed[key] {
			// Skip unknown keys but record as a warning
			s.logger.Printf("Skipping unknown setting key during import: %s", key)
			continue
		}

		value := rawValue
		if key == "tracking_code" {
			// Sanitize tracking code before saving
			sanitized, err := validateTrackingCode(value)
			if err != nil {
				s.logger.Printf("Invalid tracking code in import, skipping: %v", err)
				continue
			}
			value = sanitized
		}

		if _, err := stmt.ExecContext(ctx, key, value); err != nil {
			s.logger.Printf("Error importing setting %s: %v", key, err)
			continue
		}
		results.Settings++
	}

	return nil
}

func (s *Server) importFeeds(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}, backupVersion string) error {
	if len(backup.Feeds) == 0 || string(backup.Feeds) == "[]" || string(backup.Feeds) == "null" {
		return nil
	}

	// Handle backwards compatibility with version 1.0 backups
	if backupVersion == "1.0" {
		// Parse legacy feed format
		var legacyFeeds []LegacyFeed
		if err := json.Unmarshal(backup.Feeds, &legacyFeeds); err != nil {
			return fmt.Errorf("failed to parse legacy feeds: %w", err)
		}

		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO feeds (url, title, status, created_at, updated_at) 
			VALUES (?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(url) DO UPDATE SET 
				title = excluded.title,
				status = 'active',
				updated_at = CURRENT_TIMESTAMP`)
		if err != nil {
			return fmt.Errorf("failed to prepare feeds statement: %w", err)
		}
		defer stmt.Close()

		for _, feed := range legacyFeeds {
			if feed.URL == "" {
				continue
			}

			if _, err := stmt.ExecContext(ctx, feed.URL, feed.Title); err != nil {
				s.logger.Printf("Error importing legacy feed %s: %v", feed.URL, err)
				continue
			}
			results.Feeds++
		}
	} else {
		// Parse version 2.0 feed format
		var feeds []BackupFeed
		if err := json.Unmarshal(backup.Feeds, &feeds); err != nil {
			return fmt.Errorf("failed to parse feeds: %w", err)
		}

		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO feeds 
			(url, title, category, status, error_count, last_error, last_fetched, last_modified, etag, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(url) DO UPDATE SET 
				title = excluded.title,
				category = excluded.category,
				status = CASE WHEN excluded.status = '' OR excluded.status = 'pending' THEN 'active' ELSE excluded.status END,
				error_count = excluded.error_count,
				last_error = excluded.last_error,
				last_fetched = excluded.last_fetched,
				last_modified = excluded.last_modified,
				etag = excluded.etag,
				updated_at = excluded.updated_at`)
		if err != nil {
			return fmt.Errorf("failed to prepare feeds statement: %w", err)
		}
		defer stmt.Close()

		for _, feed := range feeds {
			if feed.URL == "" {
				continue
			}

			// Ensure feed has a valid status - default to 'active' if empty or 'pending'
			status := feed.Status
			if status == "" || status == "pending" {
				status = "active"
			}

			_, err := stmt.ExecContext(ctx, feed.URL, feed.Title, feed.Category, status,
				feed.ErrorCount, feed.LastError, feed.LastFetched, feed.LastModified, feed.Etag,
				feed.CreatedAt, feed.UpdatedAt)
			if err != nil {
				s.logger.Printf("Error importing feed %s: %v", feed.URL, err)
				continue
			}
			results.Feeds++
		}
	}

	return nil
}

func (s *Server) importFilters(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}) error {
	if len(backup.Filters) == 0 {
		return nil
	}

	// Deduplicate by natural key: pattern + pattern_type + target_type + case_sensitive
	// Update name if exists; otherwise insert new
	for _, filter := range backup.Filters {
		caseSensitive := 0
		if filter.CaseSensitive {
			caseSensitive = 1
		}

		var existingID int64
		err := tx.QueryRowContext(ctx, `
            SELECT id FROM entry_filters 
            WHERE pattern = ? AND pattern_type = ? AND target_type = ? AND case_sensitive = ?
        `, filter.Pattern, filter.PatternType, filter.TargetType, caseSensitive).Scan(&existingID)

		if err == nil && existingID > 0 {
			// Update name and timestamps to latest if desired
			if _, err := tx.ExecContext(ctx, `
                UPDATE entry_filters 
                SET name = COALESCE(NULLIF(?, ''), name), updated_at = CURRENT_TIMESTAMP
                WHERE id = ?
            `, filter.Name, existingID); err != nil {
				s.logger.Printf("Error updating existing filter %d: %v", existingID, err)
			}
			results.Filters++
			continue
		}
		if err != nil && err != sql.ErrNoRows {
			s.logger.Printf("Error checking existing filter: %v", err)
			continue
		}

		if _, err := tx.ExecContext(ctx, `
            INSERT INTO entry_filters (name, pattern, pattern_type, target_type, case_sensitive, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        `, filter.Name, filter.Pattern, filter.PatternType, filter.TargetType, caseSensitive, filter.CreatedAt, filter.UpdatedAt); err != nil {
			s.logger.Printf("Error inserting filter %s: %v", filter.Name, err)
			continue
		}
		results.Filters++
	}

	return nil
}

func (s *Server) importFilterGroups(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}) error {
	if len(backup.FilterGroups) == 0 {
		return nil
	}

	for _, group := range backup.FilterGroups {
		isActive := 0
		if group.IsActive {
			isActive = 1
		}

		var groupID int64
		// Try to find existing group by name
		err := tx.QueryRowContext(ctx, `SELECT id FROM filter_groups WHERE name = ?`, group.Name).Scan(&groupID)
		if err == sql.ErrNoRows {
			// Insert new group
			res, err := tx.ExecContext(ctx, `
                INSERT INTO filter_groups (name, action, is_active, priority, apply_to_category, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)`, group.Name, group.Action, isActive, group.Priority, group.ApplyToCategory, group.CreatedAt, group.UpdatedAt)
			if err != nil {
				s.logger.Printf("Error inserting filter group %s: %v", group.Name, err)
				continue
			}
			gid, _ := res.LastInsertId()
			groupID = gid
		} else if err != nil {
			s.logger.Printf("Error checking existing filter group %s: %v", group.Name, err)
			continue
		} else {
			// Update existing group
			if _, err := tx.ExecContext(ctx, `
                UPDATE filter_groups SET action = ?, is_active = ?, priority = ?, apply_to_category = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
            `, group.Action, isActive, group.Priority, group.ApplyToCategory, groupID); err != nil {
				s.logger.Printf("Error updating filter group %s: %v", group.Name, err)
				continue
			}
			// Clear existing rules to avoid duplicates
			if _, err := tx.ExecContext(ctx, `DELETE FROM filter_group_rules WHERE group_id = ?`, groupID); err != nil {
				s.logger.Printf("Error clearing rules for group %s: %v", group.Name, err)
			}
		}

		// Import rules for this group (note: will be remapped in orchestrated import)
		for _, rule := range group.Rules {
			if _, err := tx.ExecContext(ctx, `
                INSERT INTO filter_group_rules (group_id, filter_id, operator, position)
                VALUES (?, ?, ?, ?)`, groupID, rule.FilterID, rule.Operator, rule.Position); err != nil {
				s.logger.Printf("Error inserting filter rule for group %s: %v", group.Name, err)
			}
		}

		results.FilterGroups++
	}

	return nil
}

func (s *Server) importTags(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}) error {
	if len(backup.Tags) == 0 {
		return nil
	}

	for _, tag := range backup.Tags {
		// Upsert by name (case-insensitive)
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? COLLATE NOCASE`, tag.Name).Scan(&id)
		if err == sql.ErrNoRows {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tags (name, created_at) VALUES (?, ?)`, tag.Name, tag.CreatedAt); err != nil {
				s.logger.Printf("Error inserting tag %s: %v", tag.Name, err)
				continue
			}
			results.Tags++
			continue
		}
		if err != nil {
			s.logger.Printf("Error checking tag %s: %v", tag.Name, err)
			continue
		}
		// Tag exists; nothing to do
		results.Tags++
	}

	return nil
}

func (s *Server) importFeedTags(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}) error {
	if len(backup.FeedTags) == 0 {
		return nil
	}

	// This basic import assumes IDs match; the orchestrated import remaps IDs correctly
	for _, feedTag := range backup.FeedTags {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO feed_tags (feed_id, tag_id, created_at) VALUES (?, ?, ?)", feedTag.FeedID, feedTag.TagID, feedTag.CreatedAt); err != nil {
			s.logger.Printf("Error importing feed tag relationship: %v", err)
			continue
		}
		results.FeedTags++
	}

	return nil
}

func (s *Server) importClickStats(ctx context.Context, tx *sql.Tx, backup *BackupData, results *struct {
	Settings     int
	Feeds        int
	Filters      int
	FilterGroups int
	Tags         int
	FeedTags     int
	ClickStats   int
	Errors       []string
}) error {
	if len(backup.ClickStats) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO click_stats (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)")
	if err != nil {
		return fmt.Errorf("failed to prepare click stats statement: %w", err)
	}
	defer stmt.Close()

	for key, value := range backup.ClickStats {
		_, err := stmt.ExecContext(ctx, key, value)
		if err != nil {
			s.logger.Printf("Error importing click stat %s: %v", key, err)
			continue
		}
		results.ClickStats++
	}

	return nil
}

// --------------------
// Orchestrated import with ID remapping and deduplication across entities
// --------------------

// importBackupOrchestrated imports a backup with proper ID remapping for feeds, tags, filters and filter group rules,
// deduplicating by natural keys to avoid duplicates across repeated imports.
func (s *Server) importBackupOrchestrated(ctx context.Context, tx *sql.Tx, backup *BackupData, results *ImportResults, backupVersion string) error {
	// 1) Settings
	if err := s.importSettings(ctx, tx, backup, &struct {
		Settings     int
		Feeds        int
		Filters      int
		FilterGroups int
		Tags         int
		FeedTags     int
		ClickStats   int
		Errors       []string
	}{Errors: results.Errors}); err != nil {
		results.Errors = append(results.Errors, fmt.Sprintf("Settings: %v", err))
	} else {
		// Approximate settings count
		results.Settings = len(backup.Settings)
	}

	// ID maps
	feedIDMap := make(map[int64]int64)
	tagIDMap := make(map[int64]int64)
	filterIDMap := make(map[int64]int64)

	// 2) Tags (build tag ID map by name)
	if backupVersion != "1.0" && len(backup.Tags) > 0 {
		for _, tag := range backup.Tags {
			var newID int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM tags WHERE name = ? COLLATE NOCASE`, tag.Name).Scan(&newID)
			if err == sql.ErrNoRows {
				res, err := tx.ExecContext(ctx, `INSERT INTO tags (name, created_at) VALUES (?, ?)`, tag.Name, tag.CreatedAt)
				if err != nil {
					s.logger.Printf("Error inserting tag %s: %v", tag.Name, err)
					results.Errors = append(results.Errors, fmt.Sprintf("Tag %s: %v", tag.Name, err))
					continue
				}
				id, _ := res.LastInsertId()
				newID = id
				results.Tags++
			} else if err != nil {
				s.logger.Printf("Error checking tag %s: %v", tag.Name, err)
				results.Errors = append(results.Errors, fmt.Sprintf("Tag %s: %v", tag.Name, err))
				continue
			} else {
				results.Tags++
			}
			tagIDMap[tag.ID] = newID
		}
	}

	// 3) Filters (map by natural key)
	if backupVersion != "1.0" && len(backup.Filters) > 0 {
		for _, filter := range backup.Filters {
			caseSensitive := 0
			if filter.CaseSensitive {
				caseSensitive = 1
			}
			var newID int64
			err := tx.QueryRowContext(ctx, `
                SELECT id FROM entry_filters WHERE pattern = ? AND pattern_type = ? AND target_type = ? AND case_sensitive = ?
            `, filter.Pattern, filter.PatternType, filter.TargetType, caseSensitive).Scan(&newID)
			if err == sql.ErrNoRows {
				res, err := tx.ExecContext(ctx, `
                    INSERT INTO entry_filters (name, pattern, pattern_type, target_type, case_sensitive, created_at, updated_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?)`, filter.Name, filter.Pattern, filter.PatternType, filter.TargetType, caseSensitive, filter.CreatedAt, filter.UpdatedAt)
				if err != nil {
					s.logger.Printf("Error inserting filter %s: %v", filter.Name, err)
					results.Errors = append(results.Errors, fmt.Sprintf("Filter %s: %v", filter.Name, err))
					continue
				}
				id, _ := res.LastInsertId()
				newID = id
				results.Filters++
			} else if err != nil {
				s.logger.Printf("Error checking filter %s: %v", filter.Name, err)
				results.Errors = append(results.Errors, fmt.Sprintf("Filter %s: %v", filter.Name, err))
				continue
			} else {
				// Optionally update the name
				_, _ = tx.ExecContext(ctx, `UPDATE entry_filters SET name = COALESCE(NULLIF(?, ''), name) WHERE id = ?`, filter.Name, newID)
				results.Filters++
			}
			filterIDMap[filter.ID] = newID
		}
	}

	// 4) Feeds (map by URL)
	if len(backup.Feeds) > 0 && string(backup.Feeds) != "[]" && string(backup.Feeds) != "null" {
		if backupVersion == "1.0" {
			var legacyFeeds []LegacyFeed
			if err := json.Unmarshal(backup.Feeds, &legacyFeeds); err != nil {
				return fmt.Errorf("failed to parse legacy feeds: %w", err)
			}
			for _, f := range legacyFeeds {
				if f.URL == "" {
					continue
				}
				// Insert or update by URL, default status to active
				if _, err := tx.ExecContext(ctx, `
                    INSERT INTO feeds (url, title, status, created_at, updated_at)
                    VALUES (?, ?, 'active', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
                    ON CONFLICT(url) DO UPDATE SET 
                        title = excluded.title,
                        status = 'active',
                        updated_at = CURRENT_TIMESTAMP
                `, f.URL, f.Title); err != nil {
					s.logger.Printf("Error importing legacy feed %s: %v", f.URL, err)
					results.Errors = append(results.Errors, fmt.Sprintf("Feed %s: %v", f.URL, err))
					continue
				}
				var newID int64
				if err := tx.QueryRowContext(ctx, `SELECT id FROM feeds WHERE url = ?`, f.URL).Scan(&newID); err == nil {
					feedIDMap[f.ID] = newID
				}
				results.Feeds++
			}
		} else {
			var feeds []BackupFeed
			if err := json.Unmarshal(backup.Feeds, &feeds); err != nil {
				return fmt.Errorf("failed to parse feeds: %w", err)
			}
			for _, f := range feeds {
				if f.URL == "" {
					continue
				}
				status := f.Status
				if status == "" || status == "pending" {
					status = "active"
				}
				if _, err := tx.ExecContext(ctx, `
                    INSERT INTO feeds (url, title, category, status, error_count, last_error, last_fetched, last_modified, etag, created_at, updated_at)
                    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    ON CONFLICT(url) DO UPDATE SET 
                        title = excluded.title,
                        category = excluded.category,
                        status = CASE WHEN excluded.status = '' OR excluded.status = 'pending' THEN 'active' ELSE excluded.status END,
                        error_count = excluded.error_count,
                        last_error = excluded.last_error,
                        last_fetched = excluded.last_fetched,
                        last_modified = excluded.last_modified,
                        etag = excluded.etag,
                        updated_at = excluded.updated_at
                `, f.URL, f.Title, f.Category, status, f.ErrorCount, f.LastError, f.LastFetched, f.LastModified, f.Etag, f.CreatedAt, f.UpdatedAt); err != nil {
					s.logger.Printf("Error importing feed %s: %v", f.URL, err)
					results.Errors = append(results.Errors, fmt.Sprintf("Feed %s: %v", f.URL, err))
					continue
				}
				var newID int64
				if err := tx.QueryRowContext(ctx, `SELECT id FROM feeds WHERE url = ?`, f.URL).Scan(&newID); err == nil {
					feedIDMap[f.ID] = newID
				}
				results.Feeds++
			}
		}
	}

	// 5) Filter groups + rules (remap filter IDs)
	if backupVersion != "1.0" && len(backup.FilterGroups) > 0 {
		for _, group := range backup.FilterGroups {
			isActive := 0
			if group.IsActive {
				isActive = 1
			}
			var groupID int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM filter_groups WHERE name = ?`, group.Name).Scan(&groupID)
			if err == sql.ErrNoRows {
				res, err := tx.ExecContext(ctx, `INSERT INTO filter_groups (name, action, is_active, priority, apply_to_category, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, group.Name, group.Action, isActive, group.Priority, group.ApplyToCategory, group.CreatedAt, group.UpdatedAt)
				if err != nil {
					s.logger.Printf("Error inserting filter group %s: %v", group.Name, err)
					results.Errors = append(results.Errors, fmt.Sprintf("Group %s: %v", group.Name, err))
					continue
				}
				id, _ := res.LastInsertId()
				groupID = id
			} else if err != nil {
				s.logger.Printf("Error checking filter group %s: %v", group.Name, err)
				results.Errors = append(results.Errors, fmt.Sprintf("Group %s: %v", group.Name, err))
				continue
			} else {
				if _, err := tx.ExecContext(ctx, `UPDATE filter_groups SET action = ?, is_active = ?, priority = ?, apply_to_category = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, group.Action, isActive, group.Priority, group.ApplyToCategory, groupID); err != nil {
					s.logger.Printf("Error updating filter group %s: %v", group.Name, err)
					continue
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM filter_group_rules WHERE group_id = ?`, groupID); err != nil {
					s.logger.Printf("Error clearing rules for group %s: %v", group.Name, err)
				}
			}

			// Insert remapped rules
			position := 0
			for _, rule := range group.Rules {
				newFilterID, ok := filterIDMap[rule.FilterID]
				if !ok || newFilterID == 0 {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO filter_group_rules (group_id, filter_id, operator, position) VALUES (?, ?, ?, ?)`, groupID, newFilterID, rule.Operator, position); err != nil {
					s.logger.Printf("Error inserting rule for group %s: %v", group.Name, err)
				}
				position++
			}
			results.FilterGroups++
		}
	}

	// 6) Feed tags (remap feed+tag IDs)
	if backupVersion != "1.0" && len(backup.FeedTags) > 0 {
		for _, ft := range backup.FeedTags {
			newFeedID := feedIDMap[ft.FeedID]
			newTagID := tagIDMap[ft.TagID]
			if newFeedID == 0 || newTagID == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO feed_tags (feed_id, tag_id, created_at) VALUES (?, ?, ?)`, newFeedID, newTagID, ft.CreatedAt); err != nil {
				s.logger.Printf("Error inserting feed_tag: %v", err)
				continue
			}
			results.FeedTags++
		}
	}

	// 7) Click stats
	if backupVersion != "1.0" && len(backup.ClickStats) > 0 {
		if err := s.importClickStats(ctx, tx, backup, &struct {
			Settings     int
			Feeds        int
			Filters      int
			FilterGroups int
			Tags         int
			FeedTags     int
			ClickStats   int
			Errors       []string
		}{Errors: results.Errors}); err != nil {
			results.Errors = append(results.Errors, fmt.Sprintf("Click Stats: %v", err))
		}
	}

	return nil
}

// --------------------
// Backup to disk + scheduler helpers
// --------------------

// generateBackupData builds the BackupData struct with current DB contents.
func (s *Server) generateBackupData(ctx context.Context) (BackupData, error) {
	backup := BackupData{
		Version:      "2.0",
		ExportDate:   time.Now(),
		Settings:     make(map[string]string),
		Filters:      make([]BackupFilter, 0),
		FilterGroups: make([]BackupFilterGroup, 0),
		Tags:         make([]BackupTag, 0),
		FeedTags:     make([]BackupFeedTag, 0),
		ClickStats:   make(map[string]int),
	}
	var feeds []BackupFeed
	if err := s.exportSettings(ctx, &backup); err != nil {
		s.logger.Printf("Error exporting settings: %v", err)
	}
	if err := s.exportFeedsToSlice(ctx, &feeds); err != nil {
		s.logger.Printf("Error exporting feeds: %v", err)
	}
	if err := s.exportFilters(ctx, &backup); err != nil {
		s.logger.Printf("Error exporting filters: %v", err)
	}
	if err := s.exportFilterGroups(ctx, &backup); err != nil {
		s.logger.Printf("Error exporting filter groups: %v", err)
	}
	if err := s.exportTags(ctx, &backup); err != nil {
		s.logger.Printf("Error exporting tags: %v", err)
	}
	if err := s.exportFeedTags(ctx, &backup); err != nil {
		s.logger.Printf("Error exporting feed tags: %v", err)
	}
	if err := s.exportClickStats(ctx, &backup); err != nil {
		s.logger.Printf("Error exporting click stats: %v", err)
	}
	if feedsJSON, err := json.Marshal(feeds); err == nil {
		backup.Feeds = json.RawMessage(feedsJSON)
	} else {
		backup.Feeds = json.RawMessage("[]")
	}
	return backup, nil
}

func (s *Server) getBackupDir() string {
	base := s.config.WebPath
	if s.config.DataPath != "" {
		base = s.config.DataPath
	}
	return base + string('/') + "backups"
}

func (s *Server) writeBackupToDisk(ctx context.Context, backup BackupData) (string, error) {
	dir := s.getBackupDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup dir: %w", err)
	}
	// Marshal compact JSON
	data, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal backup: %w", err)
	}
	name := fmt.Sprintf("infoscope_backup_%s.json", time.Now().Format("20060102_150405"))
	path := dir + string('/') + name
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}
	return name, nil
}

// pruneOldBackups deletes backups older than retentionDays.
func (s *Server) pruneOldBackups(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	dir := s.getBackupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(dir + string('/') + e.Name())
		}
	}
}

// startAutoBackupLoop runs a lightweight scheduler checking settings and performing backups at configured intervals.
func (s *Server) startAutoBackupLoop() {
	if s.backupStop == nil {
		s.backupStop = make(chan struct{})
	}
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		var lastRun time.Time
		for {
			select {
			case <-ticker.C:
				// Read settings
				settings := make(map[string]string)
				rows, err := s.db.Query("SELECT key, value FROM settings WHERE key IN ('backup_enabled','backup_interval_hours','backup_retention_days','backup_last_run')")
				if err == nil {
					for rows.Next() {
						var k, v string
						_ = rows.Scan(&k, &v)
						settings[k] = v
					}
					rows.Close()
				}
				if strings.ToLower(settings["backup_enabled"]) != "true" {
					continue
				}
				intervalHours := 24
				if ih, err := strconv.Atoi(strings.TrimSpace(settings["backup_interval_hours"])); err == nil && ih > 0 {
					intervalHours = ih
				}
				retentionDays := 30
				if rd, err := strconv.Atoi(strings.TrimSpace(settings["backup_retention_days"])); err == nil && rd > 0 {
					retentionDays = rd
				}
				if lr := strings.TrimSpace(settings["backup_last_run"]); lr != "" {
					if t, err := time.Parse(time.RFC3339, lr); err == nil {
						lastRun = t
					}
				}
				if time.Since(lastRun) < time.Duration(intervalHours)*time.Hour {
					continue
				}
				// Run backup
				backup, err := s.generateBackupData(context.Background())
				if err == nil {
					if _, err := s.writeBackupToDisk(context.Background(), backup); err == nil {
						// Update last run
						_, _ = s.db.Exec(`INSERT INTO settings (key, value) VALUES ('backup_last_run', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, time.Now().UTC().Format(time.RFC3339))
						lastRun = time.Now()
						// Prune
						s.pruneOldBackups(retentionDays)
					} else {
						s.logger.Printf("Auto-backup write error: %v", err)
					}
				} else {
					s.logger.Printf("Auto-backup generate error: %v", err)
				}
			case <-s.backupStop:
				return
			}
		}
	}()
}

// HTTP: export backup to disk and return JSON
func (s *Server) handleExportToDisk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.csrf.Validate(w, r) {
		return
	}
	backup, err := s.generateBackupData(r.Context())
	if err != nil {
		http.Error(w, "Failed to create backup", http.StatusInternalServerError)
		return
	}
	name, err := s.writeBackupToDisk(r.Context(), backup)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "filename": name})
}

// HTTP: list backups on disk
func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dir := s.getBackupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		entries = []os.DirEntry{}
	}
	type fileInfo struct {
		Name     string    `json:"name"`
		Size     int64     `json:"size"`
		Modified time.Time `json:"modified"`
	}
	files := make([]fileInfo, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{Name: e.Name(), Size: info.Size(), Modified: info.ModTime()})
	}
	// Sort newest first
	sort.Slice(files, func(i, j int) bool { return files[i].Modified.After(files[j].Modified) })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"files": files})
}

// HTTP: restore a backup from disk by filename
func (s *Server) handleRestoreFromFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.csrf.Validate(w, r) {
		return
	}
	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	name := filepath.Base(strings.TrimSpace(req.Filename))
	if name == "" {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.getBackupDir(), name)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	var backup BackupData
	if err := json.NewDecoder(f).Decode(&backup); err != nil {
		http.Error(w, "Invalid backup file", http.StatusBadRequest)
		return
	}
	// Determine version
	version := backup.Version
	if version == "" {
		version = "1.0"
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "Failed to start import", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	results := ImportResults{Errors: make([]string, 0)}
	if err := s.importBackupOrchestrated(r.Context(), tx, &backup, &results, version); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, "Failed to complete import", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "Backup restored", "stats": map[string]int{
		"settings": results.Settings, "feeds": results.Feeds, "filters": results.Filters, "filterGroups": results.FilterGroups, "tags": results.Tags, "feedTags": results.FeedTags, "clickStats": results.ClickStats,
	}})
}

// HTTP: authenticated download of backup file
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
	if name == "" {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.getBackupDir(), name)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, r, path)
}

// HTTP: delete a backup file (with caution modal on frontend)
func (s *Server) handleDeleteBackupFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.csrf.Validate(w, r) {
		return
	}
	var req struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	name := filepath.Base(strings.TrimSpace(req.Filename))
	if name == "" {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}
	path := filepath.Join(s.getBackupDir(), name)
	if err := os.Remove(path); err != nil {
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}
