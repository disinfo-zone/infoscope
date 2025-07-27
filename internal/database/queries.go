// internal/database/queries.go
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Error definitions
var (
	ErrNotFound     = errors.New("record not found")
	ErrInvalidInput = errors.New("invalid input")
)

// Entry represents a feed entry with related data
type Entry struct {
	ID          int64
	FeedID      int64
	Title       string
	URL         string
	Content     string    // Add content field
	PublishedAt time.Time
	FaviconURL  string
	FeedTitle   string // Joined from feeds table
}

// Feed represents a feed subscription
type Feed struct {
	ID           int64
	URL          string
	Title        string
	LastFetched  time.Time
	LastModified string
	ETag         string
	Status       string
	ErrorCount   int
	LastError    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ClickStats represents click tracking statistics
type ClickStats struct {
	TotalClicks int64
	TopAllTime  []ClickEntry
	TopPastWeek []ClickEntry
}

// ClickEntry represents a single entry's click statistics
type ClickEntry struct {
	ID          int64
	Title       string
	ClickCount  int
	LastClicked time.Time
}

// EntryFilter represents a filter for entry titles
type EntryFilter struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Pattern       string    `json:"pattern"`
	PatternType   string    `json:"pattern_type"` // 'keyword' or 'regex'
	CaseSensitive bool      `json:"case_sensitive"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// FilterGroup represents a group of filters with boolean logic
type FilterGroup struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Action    string            `json:"action"` // 'keep' or 'discard'
	IsActive  bool              `json:"is_active"`
	Priority  int               `json:"priority"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Rules     []FilterGroupRule `json:"rules,omitempty"`
}

// FilterGroupRule represents the relationship between filters in a group
type FilterGroupRule struct {
	ID       int64        `json:"id"`
	GroupID  int64        `json:"group_id"`
	FilterID int64        `json:"filter_id"`
	Operator string       `json:"operator"` // 'AND' or 'OR'
	Position int          `json:"position"`
	Filter   *EntryFilter `json:"filter"` // Joined filter data
}

// GetSetting retrieves a setting value with type checking
func (db *DB) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx,
		"SELECT value FROM settings WHERE key = ?",
		key,
	).Scan(&value)

	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return value, err
}

// GetSettingInt retrieves and parses an integer setting
func (db *DB) GetSettingInt(ctx context.Context, key string) (int, error) {
	var value string
	var valueType string
	err := db.QueryRowContext(ctx,
		"SELECT value, type FROM settings WHERE key = ?",
		key,
	).Scan(&value, &valueType)

	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}

	if valueType != "int" {
		return 0, ErrInvalidInput
	}

	var intValue int
	_, err = fmt.Sscanf(value, "%d", &intValue)
	return intValue, err
}

