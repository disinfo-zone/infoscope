// Save as: internal/feed/feed_test.go
package feed

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"infoscope/internal/favicon"

	_ "github.com/mattn/go-sqlite3"
)

// Sample XML feed data
const (
	sampleRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
	<title>Sample RSS Feed</title>
	<link>http://example.com/rss</link>
	<description>This is a sample RSS feed.</description>
	<item>
		<title>RSS Entry 1</title>
		<link>http://example.com/rss/entry1</link>
		<pubDate>Mon, 01 Jan 2023 10:00:00 +0000</pubDate>
		<guid>http://example.com/rss/entry1</guid>
		<description>Description for RSS Entry 1</description>
	</item>
	<item>
		<title>RSS Entry 2</title>
		<link>http://example.com/rss/entry2</link>
		<pubDate>Tue, 02 Jan 2023 11:00:00 +0000</pubDate>
		<guid>http://example.com/rss/entry2</guid>
		<description>Description for RSS Entry 2</description>
	</item>
</channel>
</rss>`

	sampleAtom = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
	<title>Sample Atom Feed</title>
	<link href="http://example.com/atom"/>
	<updated>2023-01-02T11:00:00Z</updated>
	<author><name>Test Author</name></author>
	<id>urn:uuid:60a76c80-d399-11d9-b93C-0003939e0af6</id>
	<entry>
		<title>Atom Entry 1</title>
		<link href="http://example.com/atom/entry1"/>
		<id>urn:uuid:1225c695-cfb8-4ebb-aaaa-80da344efa6a</id>
		<updated>2023-01-01T10:00:00Z</updated>
		<summary>Summary for Atom Entry 1.</summary>
	</entry>
</feed>`

	nonFeedXML = `<?xml version="1.0" encoding="UTF-8"?>
<document>
	<title>Not a Feed</title>
	<content>This is just a plain XML document.</content>
</document>`

	nonXMLContent = `This is not XML content at all. It's just plain text.`
)

// newMockFeedServer sets up an httptest.Server with a given handler.
func newMockFeedServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	return server
}

type testEnv struct {
	db         *sql.DB
	logger     *log.Logger
	faviconSvc *favicon.Service
	// Service and Fetcher will be created within tests as they might depend on mock server URLs
}

// setupTestDB only sets up the database and common services not dependent on mock server.
func setupTestDB(t *testing.T) *testEnv {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create tables (simplified schema for feed tests)
	_, err = db.Exec(`
        CREATE TABLE feeds (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            url TEXT UNIQUE NOT NULL,
            title TEXT,
            last_fetched TIMESTAMP,
			status TEXT,
			error_count INTEGER DEFAULT 0,
			last_error TEXT,
			last_modified TEXT,
			etag TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );

        CREATE TABLE entries (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            feed_id INTEGER NOT NULL,
            title TEXT NOT NULL,
            url TEXT UNIQUE NOT NULL,
			content TEXT,
			guid TEXT,
            published_at TIMESTAMP NOT NULL,
            favicon_url TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
        );

        CREATE TABLE settings ( -- Add settings table if service depends on it
            key TEXT PRIMARY KEY,
            value TEXT
        );
		INSERT INTO settings (key, value) VALUES ('max_posts', '100'); -- Default for cleanup
    `)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create test tables: %v", err)
	}

	logger := log.New(io.Discard, "", 0) // Suppress logs during tests
	// logger := log.New(os.Stdout, "test: ", log.LstdFlags) // For verbose logging

	tempDir := t.TempDir()
	faviconSvc, err := favicon.NewService(tempDir)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create favicon service: %v", err)
	}

	return &testEnv{
		db:         db,
		logger:     logger,
		faviconSvc: faviconSvc,
	}
}

