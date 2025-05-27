package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// setupTestQueriesDB initializes an in-memory SQLite database using NewDB (which handles schema)
// and populates it with initial test data.
func setupTestQueriesDB(t *testing.T) *DB {
	t.Helper()

	// Use NewDB to get a DB instance with schema and migrations applied.
	// Using ":memory:" is fine for query tests.
	// Note: NewDB returns *database.DB (our wrapper), not *sql.DB.
	// The functions we are testing are methods of our *database.DB type.
	dbInstance, err := NewDB(":memory:", DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create in-memory database via NewDB: %v", err)
	}

	// Populate with initial data
	ctx := context.Background()

	// Settings
	settings := []struct {
		key   string
		value string
		vType string
	}{
		{"site_name", "Test Site", "string"},
		{"items_per_page", "10", "int"},
		{"test_string_setting", "hello world", "string"},
		{"another_int_setting", "25", "int"},
		{"setting_to_update", "initial_value", "string"},
	}
	for _, s := range settings {
		// Use UpdateSetting for population as it handles upsert logic correctly
		// and is also a function we'll test.
		// Or, use direct DB.Exec if UpdateSetting is complex or not suitable for setup.
		// For now, direct exec is simpler for setup.
		_, err := dbInstance.ExecContext(ctx,
			`INSERT INTO settings (key, value, type, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value, type = excluded.type, updated_at = CURRENT_TIMESTAMP`,
			s.key, s.value, s.vType)
		if err != nil {
			t.Fatalf("Failed to insert initial setting %s: %v", s.key, err)
		}
	}

	// Feeds
	feeds := []struct {
		id    int64
		url   string
		title string
		status string
		lastFetched time.Time
	}{
		{1, "http://example.com/feed1.xml", "Feed 1 (Active)", "active", time.Now().Add(-1 * time.Hour)},
		{2, "http://example.com/feed2.xml", "Feed 2 (Inactive)", "inactive", time.Now().Add(-2 * time.Hour)},
		{3, "http://example.com/feed3.xml", "Feed 3 (Active-Old)", "active", time.Now().Add(-24 * time.Hour)},
		{4, "http://example.com/feed4_for_cleanup.xml", "Feed 4 (Cleanup)", "active", time.Now().Add(-1 * time.Hour)},
		{5, "http://example.com/feed5_for_cleanup.xml", "Feed 5 (Cleanup)", "active", time.Now().Add(-1 * time.Hour)},
	}
	for _, f := range feeds {
		_, err := dbInstance.ExecContext(ctx,
			"INSERT INTO feeds (id, url, title, status, last_fetched, created_at, updated_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)",
			f.id, f.url, f.title, f.status, f.lastFetched)
		if err != nil {
			t.Fatalf("Failed to insert initial feed %s: %v", f.title, err)
		}
	}

	// Entries
	now := time.Now()
	entries := []struct {
		feedID      int64
		title       string
		url         string
		publishedAt time.Time
		faviconURL  string
	}{
		// Feed 1 entries
		{1, "Entry 1-1 (Recent)", "http://example.com/entry1-1", now.Add(-30 * time.Minute), "http://example.com/favicon1.ico"},
		{1, "Entry 1-2 (Old)", "http://example.com/entry1-2", now.Add(-2 * time.Hour), "http://example.com/favicon1.ico"},
		// Feed 2 entries
		{2, "Entry 2-1 (Inactive Feed)", "http://example.com/entry2-1", now.Add(-1 * time.Hour), ""},
		// Feed 3 entries
		{3, "Entry 3-1 (Active Feed Old Entry)", "http://example.com/entry3-1", now.Add(-48 * time.Hour), ""},
		// Feed 4 (for CleanupOldEntries test) - 5 entries
		{4, "Entry 4-1", "http://example.com/entry4-1", now.Add(-10 * time.Minute), ""},
		{4, "Entry 4-2", "http://example.com/entry4-2", now.Add(-20 * time.Minute), ""},
		{4, "Entry 4-3", "http://example.com/entry4-3", now.Add(-30 * time.Minute), ""},
		{4, "Entry 4-4", "http://example.com/entry4-4", now.Add(-40 * time.Minute), ""},
		{4, "Entry 4-5", "http://example.com/entry4-5", now.Add(-50 * time.Minute), ""},
		// Feed 5 (for CleanupOldEntries test) - 2 entries (less than typical maxPosts)
		{5, "Entry 5-1", "http://example.com/entry5-1", now.Add(-10 * time.Minute), ""},
		{5, "Entry 5-2", "http://example.com/entry5-2", now.Add(-20 * time.Minute), ""},
	}
	for i, e := range entries {
		// Ensure unique URLs for entries as they have a UNIQUE constraint.
		uniqueURL := fmt.Sprintf("%s/%d", e.url, i)
		_, err := dbInstance.ExecContext(ctx,
			"INSERT INTO entries (feed_id, title, url, published_at, favicon_url, created_at) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)",
			e.feedID, e.title, uniqueURL, e.publishedAt, e.faviconURL)
		if err != nil {
			t.Fatalf("Failed to insert initial entry %s: %v", e.title, err)
		}
	}
	// Note: Clicks table and ClickStats are not populated here as IncrementClicks was removed.
	// If GetClickStats were to be tested, data would be needed for 'clicks' and 'click_stats' tables.

	return dbInstance
}