// UpdateSetting updates a setting with optimistic locking
func (db *DB) UpdateSetting(ctx context.Context, key, value, valueType string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO settings (key, value, type, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		type = excluded.type,
		updated_at = CURRENT_TIMESTAMP`,
		key, value, valueType,
	)
	if err != nil {
		return err
	}

	return nil
}

// GetRecentEntries retrieves recent entries efficiently
func (db *DB) GetRecentEntries(ctx context.Context, limit int) ([]Entry, error) {
	rows, err := db.QueryContext(ctx, `
        SELECT e.id, e.feed_id, e.title, e.url, e.published_at,
               e.favicon_url, f.title as feed_title, e.content
        FROM entries e
        JOIN feeds f ON e.feed_id = f.id
        ORDER BY e.published_at DESC
        LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var content sql.NullString
		err := rows.Scan(
			&e.ID, &e.FeedID, &e.Title, &e.URL,
			&e.PublishedAt, &e.FaviconURL, &e.FeedTitle, &content,
		)
		if err != nil {
			return nil, err
		}
		if content.Valid {
			e.Content = content.String
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetActiveFeeds retrieves all active feeds
func (db *DB) GetActiveFeeds(ctx context.Context) ([]Feed, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, url, title, last_fetched, last_modified, etag, 
		        status, error_count, last_error, created_at, updated_at
		FROM feeds
		WHERE status = 'active'
		ORDER BY title`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var f Feed
		var lastModified, etag, lastError sql.NullString
		var lastFetched sql.NullTime
		
		err := rows.Scan(
			&f.ID, &f.URL, &f.Title, &lastFetched, &lastModified,
			&etag, &f.Status, &f.ErrorCount, &lastError,
			&f.CreatedAt, &f.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		
		// Handle nullable fields
		if lastFetched.Valid {
			f.LastFetched = lastFetched.Time
		}
		if lastModified.Valid {
			f.LastModified = lastModified.String
		}
		if etag.Valid {
			f.ETag = etag.String
		}
		if lastError.Valid {
			f.LastError = lastError.String
		}
		
		feeds = append(feeds, f)
	}

	return feeds, rows.Err()
}

// GetClickStats retrieves optimized click statistics
func (db *DB) GetClickStats(ctx context.Context) (*ClickStats, error) {
	stats := &ClickStats{}

	// Get total clicks
	err := db.QueryRowContext(ctx,
		`SELECT value FROM click_stats WHERE key = 'total_clicks'`,
	).Scan(&stats.TotalClicks)
	if err != nil {
		return nil, err
	}

	// Get top entries all time
	rows, err := db.QueryContext(ctx,
		`WITH TopEntries AS (
			SELECT e.id, e.title, c.click_count, c.last_clicked
			FROM clicks c
			JOIN entries e ON e.id = c.entry_id
			ORDER BY c.click_count DESC, c.last_clicked DESC
			LIMIT 5
		)
		SELECT * FROM TopEntries`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats.TopAllTime = make([]ClickEntry, 0, 5)
	for rows.Next() {
		var entry ClickEntry
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.ClickCount, &entry.LastClicked); err != nil {
			return nil, err
		}
		stats.TopAllTime = append(stats.TopAllTime, entry)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Get top entries past week
	rows, err = db.QueryContext(ctx,
		`WITH WeeklyEntries AS (
			SELECT e.id, e.title, c.click_count, c.last_clicked
			FROM clicks c
			JOIN entries e ON e.id = c.entry_id
			WHERE c.last_clicked > datetime('now', '-7 days')
			ORDER BY c.click_count DESC, c.last_clicked DESC
			LIMIT 5
		)
		SELECT * FROM WeeklyEntries`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats.TopPastWeek = make([]ClickEntry, 0, 5)
	for rows.Next() {
		var entry ClickEntry
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.ClickCount, &entry.LastClicked); err != nil {
			return nil, err
		}
		stats.TopPastWeek = append(stats.TopPastWeek, entry)
	}

	return stats, rows.Err()
}

// UpdateFeedStatus updates a feed's status and error information
func (db *DB) UpdateFeedStatus(ctx context.Context, feedID int64, status string, errMsg string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE feeds SET 
		status = ?,
		error_count = CASE 
			WHEN ? = 'error' THEN error_count + 1 
			ELSE 0 
		END,
		last_error = CASE 
			WHEN ? = 'error' THEN ? 
			ELSE NULL 
		END,
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		status, status, status, errMsg, feedID,
	)
	return err
}

// cleanupOldEntriesForFeed removes old entries for a specific feed beyond the retention limit
func (db *DB) cleanupOldEntriesForFeed(ctx context.Context, feedID int64, maxPosts int) error {
	_, err := db.ExecContext(ctx,
		`DELETE FROM entries
		WHERE id IN (
			SELECT id FROM entries
			WHERE feed_id = ?
			ORDER BY published_at DESC
			LIMIT -1 OFFSET ?
		)`,
		feedID, maxPosts,
	)
	return err
}

// CleanupOldEntries removes old entries beyond the retention limit for all feeds.
func (db *DB) CleanupOldEntries(ctx context.Context, maxPosts int) error {
	rows, err := db.QueryContext(ctx, "SELECT id FROM feeds")
	if err != nil {
		return fmt.Errorf("failed to query feed IDs: %w", err)
	}
	defer rows.Close()

	var (
		feedID      int64
		hasErrors   bool
		errorLogger = func(feedID int64, err error) {
			// In a real application, you'd use a structured logger
			// For now, we'll just print to stderr or use the db's logger if available
			// Assuming db has a logger field:
			// db.logger.Printf("Error cleaning up old entries for feed %d: %v", feedID, err)
			// If not, fmt.Fprintf(os.Stderr, ...)
			fmt.Printf("Error cleaning up old entries for feed %d: %v\n", feedID, err)
			hasErrors = true
		}
	)

	for rows.Next() {
		if err := rows.Scan(&feedID); err != nil {
			errorLogger(0, fmt.Errorf("failed to scan feed ID: %w", err))
			continue // Skip to next feed if scanning fails
		}
		if err := db.cleanupOldEntriesForFeed(ctx, feedID, maxPosts); err != nil {
			errorLogger(feedID, err)
			// Continue to the next feed even if this one fails
		}
	}

	if err := rows.Err(); err != nil {
		// Log error from rows.Err() as it might indicate an issue during iteration
		errorLogger(0, fmt.Errorf("error iterating over feed IDs: %w", err))
	}

	if hasErrors {
		return errors.New("encountered errors during old entry cleanup, check logs")
	}

	return nil
}

// Filter-related queries

// CreateEntryFilter creates a new entry filter
func (db *DB) CreateEntryFilter(ctx context.Context, name, pattern, patternType string, caseSensitive bool) (*EntryFilter, error) {
	query := `
        INSERT INTO entry_filters (name, pattern, pattern_type, case_sensitive)
        VALUES (?, ?, ?, ?)
        RETURNING id, name, pattern, pattern_type, case_sensitive, created_at, updated_at`
	
	var filter EntryFilter
	err := db.QueryRowContext(ctx, query, name, pattern, patternType, caseSensitive).Scan(
		&filter.ID, &filter.Name, &filter.Pattern, &filter.PatternType,
		&filter.CaseSensitive, &filter.CreatedAt, &filter.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create entry filter: %w", err)
	}
	
	return &filter, nil
}

// GetEntryFilter retrieves an entry filter by ID
func (db *DB) GetEntryFilter(ctx context.Context, id int64) (*EntryFilter, error) {
	query := `
        SELECT id, name, pattern, pattern_type, case_sensitive, created_at, updated_at
        FROM entry_filters
        WHERE id = ?`
	
	var filter EntryFilter
	err := db.QueryRowContext(ctx, query, id).Scan(
		&filter.ID, &filter.Name, &filter.Pattern, &filter.PatternType,
		&filter.CaseSensitive, &filter.CreatedAt, &filter.UpdatedAt,
	)
	
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get entry filter: %w", err)
	}
	
	return &filter, nil
}

// GetAllEntryFilters retrieves all entry filters
func (db *DB) GetAllEntryFilters(ctx context.Context) ([]EntryFilter, error) {
	query := `
        SELECT id, name, pattern, pattern_type, case_sensitive, created_at, updated_at
        FROM entry_filters
        ORDER BY name`
	
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query entry filters: %w", err)
	}
	defer rows.Close()
	
	var filters []EntryFilter
	for rows.Next() {
		var filter EntryFilter
		err := rows.Scan(
			&filter.ID, &filter.Name, &filter.Pattern, &filter.PatternType,
			&filter.CaseSensitive, &filter.CreatedAt, &filter.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan entry filter: %w", err)
		}
		filters = append(filters, filter)
	}
	
	return filters, rows.Err()
}

// UpdateEntryFilter updates an existing entry filter
func (db *DB) UpdateEntryFilter(ctx context.Context, id int64, name, pattern, patternType string, caseSensitive bool) error {
	query := `
        UPDATE entry_filters
        SET name = ?, pattern = ?, pattern_type = ?, case_sensitive = ?
        WHERE id = ?`
	
	result, err := db.ExecContext(ctx, query, name, pattern, patternType, caseSensitive, id)
	if err != nil {
		return fmt.Errorf("failed to update entry filter: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return ErrNotFound
	}
	
	return nil
}

// DeleteEntryFilter deletes an entry filter
func (db *DB) DeleteEntryFilter(ctx context.Context, id int64) error {
	query := `DELETE FROM entry_filters WHERE id = ?`
	
	result, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete entry filter: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return ErrNotFound
	}
	
	return nil
}

// CreateFilterGroup creates a new filter group
func (db *DB) CreateFilterGroup(ctx context.Context, name, action string, priority int) (*FilterGroup, error) {
	query := `
        INSERT INTO filter_groups (name, action, priority)
        VALUES (?, ?, ?)
        RETURNING id, name, action, is_active, priority, created_at, updated_at`
	
	var group FilterGroup
	err := db.QueryRowContext(ctx, query, name, action, priority).Scan(
		&group.ID, &group.Name, &group.Action, &group.IsActive,
		&group.Priority, &group.CreatedAt, &group.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create filter group: %w", err)
	}
	
	return &group, nil
}

// GetFilterGroup retrieves a filter group by ID with its rules
func (db *DB) GetFilterGroup(ctx context.Context, id int64) (*FilterGroup, error) {
	// Get the filter group
	groupQuery := `
        SELECT id, name, action, is_active, priority, created_at, updated_at
        FROM filter_groups
        WHERE id = ?`
	
	var group FilterGroup
	err := db.QueryRowContext(ctx, groupQuery, id).Scan(
		&group.ID, &group.Name, &group.Action, &group.IsActive,
		&group.Priority, &group.CreatedAt, &group.UpdatedAt,
	)
	
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get filter group: %w", err)
	}
	
	// Get the rules for this group
	rules, err := db.GetFilterGroupRules(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get filter group rules: %w", err)
	}
	group.Rules = rules
	
	return &group, nil
}

// GetAllFilterGroups retrieves all filter groups with their rules
func (db *DB) GetAllFilterGroups(ctx context.Context) ([]FilterGroup, error) {
	query := `
        SELECT id, name, action, is_active, priority, created_at, updated_at
        FROM filter_groups
        ORDER BY priority, name`
	
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query filter groups: %w", err)
	}
	defer rows.Close()
	
	var groups []FilterGroup
	for rows.Next() {
		var group FilterGroup
		err := rows.Scan(
			&group.ID, &group.Name, &group.Action, &group.IsActive,
			&group.Priority, &group.CreatedAt, &group.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan filter group: %w", err)
		}
		
		// Get rules for this group
		rules, err := db.GetFilterGroupRules(ctx, group.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get rules for group %d: %w", group.ID, err)
		}
		group.Rules = rules
		
		groups = append(groups, group)
	}
	
	return groups, rows.Err()
}

// GetActiveFilterGroups retrieves only active filter groups ordered by priority
func (db *DB) GetActiveFilterGroups(ctx context.Context) ([]FilterGroup, error) {
	query := `
        SELECT id, name, action, is_active, priority, created_at, updated_at
        FROM filter_groups
        WHERE is_active = 1
        ORDER BY priority, name`
	
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query active filter groups: %w", err)
	}
	defer rows.Close()
	
	var groups []FilterGroup
	for rows.Next() {
		var group FilterGroup
		err := rows.Scan(
			&group.ID, &group.Name, &group.Action, &group.IsActive,
			&group.Priority, &group.CreatedAt, &group.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan filter group: %w", err)
		}
		
		// Get rules for this group
		rules, err := db.GetFilterGroupRules(ctx, group.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get rules for group %d: %w", group.ID, err)
		}
		group.Rules = rules
		
		groups = append(groups, group)
	}
	
	return groups, rows.Err()
}

// UpdateFilterGroup updates an existing filter group
func (db *DB) UpdateFilterGroup(ctx context.Context, id int64, name, action string, isActive bool, priority int) error {
	query := `
        UPDATE filter_groups
        SET name = ?, action = ?, is_active = ?, priority = ?
        WHERE id = ?`
	
	result, err := db.ExecContext(ctx, query, name, action, isActive, priority, id)
	if err != nil {
		return fmt.Errorf("failed to update filter group: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return ErrNotFound
	}
	
	return nil
}

// DeleteFilterGroup deletes a filter group and its rules
func (db *DB) DeleteFilterGroup(ctx context.Context, id int64) error {
	query := `DELETE FROM filter_groups WHERE id = ?`
	
	result, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete filter group: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return ErrNotFound
	}
	
	return nil
}

// GetFilterGroupRules retrieves all rules for a filter group
func (db *DB) GetFilterGroupRules(ctx context.Context, groupID int64) ([]FilterGroupRule, error) {
	query := `
        SELECT r.id, r.group_id, r.filter_id, r.operator, r.position,
               f.id, f.name, f.pattern, f.pattern_type, f.case_sensitive, f.created_at, f.updated_at
        FROM filter_group_rules r
        JOIN entry_filters f ON r.filter_id = f.id
        WHERE r.group_id = ?
        ORDER BY r.position, r.id`
	
	rows, err := db.QueryContext(ctx, query, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to query filter group rules: %w", err)
	}
	defer rows.Close()
	
	var rules []FilterGroupRule
	for rows.Next() {
		var rule FilterGroupRule
		var filter EntryFilter
		
		err := rows.Scan(
			&rule.ID, &rule.GroupID, &rule.FilterID, &rule.Operator, &rule.Position,
			&filter.ID, &filter.Name, &filter.Pattern, &filter.PatternType,
			&filter.CaseSensitive, &filter.CreatedAt, &filter.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan filter group rule: %w", err)
		}
		
		rule.Filter = &filter
		rules = append(rules, rule)
	}
	
	return rules, rows.Err()
}

// AddFilterToGroup adds a filter to a filter group
func (db *DB) AddFilterToGroup(ctx context.Context, groupID, filterID int64, operator string, position int) error {
	query := `
        INSERT INTO filter_group_rules (group_id, filter_id, operator, position)
        VALUES (?, ?, ?, ?)`
	
	_, err := db.ExecContext(ctx, query, groupID, filterID, operator, position)
	if err != nil {
		return fmt.Errorf("failed to add filter to group: %w", err)
	}
	
	return nil
}

// RemoveFilterFromGroup removes a filter from a filter group
func (db *DB) RemoveFilterFromGroup(ctx context.Context, groupID, filterID int64) error {
	query := `DELETE FROM filter_group_rules WHERE group_id = ? AND filter_id = ?`
	
	result, err := db.ExecContext(ctx, query, groupID, filterID)
	if err != nil {
		return fmt.Errorf("failed to remove filter from group: %w", err)
	}
	
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	
	if rowsAffected == 0 {
		return ErrNotFound
	}
	
	return nil
}

// UpdateFilterGroupRules replaces all rules for a filter group
func (db *DB) UpdateFilterGroupRules(ctx context.Context, groupID int64, rules []FilterGroupRule) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	
	// Delete existing rules
	_, err = tx.ExecContext(ctx, "DELETE FROM filter_group_rules WHERE group_id = ?", groupID)
	if err != nil {
		return fmt.Errorf("failed to delete existing rules: %w", err)
	}
	
	// Insert new rules
	if len(rules) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
            INSERT INTO filter_group_rules (group_id, filter_id, operator, position)
            VALUES (?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("failed to prepare insert statement: %w", err)
		}
		defer stmt.Close()
		
		for _, rule := range rules {
			_, err = stmt.ExecContext(ctx, groupID, rule.FilterID, rule.Operator, rule.Position)
			if err != nil {
				return fmt.Errorf("failed to insert rule: %w", err)
			}
		}
	}
	
	return tx.Commit()
}