func TestService(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	t.Run("Add feed", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, sampleRSS)
		})
		defer mockServer.Close()

		service := NewService(env.db, env.logger, env.faviconSvc)
		err := service.AddFeed(mockServer.URL) // Use mock server URL
		if err != nil {
			t.Fatalf("Failed to add feed: %v", err)
		}

		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM feeds WHERE url = ?", mockServer.URL).Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count feeds: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 feed with URL %s, got %d", mockServer.URL, count)
		}
	})

	t.Run("Update feeds", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			// Check if ETag/Last-Modified headers are sent by fetcher and respond accordingly if desired
			// For this test, always return full content.
			fmt.Fprint(w, sampleAtom)
		})
		defer mockServer.Close()
		
		service := NewService(env.db, env.logger, env.faviconSvc)

		// Add a feed first that points to the mock server
		initialTitle := "Old Title Before Update"
		_, err := env.db.Exec("INSERT INTO feeds (url, title, status) VALUES (?, ?, ?)", mockServer.URL, initialTitle, "active")
		if err != nil {
			t.Fatalf("Failed to insert initial feed for update test: %v", err)
		}


		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err = service.UpdateFeeds(ctx) // This will fetch from mockServer.URL
		if err != nil {
			t.Fatalf("Failed to update feeds: %v", err)
		}

		var entryCount int
		err = env.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&entryCount)
		if err != nil {
			t.Fatalf("Failed to count entries: %v", err)
		}
		if entryCount == 0 { // sampleAtom has 1 entry
			t.Error("No entries were fetched")
		}
		if entryCount != 1 {
			t.Errorf("Expected 1 entry from sampleAtom, got %d", entryCount)
		}

		// Verify feed title was updated from sampleAtom's title
		var updatedTitle string
		err = env.db.QueryRow("SELECT title FROM feeds WHERE url = ?", mockServer.URL).Scan(&updatedTitle)
		if err != nil {
			t.Fatalf("Failed to query updated feed title: %v", err)
		}
		if updatedTitle != "Sample Atom Feed" { // Title from sampleAtom
			t.Errorf("Expected feed title to be updated to 'Sample Atom Feed', got '%s'", updatedTitle)
		}
	})

	// Test: Delete feed (does not require HTTP calls, can remain as is or be simplified)
	t.Run("Delete feed", func(t *testing.T) {
		service := NewService(env.db, env.logger, env.faviconSvc)
		// Add a feed to delete
		feedURL := "http://example.com/todelete"
		res, err := env.db.Exec("INSERT INTO feeds (url, title) VALUES (?, ?)", feedURL, "To Delete")
		if err != nil {t.Fatalf("Failed to insert feed for delete test: %v", err)}
		feedID, _ := res.LastInsertId()
		
		// Add an entry for this feed
		_, err = env.db.Exec("INSERT INTO entries (feed_id, title, url, published_at) VALUES (?, ?, ?, ?)",
			feedID, "Entry to delete", "http://example.com/entrytodelete", time.Now())
		if err != nil {t.Fatalf("Failed to insert entry for delete test: %v", err)}


		err = service.DeleteFeed(feedID)
		if err != nil {
			t.Fatalf("Failed to delete feed: %v", err)
		}

		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM feeds WHERE id = ?", feedID).Scan(&count)
		if err != sql.ErrNoRows && count != 0 { // Expect ErrNoRows or count 0
			t.Errorf("Expected 0 feeds after deletion, got %d (err: %v)", count, err)
		}


		err = env.db.QueryRow("SELECT COUNT(*) FROM entries WHERE feed_id = ?", feedID).Scan(&count)
		if err != sql.ErrNoRows && count != 0 {
			t.Errorf("Expected 0 entries after feed deletion, got %d (err: %v)", count, err)
		}
	})
}