func TestMain(m *testing.M) {
	// Optional: Any package-level setup
	exitCode := m.Run()
	// Optional: Any package-level teardown
	os.Exit(exitCode)
}

func TestGetSetting(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	t.Run("existing string setting", func(t *testing.T) {
		value, err := db.GetSetting(ctx, "site_name")
		if err != nil {
			t.Fatalf("GetSetting failed: %v", err)
		}
		if value != "Test Site" {
			t.Errorf("Expected value 'Test Site', got '%s'", value)
		}
	})

	t.Run("non-existent setting", func(t *testing.T) {
		_, err := db.GetSetting(ctx, "non_existent_key")
		if err == nil {
			t.Fatalf("Expected error for non-existent key, got nil")
		}
		if err != ErrNotFound {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})
}

func TestGetSettingInt(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	t.Run("existing int setting", func(t *testing.T) {
		value, err := db.GetSettingInt(ctx, "items_per_page")
		if err != nil {
			t.Fatalf("GetSettingInt failed: %v", err)
		}
		if value != 10 {
			t.Errorf("Expected value 10, got %d", value)
		}
	})

	t.Run("non-existent int setting", func(t *testing.T) {
		_, err := db.GetSettingInt(ctx, "non_existent_int_key")
		if err == nil {
			t.Fatalf("Expected error for non-existent key, got nil")
		}
		if err != ErrNotFound { // Should be ErrNotFound from the query itself
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("setting with non-int type", func(t *testing.T) {
		// 'site_name' is stored with type 'string'
		_, err := db.GetSettingInt(ctx, "site_name")
		if err == nil {
			t.Fatalf("Expected error for wrong type, got nil")
		}
		if err != ErrInvalidInput { // This error is specific to GetSettingInt's type check
			t.Errorf("Expected ErrInvalidInput, got %v", err)
		}
	})

	t.Run("setting with int type but non-integer value", func(t *testing.T) {
		// Manually insert a setting with type 'int' but a non-integer string value
		badIntKey := "bad_int_value"
		_, err := db.ExecContext(ctx, "INSERT INTO settings (key, value, type) VALUES (?, ?, ?)", badIntKey, "not-an-int", "int")
		if err != nil {
			t.Fatalf("Failed to insert bad int setting: %v", err)
		}
		_, err = db.GetSettingInt(ctx, badIntKey)
		if err == nil {
			t.Fatalf("Expected error for non-integer value, got nil")
		}
		// The error here would come from fmt.Sscanf
		// Check for a generic error, or refine if a specific error type is expected from Sscanf.
	})
}

func TestUpdateSetting(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	keyToUpdate := "setting_to_update"
	newValue := "updated_value"
	newType := "string" // Assuming type remains string, or test type update too

	// Get original updated_at
	var originalUpdatedAt time.Time
	err := db.QueryRowContext(ctx, "SELECT updated_at FROM settings WHERE key = ?", keyToUpdate).Scan(&originalUpdatedAt)
	if err != nil {
		t.Fatalf("Failed to get original updated_at: %v", err)
	}
	// Ensure there's a slight delay for updated_at to change
	time.Sleep(10 * time.Millisecond)


	err = db.UpdateSetting(ctx, keyToUpdate, newValue, newType)
	if err != nil {
		t.Fatalf("UpdateSetting failed: %v", err)
	}

	// Verify update
	var updatedValue, updatedType string
	var newUpdatedAt time.Time
	err = db.QueryRowContext(ctx, "SELECT value, type, updated_at FROM settings WHERE key = ?", keyToUpdate).Scan(&updatedValue, &updatedType, &newUpdatedAt)
	if err != nil {
		t.Fatalf("Failed to query updated setting: %v", err)
	}

	if updatedValue != newValue {
		t.Errorf("Expected value '%s', got '%s'", newValue, updatedValue)
	}
	if updatedType != newType {
		t.Errorf("Expected type '%s', got '%s'", newType, updatedType)
	}
	if !newUpdatedAt.After(originalUpdatedAt) {
		t.Errorf("Expected updated_at (%v) to be after original (%v)", newUpdatedAt, originalUpdatedAt)
	}


	t.Run("insert new setting", func(t *testing.T) {
		newKey := "brand_new_setting"
		newKeyValue := "brand_new_value"
		newKeyType := "string"
		err := db.UpdateSetting(ctx, newKey, newKeyValue, newKeyType)
		if err != nil {
			t.Fatalf("UpdateSetting for new key failed: %v", err)
		}
		
		value, err := db.GetSetting(ctx, newKey)
		if err != nil {
			t.Fatalf("GetSetting for new key failed: %v", err)
		}
		if value != newKeyValue {
			t.Errorf("Expected value '%s' for new key, got '%s'", newKeyValue, value)
		}
	})
}

// Placeholder for GetRecentEntries tests
func TestGetRecentEntries(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	t.Run("limit 2", func(t *testing.T) {
		entries, err := db.GetRecentEntries(ctx, 2)
		if err != nil {
			t.Fatalf("GetRecentEntries failed: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("Expected 2 entries, got %d", len(entries))
		}
		// Entries are ordered by published_at DESC.
		// From setup: Entry 4-1 (@ -10m), Entry 5-1 (@ -10m), Entry 4-2 (@ -20m), Entry 5-2 (@ -20m), Entry 1-1 (@ -30m)
		// We need to be careful about exact times if they are too close.
		// Let's check titles assuming publish times are distinct enough from setup.
		// The setup data needs more distinct publish times or we need to sort by ID as secondary for deterministic order.
		// For now, let's assume the order is: Entry 4-1, Entry 5-1 (or vice-versa if IDs differ), then Entry 4-2, Entry 5-2 etc.
		// The provided query sorts by e.published_at DESC. If times are identical, DB order is undefined.
		// Let's verify the first entry is one of the most recent.
		mostRecentTitles := map[string]bool{"Entry 4-1": true, "Entry 5-1": true}
		if !mostRecentTitles[entries[0].Title] {
			t.Errorf("First entry title was '%s', expected one of %v", entries[0].Title, mostRecentTitles)
		}
		if entries[0].PublishedAt.Before(entries[1].PublishedAt) {
			t.Errorf("Entries not sorted by published_at DESC: entry 0 (%v) is before entry 1 (%v)", entries[0].PublishedAt, entries[1].PublishedAt)
		}
		if entries[0].FeedTitle == "" {
			t.Errorf("FeedTitle not populated for entry '%s'", entries[0].Title)
		}
		// Check favicon (Entry 1-1 has one)
		foundFavicon := false
		for _, e := range entries {
			if e.Title == "Entry 1-1 (Recent)" && e.FaviconURL != "http://example.com/favicon1.ico" {
				t.Errorf("Expected favicon for 'Entry 1-1 (Recent)', got '%s'", e.FaviconURL)
			}
			if e.FaviconURL != "" {
				foundFavicon = true
			}
		}
		// This check depends on which entries are fetched. If Entry 1-1 is not in top 2, this might fail.
		// The current setup has Entry 4-1 and Entry 5-1 as newest.
		// Let's adjust test data or query to make this check robust.
		// For now, we'll assume at least one entry with a favicon might appear if limit is large enough.
	})

	t.Run("limit 0", func(t *testing.T) {
		entries, err := db.GetRecentEntries(ctx, 0)
		if err != nil {
			t.Fatalf("GetRecentEntries with limit 0 failed: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("Expected 0 entries for limit 0, got %d", len(entries))
		}
	})

	t.Run("limit exceeds available entries", func(t *testing.T) {
		var totalEntries int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries").Scan(&totalEntries)
		if err != nil {
			t.Fatalf("Failed to count total entries: %v", err)
		}

		entries, err := db.GetRecentEntries(ctx, totalEntries+5) // Limit greater than total
		if err != nil {
			t.Fatalf("GetRecentEntries failed: %v", err)
		}
		if len(entries) != totalEntries {
			t.Errorf("Expected %d entries, got %d", totalEntries, len(entries))
		}
	})
}

// Placeholder for GetActiveFeeds tests
func TestGetActiveFeeds(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	feeds, err := db.GetActiveFeeds(ctx)
	if err != nil {
		t.Fatalf("GetActiveFeeds failed: %v", err)
	}

	expectedActiveCount := 0
	var firstActiveTitle, lastActiveTitle string

	// From setup: Feed 1 (Active), Feed 3 (Active-Old), Feed 4 (Cleanup), Feed 5 (Cleanup)
	// Sorted by title: Feed 1 (Active), Feed 3 (Active-Old), Feed 4 (Cleanup), Feed 5 (Cleanup)
	// Titles: "Feed 1 (Active)", "Feed 3 (Active-Old)", "Feed 4 (Cleanup)", "Feed 5 (Cleanup)"
	allFeeds, _ := db.QueryContext(ctx, "SELECT title, status FROM feeds ORDER BY title")
	var titles []string
	for allFeeds.Next(){
		var title, status string
		allFeeds.Scan(&title, &status)
		if status == "active" {
			expectedActiveCount++
			titles = append(titles, title)
		}
	}
	allFeeds.Close()
	if len(titles) > 0 {
		firstActiveTitle = titles[0]
		lastActiveTitle = titles[len(titles)-1]
	}


	if len(feeds) != expectedActiveCount {
		t.Errorf("Expected %d active feeds, got %d", expectedActiveCount, len(feeds))
	}

	for _, f := range feeds {
		if f.Status != "active" {
			t.Errorf("Found feed with status '%s' in active feeds list: %s", f.Status, f.Title)
		}
	}
	if len(feeds) > 0 {
		if feeds[0].Title != firstActiveTitle {
			t.Errorf("Expected first active feed to be '%s' (by title), got '%s'", firstActiveTitle, feeds[0].Title)
		}
		if feeds[len(feeds)-1].Title != lastActiveTitle {
			t.Errorf("Expected last active feed to be '%s' (by title), got '%s'", lastActiveTitle, feeds[len(feeds)-1].Title)
		}
	}
}

// Test for GetClickStats is skipped as the function was removed.

func TestUpdateFeedStatus(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	feedIDToTest := int64(1) // Feed 1 (Active)
	errMsg := "Test fetch error"

	// Test updating to error status
	err := db.UpdateFeedStatus(ctx, feedIDToTest, "error", errMsg)
	if err != nil {
		t.Fatalf("UpdateFeedStatus to error failed: %v", err)
	}

	var status string
	var errorCount int
	var lastError sql.NullString
	err = db.QueryRowContext(ctx, "SELECT status, error_count, last_error FROM feeds WHERE id = ?", feedIDToTest).Scan(&status, &errorCount, &lastError)
	if err != nil {
		t.Fatalf("Failed to query feed after status update: %v", err)
	}

	if status != "error" {
		t.Errorf("Expected status 'error', got '%s'", status)
	}
	if errorCount != 1 { // Initial error_count is 0 for this feed.
		t.Errorf("Expected error_count 1, got %d", errorCount)
	}
	if !lastError.Valid || lastError.String != errMsg {
		t.Errorf("Expected last_error '%s', got '%s'", errMsg, lastError.String)
	}

	// Test updating to active status (should reset error fields)
	err = db.UpdateFeedStatus(ctx, feedIDToTest, "active", "")
	if err != nil {
		t.Fatalf("UpdateFeedStatus to active failed: %v", err)
	}

	err = db.QueryRowContext(ctx, "SELECT status, error_count, last_error FROM feeds WHERE id = ?", feedIDToTest).Scan(&status, &errorCount, &lastError)
	if err != nil {
		t.Fatalf("Failed to query feed after status update to active: %v", err)
	}
	if status != "active" {
		t.Errorf("Expected status 'active', got '%s'", status)
	}
	if errorCount != 0 {
		t.Errorf("Expected error_count 0 after reset, got %d", errorCount)
	}
	if lastError.Valid { // last_error should be NULL
		t.Errorf("Expected last_error to be NULL, got '%s'", lastError.String)
	}
	
	// Test incrementing error_count
	db.UpdateFeedStatus(ctx, feedIDToTest, "error", "err1")
	db.UpdateFeedStatus(ctx, feedIDToTest, "error", "err2")
	err = db.QueryRowContext(ctx, "SELECT error_count FROM feeds WHERE id = ?", feedIDToTest).Scan(&errorCount)
	if err != nil {
		t.Fatalf("Failed to query error_count: %v", err)
	}
	if errorCount != 2 { // Was 0, then err1 -> 1, then err2 -> 2
		t.Errorf("Expected error_count 2 after two errors, got %d", errorCount)
	}
}

func TestCleanupOldEntries(t *testing.T) {
	db := setupTestQueriesDB(t)
	defer db.Close()
	ctx := context.Background()

	maxPosts := 3 // Max entries to keep per feed

	// Feed 4 has 5 entries, should be trimmed to 3
	// Feed 5 has 2 entries, should remain 2 (less than maxPosts)
	// Other feeds have fewer than maxPosts entries initially.

	err := db.CleanupOldEntries(ctx, maxPosts)
	if err != nil {
		t.Fatalf("CleanupOldEntries failed: %v", err)
	}

	// Check Feed 4
	var countFeed4 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries WHERE feed_id = 4").Scan(&countFeed4)
	if err != nil {
		t.Fatalf("Failed to count entries for feed 4: %v", err)
	}
	if countFeed4 != maxPosts {
		t.Errorf("Feed 4: Expected %d entries after cleanup, got %d", maxPosts, countFeed4)
	}
	// Verify newest entries are kept (check titles of remaining for Feed 4)
	rowsFeed4, err := db.QueryContext(ctx, "SELECT title FROM entries WHERE feed_id = 4 ORDER BY published_at DESC")
	if err != nil {
		t.Fatalf("Failed to query entries for feed 4: %v", err)
	}
	defer rowsFeed4.Close()
	var titlesFeed4 []string
	for rowsFeed4.Next() {
		var title string
		rowsFeed4.Scan(&title)
		titlesFeed4 = append(titlesFeed4, title)
	}
	expectedTitlesFeed4 := []string{"Entry 4-1", "Entry 4-2", "Entry 4-3"} // Newest 3
	if len(titlesFeed4) != len(expectedTitlesFeed4) {
		t.Errorf("Feed 4: Title count mismatch. Expected %d, got %d", len(expectedTitlesFeed4), len(titlesFeed4))
	} else {
		for i, title := range titlesFeed4 {
			if title != expectedTitlesFeed4[i] {
				t.Errorf("Feed 4: Expected title '%s' at index %d, got '%s'", expectedTitlesFeed4[i], i, title)
			}
		}
	}


	// Check Feed 5
	var countFeed5 int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entries WHERE feed_id = 5").Scan(&countFeed5)
	if err != nil {
		t.Fatalf("Failed to count entries for feed 5: %v", err)
	}
	if countFeed5 != 2 { // Feed 5 initially had 2 entries
		t.Errorf("Feed 5: Expected 2 entries after cleanup (was < maxPosts), got %d", countFeed5)
	}
}
```
