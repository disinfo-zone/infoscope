// internal/server/backup_handler.go
package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// BackupData represents the complete backup structure
type BackupData struct {
	Version      string                 `json:"version"`
	ExportDate   time.Time              `json:"exportDate"`
	Settings     map[string]string      `json:"settings"`
	Feeds        json.RawMessage        `json:"feeds"` // Use RawMessage to handle both v1.0 and v2.0 formats
	Filters      []BackupFilter         `json:"filters,omitempty"`
	FilterGroups []BackupFilterGroup    `json:"filterGroups,omitempty"`
	Tags         []BackupTag            `json:"tags,omitempty"`
	FeedTags     []BackupFeedTag        `json:"feedTags,omitempty"`
	ClickStats   map[string]int         `json:"clickStats,omitempty"`
}

// BackupFeed represents a feed with all its metadata
type BackupFeed struct {
	ID           int64     `json:"id"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	Category     string    `json:"category,omitempty"`
	Status       string    `json:"status,omitempty"`
	ErrorCount   int       `json:"errorCount,omitempty"`
	LastError    string    `json:"lastError,omitempty"`
	LastFetched  *time.Time `json:"lastFetched,omitempty"`
	LastModified string    `json:"lastModified,omitempty"`
	Etag         string    `json:"etag,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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
	importResults := struct {
		Settings     int
		Feeds        int
		Filters      int
		FilterGroups int
		Tags         int
		FeedTags     int
		ClickStats   int
		Errors       []string
	}{
		Errors: make([]string, 0),
	}

	// Import settings with error handling
	if err := s.importSettings(r.Context(), tx, &backup, &importResults); err != nil {
		s.logger.Printf("Error importing settings: %v", err)
		importResults.Errors = append(importResults.Errors, fmt.Sprintf("Settings: %v", err))
	}

	// Import feeds with error handling
	if err := s.importFeeds(r.Context(), tx, &backup, &importResults, backupVersion); err != nil {
		s.logger.Printf("Error importing feeds: %v", err)
		importResults.Errors = append(importResults.Errors, fmt.Sprintf("Feeds: %v", err))
	}

	// Import filters (only for version 2.0+)
	if backupVersion != "1.0" && len(backup.Filters) > 0 {
		if err := s.importFilters(r.Context(), tx, &backup, &importResults); err != nil {
			s.logger.Printf("Error importing filters: %v", err)
			importResults.Errors = append(importResults.Errors, fmt.Sprintf("Filters: %v", err))
		}
	}

	// Import filter groups (only for version 2.0+)
	if backupVersion != "1.0" && len(backup.FilterGroups) > 0 {
		if err := s.importFilterGroups(r.Context(), tx, &backup, &importResults); err != nil {
			s.logger.Printf("Error importing filter groups: %v", err)
			importResults.Errors = append(importResults.Errors, fmt.Sprintf("Filter Groups: %v", err))
		}
	}

	// Import tags (only for version 2.0+)
	if backupVersion != "1.0" && len(backup.Tags) > 0 {
		if err := s.importTags(r.Context(), tx, &backup, &importResults); err != nil {
			s.logger.Printf("Error importing tags: %v", err)
			importResults.Errors = append(importResults.Errors, fmt.Sprintf("Tags: %v", err))
		}
	}

	// Import feed tags (only for version 2.0+)
	if backupVersion != "1.0" && len(backup.FeedTags) > 0 {
		if err := s.importFeedTags(r.Context(), tx, &backup, &importResults); err != nil {
			s.logger.Printf("Error importing feed tags: %v", err)
			importResults.Errors = append(importResults.Errors, fmt.Sprintf("Feed Tags: %v", err))
		}
	}

	// Import click stats (only for version 2.0+)
	if backupVersion != "1.0" && len(backup.ClickStats) > 0 {
		if err := s.importClickStats(r.Context(), tx, &backup, &importResults); err != nil {
			s.logger.Printf("Error importing click stats: %v", err)
			importResults.Errors = append(importResults.Errors, fmt.Sprintf("Click Stats: %v", err))
		}
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

	for key, value := range backup.Settings {
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

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO entry_filters 
		(name, pattern, pattern_type, target_type, case_sensitive, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare filters statement: %w", err)
	}
	defer stmt.Close()

	for _, filter := range backup.Filters {
		caseSensitive := 0
		if filter.CaseSensitive {
			caseSensitive = 1
		}

		_, err := stmt.ExecContext(ctx, filter.Name, filter.Pattern, filter.PatternType,
			filter.TargetType, caseSensitive, filter.CreatedAt, filter.UpdatedAt)
		if err != nil {
			s.logger.Printf("Error importing filter %s: %v", filter.Name, err)
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

	// Import filter groups
	groupStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO filter_groups 
		(name, action, is_active, priority, apply_to_category, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare filter groups statement: %w", err)
	}
	defer groupStmt.Close()

	// Import filter group rules
	ruleStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO filter_group_rules 
		(group_id, filter_id, operator, position)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("failed to prepare filter group rules statement: %w", err)
	}
	defer ruleStmt.Close()

	for _, group := range backup.FilterGroups {
		isActive := 0
		if group.IsActive {
			isActive = 1
		}

		result, err := groupStmt.ExecContext(ctx, group.Name, group.Action, isActive,
			group.Priority, group.ApplyToCategory, group.CreatedAt, group.UpdatedAt)
		if err != nil {
			s.logger.Printf("Error importing filter group %s: %v", group.Name, err)
			continue
		}

		groupID, err := result.LastInsertId()
		if err != nil {
			s.logger.Printf("Error getting group ID for %s: %v", group.Name, err)
			continue
		}

		// Import rules for this group
		for _, rule := range group.Rules {
			_, err := ruleStmt.ExecContext(ctx, groupID, rule.FilterID, rule.Operator, rule.Position)
			if err != nil {
				s.logger.Printf("Error importing filter rule for group %s: %v", group.Name, err)
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

	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO tags (name, created_at) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare tags statement: %w", err)
	}
	defer stmt.Close()

	for _, tag := range backup.Tags {
		_, err := stmt.ExecContext(ctx, tag.Name, tag.CreatedAt)
		if err != nil {
			s.logger.Printf("Error importing tag %s: %v", tag.Name, err)
			continue
		}
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

	stmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO feed_tags (feed_id, tag_id, created_at) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare feed tags statement: %w", err)
	}
	defer stmt.Close()

	for _, feedTag := range backup.FeedTags {
		_, err := stmt.ExecContext(ctx, feedTag.FeedID, feedTag.TagID, feedTag.CreatedAt)
		if err != nil {
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