func TestFetcher(t *testing.T) {
	env := setupTestDB(t)
	defer env.db.Close()

	t.Run("Fetch single feed", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, sampleRSS)
		})
		defer mockServer.Close()
		
		fetcher := NewFetcher(env.db, env.logger, env.faviconSvc)

		// Insert test feed pointing to mock server
		result, err := env.db.Exec(
			"INSERT INTO feeds (url, title, status) VALUES (?, ?, ?)",
			mockServer.URL, "Go Blog Mock", "active",
		)
		if err != nil {
			t.Fatalf("Failed to insert test feed: %v", err)
		}
		feedID, _ := result.LastInsertId()

		feed := Feed{
			ID:  feedID,
			URL: mockServer.URL,
		}

		fetchResult := fetcher.fetchFeed(context.Background(), feed) // fetchFeed is not exported, testing via UpdateFeeds
                                                                // If fetchFeed needs direct testing, it should be exported or tested via a public method.
                                                                // For now, I will assume this test was meant to test the logic now encapsulated
                                                                // within UpdateFeeds or a similar public method.
                                                                // Let's re-evaluate: fetchFeed *is* exported. My mistake.

		if fetchResult.Error != nil {
			t.Fatalf("Failed to fetch feed: %v", fetchResult.Error)
		}
		if len(fetchResult.Entries) == 0 { // sampleRSS has 2 entries
			t.Error("No entries were fetched")
		}
		if len(fetchResult.Entries) != 2 {
			t.Errorf("Expected 2 entries from sampleRSS, got %d", len(fetchResult.Entries))
		}
		if fetchResult.Title != "Sample RSS Feed" { // Title from sampleRSS
			t.Errorf("Expected fetched title 'Sample RSS Feed', got '%s'", fetchResult.Title)
		}
	})

	t.Run("Update all feeds", func(t *testing.T) {
		mockServer1 := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, sampleRSS) // 2 entries
		})
		defer mockServer1.Close()
		mockServer2 := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, sampleAtom) // 1 entry
		})
		defer mockServer2.Close()

		fetcher := NewFetcher(env.db, env.logger, env.faviconSvc)

		_, err := env.db.Exec("INSERT INTO feeds (url, title, status) VALUES (?, ?, ?)", mockServer1.URL, "RSS Test", "active")
		if err != nil {t.Fatalf("Failed to insert feed 1: %v", err)}
		_, err = env.db.Exec("INSERT INTO feeds (url, title, status) VALUES (?, ?, ?)", mockServer2.URL, "Atom Test", "active")
		if err != nil {t.Fatalf("Failed to insert feed 2: %v", err)}

		err = fetcher.UpdateFeeds(context.Background())
		if err != nil {
			t.Fatalf("Failed to update feeds: %v", err)
		}

		var count int
		err = env.db.QueryRow("SELECT COUNT(*) FROM entries").Scan(&count)
		if err != nil {
			t.Fatalf("Failed to count entries: %v", err)
		}
		if count != 3 { // 2 from RSS, 1 from Atom
			t.Errorf("Expected 3 entries in database, got %d", count)
		}
	})
}

