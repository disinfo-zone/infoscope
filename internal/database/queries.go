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
	result, err := db.ExecContext(ctx,
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

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// GetRecentEntries retrieves recent entries efficiently
func (db *DB) GetRecentEntries(ctx context.Context, limit int) ([]Entry, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT e.id, e.feed_id, e.title, e.url, e.published_at, e.favicon_url,
		        f.title as feed_title
		FROM entries e
		JOIN feeds f ON e.feed_id = f.id
		WHERE f.status = 'active'
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
		err := rows.Scan(
			&e.ID, &e.FeedID, &e.Title, &e.URL,
			&e.PublishedAt, &e.FaviconURL, &e.FeedTitle,
		)
		if err != nil {
			return nil, err
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
		err := rows.Scan(
			&f.ID, &f.URL, &f.Title, &f.LastFetched, &f.LastModified,
			&f.ETag, &f.Status, &f.ErrorCount, &f.LastError,
			&f.CreatedAt, &f.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}

	return feeds, rows.Err()
}

// IncrementClicks atomically updates click counts
func (db *DB) IncrementClicks(ctx context.Context, entryID int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update entry clicks
	_, err = tx.ExecContext(ctx,
		`INSERT INTO clicks (entry_id, click_count, last_clicked)
		VALUES (?, 1, CURRENT_TIMESTAMP)
		ON CONFLICT(entry_id) DO UPDATE SET
		click_count = click_count + 1,
		last_clicked = CURRENT_TIMESTAMP`,
		entryID,
	)
	if err != nil {
		return err
	}

	// Update total clicks atomically
	_, err = tx.ExecContext(ctx,
		`UPDATE click_stats 
		SET value = value + 1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE key = 'total_clicks'`,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
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

// CleanupOldEntries removes old entries beyond the retention limit
func (db *DB) CleanupOldEntries(ctx context.Context, maxPosts int) error {
	_, err := db.ExecContext(ctx,
		`DELETE FROM entries 
		WHERE id IN (
			SELECT e.id 
			FROM entries e
			JOIN feeds f ON e.feed_id = f.id
			WHERE e.id NOT IN (
				SELECT e2.id
				FROM entries e2
				WHERE e2.feed_id = f.id
				ORDER BY published_at DESC
				LIMIT ?
			)
		)`,
		maxPosts,
	)
	return err
}
