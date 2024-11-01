// Save as: internal/feed/feed_test.go
// (Delete internal/feed/test.go if it exists)
package feed

import (
	"context"
	"database/sql"
	"log"
	"os"
	"testing"
	"time"

	"infoscope/internal/favicon"

	_ "github.com/mattn/go-sqlite3"
)

type testEnv struct {
	db         *sql.DB
	logger     *log.Logger
	faviconSvc *favicon.Service
	service    *Service
	fetcher    *Fetcher
}

func setupTest(t *testing.T) *testEnv {
	// Create test database
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create tables
	_, err = db.Exec(`
        CREATE TABLE feeds (
            id INTEGER PRIMARY KEY,
            url TEXT UNIQUE NOT NULL,
            title TEXT,
            last_fetched TIMESTAMP
        );

        CREATE TABLE entries (
            id INTEGER PRIMARY KEY,
            feed_id INTEGER NOT NULL,
            title TEXT NOT NULL,
            url TEXT UNIQUE NOT NULL,
            published_at TIMESTAMP NOT NULL,
            favicon_url TEXT,
            FOREIGN KEY (feed_id) REFERENCES feeds(id)
        );

        CREATE TABLE settings (
            key TEXT PRIMARY KEY,
            value TEXT NOT NULL
        );
    `)
	if err != nil {
		t.Fatalf("Failed to create test tables: %v", err)
	}

	// Create test logger
	logger := log.New(os.Stdout, "test: ", log.LstdFlags)

	// Create temporary favicon directory
	tempDir := t.TempDir()
	faviconSvc, err := favicon.NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create favicon service: %v", err)
	}

	// Create service and fetcher
	service := NewService(db, logger, faviconSvc)
	fetcher := NewFetcher(db, logger, faviconSvc)

	return &testEnv{
		db:         db,
		logger:     logger,
		faviconSvc: faviconSvc,
		service:    service,
		fetcher:    fetcher,
	}
}

func TestService(t *testing.T) {
	env := setupTest(t)
	defer env.db.Close()

	// Test: Add feed
	t.Run("Add feed", func(t *testing.T) {
		err := env.service.AddFeed("https://blog.golang.org/feed.atom")
		if err != nil {
			t.Fatalf("Failed to add feed: %v", err)
		}

		// Verify feed was added
		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count feeds: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 feed, got %d", count)
		}
	})

	// Test: Update feeds
	t.Run("Update feeds", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err := env.service.UpdateFeeds(ctx)
		if err != nil {
			t.Fatalf("Failed to update feeds: %v", err)
		}

		// Verify entries were fetched
		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count entries: %v", err)
		}
		if count == 0 {
			t.Error("No entries were fetched")
		}
	})

	// Test: Delete feed
	t.Run("Delete feed", func(t *testing.T) {
		// Get the feed ID
		var feedID int64
		err := env.db.QueryRow("SELECT id FROM feeds LIMIT 1").Scan(&feedID)
		if err != nil {
			t.Fatalf("Failed to get feed ID: %v", err)
		}

		// Delete the feed
		err = env.service.DeleteFeed(feedID)
		if err != nil {
			t.Fatalf("Failed to delete feed: %v", err)
		}

		// Verify feed was deleted
		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM feeds").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count feeds: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 feeds after deletion, got %d", count)
		}

		// Verify entries were deleted
		err = env.db.QueryRow("SELECT COUNT(*) FROM entries WHERE feed_id = ?", feedID).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count entries: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 entries after feed deletion, got %d", count)
		}
	})
}

func TestFetcher(t *testing.T) {
	env := setupTest(t)
	defer env.db.Close()

	// Insert test feed
	result, err := env.db.Exec(
		"INSERT INTO feeds (url, title) VALUES (?, ?)",
		"https://blog.golang.org/feed.atom",
		"Go Blog",
	)
	if err != nil {
		t.Fatalf("Failed to insert test feed: %v", err)
	}

	feedID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("Failed to get feed ID: %v", err)
	}

	// Test fetch single feed
	t.Run("Fetch single feed", func(t *testing.T) {
		feed := Feed{
			ID:  feedID,
			URL: "https://blog.golang.org/feed.atom",
		}

		result := env.fetcher.fetchFeed(context.Background(), feed)
		if result.Error != nil {
			t.Fatalf("Failed to fetch feed: %v", result.Error)
		}

		if len(result.Entries) == 0 {
			t.Error("No entries were fetched")
		}
	})

	// Test update all feeds
	t.Run("Update all feeds", func(t *testing.T) {
		err := env.fetcher.UpdateFeeds(context.Background())
		if err != nil {
			t.Fatalf("Failed to update feeds: %v", err)
		}

		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count entries: %v", err)
		}
		if count == 0 {
			t.Error("No entries were saved to database")
		}
	})
}