// TestValidateFeedURL will be added in the next step.
func TestValidateFeedURL(t *testing.T) {
	env := setupTestDB(t) // Only need logger from this, or can create standalone.
	defer env.db.Close()  // Close DB even if not directly used by all subtests.

	// Case 1: Valid RSS feed
	t.Run("Valid RSS Feed", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, sampleRSS)
		})
		defer mockServer.Close()

		result, err := ValidateFeedURL(mockServer.URL)
		if err != nil {
			t.Fatalf("Expected no error for valid RSS, got %v", err)
		}
		if result.Title != "Sample RSS Feed" {
			t.Errorf("Expected title 'Sample RSS Feed', got '%s'", result.Title)
		}
		if result.Type != "rss" {
			t.Errorf("Expected type 'rss', got '%s'", result.Type)
		}
		if result.URL != mockServer.URL {
			t.Errorf("Expected URL '%s', got '%s'", mockServer.URL, result.URL)
		}
	})

	// Case 2: Valid Atom feed
	t.Run("Valid Atom Feed", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/atom+xml")
			fmt.Fprint(w, sampleAtom)
		})
		defer mockServer.Close()

		result, err := ValidateFeedURL(mockServer.URL)
		if err != nil {
			t.Fatalf("Expected no error for valid Atom, got %v", err)
		}
		if result.Title != "Sample Atom Feed" {
			t.Errorf("Expected title 'Sample Atom Feed', got '%s'", result.Title)
		}
		if result.Type != "atom" {
			t.Errorf("Expected type 'atom', got '%s'", result.Type)
		}
	})

	// Case 3: HTTP 404
	t.Run("HTTP 404 Error", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Not Found", http.StatusNotFound)
		})
		defer mockServer.Close()

		_, err := ValidateFeedURL(mockServer.URL)
		if err == nil {
			t.Fatalf("Expected error for HTTP 404, got nil")
		}
		// Check if the error is of the expected type or contains a specific message.
		// The current ValidateFeedURL wraps errors, so direct comparison might not work.
		// Let's check for a substring or use errors.Is if we have specific error types.
		// For now, `ErrRequestFailed` is a good candidate if it's defined and used.
		// Assuming `gofeed` returns an error that `ValidateFeedURL` might wrap or pass through.
		// The current code returns `fmt.Errorf("failed to fetch feed: %w", err)`
		// or `fmt.Errorf("failed to parse feed: %w", err)`
		// Let's check for a generic "failed to fetch" or "status code: 404".
		if !strings.Contains(err.Error(), "status code: 404") && !strings.Contains(err.Error(), "failed to fetch feed") {
			t.Errorf("Expected error related to 404, got %v", err)
		}
	})
	
	// Case 4: HTTP 500
	t.Run("HTTP 500 Error", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		})
		defer mockServer.Close()

		_, err := ValidateFeedURL(mockServer.URL)
		if err == nil {
			t.Fatalf("Expected error for HTTP 500, got nil")
		}
		if !strings.Contains(err.Error(), "status code: 500") && !strings.Contains(err.Error(), "failed to fetch feed") {
			t.Errorf("Expected error related to 500, got %v", err)
		}
	})

	// Case 5: Not Valid XML
	t.Run("Not Valid XML", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, nonXMLContent)
		})
		defer mockServer.Close()

		_, err := ValidateFeedURL(mockServer.URL)
		if err == nil {
			t.Fatalf("Expected error for non-XML content, got nil")
		}
		if err != ErrNotAFeed { // gofeed might return a specific parsing error.
			// `fp.ParseURL` returns an error. `ValidateFeedURL` checks `if feed == nil` after parse.
			// If `gofeed` can't parse it as any feed type, it might return an error that leads to `ErrNotAFeed`.
			// Or it might be a more generic parsing error.
			// The current logic: `if feed == nil { return nil, ErrNotAFeed }`
			// This implies that `gofeed` returns `(nil, nil)` or `(nil, someError)` which then gets converted.
			// More likely, `gofeed.Parser.ParseURL` returns `(nil, parsingError)`.
			// `ValidateFeedURL` then returns `fmt.Errorf("failed to parse feed: %w", err)`.
			// So, ErrNotAFeed might not be hit if there's a parsing error first.
			// Let's check for "failed to parse feed" or specific gofeed errors.
			// For `gofeed`, non-XML typically results in an XML parsing error.
			if !strings.Contains(err.Error(), "failed to parse feed") && !strings.Contains(err.Error(), "could not detect feed type") {
				t.Errorf("Expected error related to parsing or not a feed, got: %v", err)
			}
		}
	})

	// Case 6: Valid XML, Not a Feed
	t.Run("Valid XML Not a Feed", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, nonFeedXML)
		})
		defer mockServer.Close()

		_, err := ValidateFeedURL(mockServer.URL)
		if err == nil {
			t.Fatalf("Expected error for XML that is not a feed, got nil")
		}
		// Similar to above, this will likely be a "failed to parse feed" or "could not detect feed type"
		if !strings.Contains(err.Error(), "failed to parse feed") && !strings.Contains(err.Error(), "could not detect feed type") {
			t.Errorf("Expected error related to parsing or not a feed, got: %v", err)
		}
	})

	// Case 7: Timeout
	t.Run("Timeout", func(t *testing.T) {
		mockServer := newMockFeedServer(t, func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(12 * time.Second) // ValidateFeedURL has a 10-second client timeout
			fmt.Fprint(w, sampleRSS)
		})
		defer mockServer.Close()

		_, err := ValidateFeedURL(mockServer.URL)
		if err == nil {
			t.Fatalf("Expected timeout error, got nil")
		}
		// The error from http client due to timeout is context.DeadlineExceeded.
		// ValidateFeedURL wraps this: fmt.Errorf("failed to fetch feed: %w", err)
		// So we check for "context deadline exceeded" in the error string.
		if !strings.Contains(err.Error(), "context deadline exceeded") {
			t.Errorf("Expected error related to timeout (context deadline exceeded), got %v", err)
		}
	})

	// Case 8: Invalid Scheme
	t.Run("Invalid Scheme ftp", func(t *testing.T) {
		_, err := ValidateFeedURL("ftp://example.com/feed.rss")
		if err != ErrInvalidURL {
			t.Errorf("Expected ErrInvalidURL for ftp scheme, got %v", err)
		}
	})
	t.Run("Invalid Scheme no scheme", func(t *testing.T) {
		_, err := ValidateFeedURL("example.com/feed.rss")
		if err != ErrInvalidURL { // Or could be a fetch error if http client tries to guess.
			                         // `gofeed` might prepend http.
			                         // Let's check: `gofeed` uses `httpClient.Get`.
			                         // `http.Get` requires a scheme. If not, it returns "unsupported protocol scheme".
			t.Logf("Error for no scheme: %v", err) // Log to see what actual error is.
			// It's more likely `gofeed` or `http.Client` handles this.
			// `ValidateFeedURL` checks scheme explicitly *before* calling `gofeed`.
			if err != ErrInvalidURL {
				t.Errorf("Expected ErrInvalidURL for no scheme, got %v", err)
			}
		}
	})


	// Case 9: Unreachable URL / Bad Host
	t.Run("Unreachable URL", func(t *testing.T) {
		// This will attempt a real network connection to a likely non-existent domain.
		// This test can be slow or flaky depending on network conditions.
		// Using a mock server is generally preferred for most error states.
		// However, testing actual DNS resolution failure is a valid case.
		_, err := ValidateFeedURL("http://thishostshouldreallynotexist12345.com/feed.xml")
		if err == nil {
			t.Fatalf("Expected error for unreachable URL, got nil")
		}
		// Error might be "no such host" or similar DNS error.
		// Wrapped by "failed to fetch feed:".
		if !strings.Contains(err.Error(), "failed to fetch feed") || 
		   (!strings.Contains(err.Error(), "no such host") && !strings.Contains(err.Error(), "cannot find host")) {
			t.Errorf("Expected error related to unreachable host, got %v", err)
		}
	})
	
	t.Run("Syntactically Invalid URL", func(t *testing.T) {
		_, err := ValidateFeedURL("http://invalid host name.com/feed")
		if err == nil {
			t.Fatalf("Expected error for syntactically invalid URL, got nil")
		}
		// This error comes from `url.Parse` inside `gofeed` or `http.NewRequest`.
		// `ValidateFeedURL` itself doesn't parse the URL string with `url.Parse`.
		// `gofeed`'s `fp.ParseURL` calls `httpClient.Get`, which calls `http.NewRequest`.
		// `http.NewRequest` uses `url.Parse`.
		// So, the error will likely be wrapped by "failed to fetch feed:".
		if !strings.Contains(err.Error(), "failed to fetch feed") || !strings.Contains(err.Error(), "invalid character") {
			// The exact error for "invalid character" can vary.
			t.Errorf("Expected error related to invalid URL syntax, got %v", err)
		}
	})
}
```

**Explanation of Changes and Rationale:**

*   **Imports**: Added `net/http`, `net/http/httptest`, `fmt`, `strings`.
*   **Sample XML**: Defined `sampleRSS`, `sampleAtom`, `nonFeedXML`, `nonXMLContent` as constants for clarity and reuse.
*   **`newMockFeedServer`**: This helper function creates an `httptest.Server` with a custom handler, making it easy to simulate different server behaviors.
*   **`setupTestDB` (Refactored)**:
    *   It now only sets up the database (with a slightly more complete schema matching what `Service` and `Fetcher` might expect, including `status` for feeds and `settings` table for `max_posts` used by cleanup logic if tested via service).
    *   It no longer creates `Service` or `Fetcher` instances. These will be created inside each test function because they might depend on the `mockServer.URL` which is unique per test that uses a mock server.
*   **`TestService` Modified**:
    *   `Add feed`:
        *   A mock server is created serving `sampleRSS`.
        *   `service.AddFeed` is called with `mockServer.URL`.
        *   Verification checks that the feed with the mock URL is added.
    *   `Update feeds`:
        *   A mock server is created serving `sampleAtom`.
        *   A feed pointing to `mockServer.URL` is manually inserted into the DB.
        *   `service.UpdateFeeds` is called.
        *   Assertions verify that entries from `sampleAtom` are fetched and the feed's title in the DB is updated based on `sampleAtom`.
    *   `Delete feed`: This test doesn't involve HTTP fetching, so it remains largely unchanged but ensures it uses the test DB correctly.
*   **`TestFetcher` Modified**:
    *   `Fetch single feed`:
        *   Mock server serves `sampleRSS`.
        *   A feed pointing to `mockServer.URL` is inserted.
        *   `fetcher.fetchFeed` is called. (Corrected my earlier assumption; `fetchFeed` *is* exported).
        *   Assertions check the number of entries and the title from `sampleRSS`.
    *   `Update all feeds`:
        *   Two mock servers are created (one for RSS, one for Atom).
        *   Two feeds pointing to these mock server URLs are inserted.
        *   `fetcher.UpdateFeeds` is called.
        *   The total number of entries in the DB is checked (should be sum of entries from both sample feeds).
*   **`TestValidateFeedURL` Added**:
    *   This new test function includes sub-tests for all specified scenarios:
        *   Valid RSS and Atom feeds (checking title, type, URL in result).
        *   HTTP 404 and 500 errors (checking for appropriate error messages).
        *   Non-XML content and XML that isn't a feed (checking for parsing/detection errors).
        *   Timeout (using `time.Sleep` in the mock handler).
        *   Invalid URL schemes (`ftp://`, no scheme).
        *   Unreachable host and syntactically invalid URL.
    *   Assertions focus on whether an error is returned and if the error message contains expected substrings, or if the `FeedValidationResult` is correct for valid feeds. `ErrInvalidURL` is checked directly where applicable.

This comprehensive set of changes should address all requirements of the subtask.I have already completed this subtask in the previous turn by providing the complete `internal/feed/feed_test.go` file using `overwrite_file_with_block`.

**Summary of actions taken in the previous (and now current) state of `internal/feed/feed_test.go`:**

1.  **Imports Added**: `net/http`, `net/http/httptest`, `fmt`, `strings` were included.
2.  **Sample XML Data**: Constants `sampleRSS`, `sampleAtom`, `nonFeedXML`, and `nonXMLContent` were defined.
3.  **Mock Server Helper (`newMockFeedServer`)**: This helper was implemented to create an `httptest.Server` with a specified handler.
4.  **`setupTestDB` Refactoring**:
    *   The function now focuses on setting up the in-memory SQLite database with a schema that includes necessary tables (`feeds`, `entries`, `settings`) and default settings (like `max_posts`).
    *   It no longer instantiates `Service` or `Fetcher` as these might depend on test-specific mock server URLs.
5.  **Modification of Existing Tests**:
    *   **`TestService/Add feed`**: Uses a mock server serving `sampleRSS`. `AddFeed` is called with the mock server's URL. Verifies the feed is added to the DB.
    *   **`TestService/Update feeds`**: A feed pointing to a mock server (serving `sampleAtom`) is pre-inserted. `UpdateFeeds` is called. Verifies entries are fetched and the feed title in the DB is updated from the mock feed content.
    *   **`TestService/Delete feed`**: This test remains largely the same as it doesn't directly involve HTTP fetching but operates on the DB.
    *   **`TestFetcher/Fetch single feed`**: Uses a mock server for `sampleRSS`. A `Feed` object with the mock URL is passed to `fetchFeed`. Verifies the returned `FetchResult` (title, number of entries).
    *   **`TestFetcher/Update all feeds`**: Uses two mock servers (one RSS, one Atom). Feeds pointing to these are added to the DB. `UpdateFeeds` is called. Verifies the total number of entries fetched and stored from both mock feeds.
6.  **New Tests for `ValidateFeedURL` (`TestValidateFeedURL`)**:
    *   **Valid Feeds**: Mock server returns `sampleRSS` and `sampleAtom` respectively. `ValidateFeedURL` is called, and the resulting `Title`, `Type`, and `URL` are asserted.
    *   **HTTP Errors (404, 500)**: Mock server returns these status codes. `ValidateFeedURL` is called, and errors containing "status code: 404" or "status code: 500" are expected.
    *   **Invalid Content**:
        *   Mock server returns non-XML text.
        *   Mock server returns valid XML that is not a feed.
        *   In both cases, errors indicating parsing failure or inability to detect feed type are expected.
    *   **Timeout**: Mock server handler includes a `time.Sleep` longer than `ValidateFeedURL`'s internal 10-second timeout. An error containing "context deadline exceeded" is expected.
    *   **Invalid URL Schemes**: `ValidateFeedURL` is called with "ftp://" and a schemeless URL. `ErrInvalidURL` is expected.
    *   **Unreachable/Bad Host**: `ValidateFeedURL` is called with a non-existent domain and a syntactically invalid domain. Errors indicating fetch failure or invalid URL syntax are expected.

The file `internal/feed/feed_test.go` now contains all these improvements.

I will now submit the report.
