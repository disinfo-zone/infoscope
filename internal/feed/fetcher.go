// Save as: internal/feed/fetcher.go
package feed

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"infoscope/internal/favicon"

	"github.com/mmcdole/gofeed"
)

type Fetcher struct {
	db         *sql.DB
	logger     *log.Logger
	parser     *gofeed.Parser
	client     *http.Client
	faviconSvc *favicon.Service
	cache      *sync.Map // Add in-memory cache
}

func NewFetcher(db *sql.DB, logger *log.Logger, faviconSvc *favicon.Service) *Fetcher {
	return &Fetcher{
		db:         db,
		logger:     logger,
		parser:     gofeed.NewParser(),
		client:     &http.Client{Timeout: 30 * time.Second}, // Increased timeout
		faviconSvc: faviconSvc,
		cache:      &sync.Map{},
	}
}

type cacheEntry struct {
	lastModified string
	etag         string
	timestamp    time.Time
}

func (f *Fetcher) UpdateFeeds(ctx context.Context) error {
	f.logger.Printf("Starting feed update...")

	// Get all feeds from database
	rows, err := f.db.QueryContext(ctx, "SELECT id, url, title FROM feeds")
	if err != nil {
		return fmt.Errorf("error querying feeds: %w", err)
	}
	defer rows.Close()

	var feeds []Feed
	for rows.Next() {
		var feed Feed
		if err := rows.Scan(&feed.ID, &feed.URL, &feed.Title); err != nil {
			f.logger.Printf("Error scanning feed: %v", err)
			continue
		}
		feeds = append(feeds, feed)
	}

	f.logger.Printf("Found %d feeds to update", len(feeds))

	// Create a channel for results
	results := make(chan FetchResult, len(feeds))
	var wg sync.WaitGroup

	// Fetch feeds concurrently
	for _, feed := range feeds {
		wg.Add(1)
		go func(feed Feed) {
			defer wg.Done()
			f.logger.Printf("Fetching feed: %s", feed.URL)
			result := f.fetchFeed(ctx, feed)
			if result.Error != nil {
				f.logger.Printf("Error fetching feed %s: %v", feed.URL, result.Error)
			} else {
				f.logger.Printf("Successfully fetched %d entries from %s", len(result.Entries), feed.URL)
			}
			results <- result
		}(feed)
	}

	// Wait for all fetches to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	for result := range results {
		if result.Error != nil {
			f.logger.Printf("Error fetching feed %s: %v", result.Feed.URL, result.Error)
			continue
		}

		if err := f.saveFeedEntries(ctx, result); err != nil {
			f.logger.Printf("Error saving entries for feed %s: %v", result.Feed.URL, err)
		}
	}

	f.logger.Printf("Feed update completed")
	return nil
}

func (f *Fetcher) fetchFeed(ctx context.Context, feed Feed) FetchResult {
	result := FetchResult{Feed: feed}

	// Check cache
	cacheKey := fmt.Sprintf("feed_%d", feed.ID)
	cached, exists := f.cache.Load(cacheKey)

	req, err := http.NewRequestWithContext(ctx, "GET", feed.URL, nil)
	if err != nil {
		result.Error = fmt.Errorf("error creating request: %w", err)
		return result
	}

	// Add conditional GET headers if we have cached data
	if exists {
		entry := cached.(cacheEntry)
		if entry.lastModified != "" {
			req.Header.Set("If-Modified-Since", entry.lastModified)
		}
		if entry.etag != "" {
			req.Header.Set("If-None-Match", entry.etag)
		}
	}

	resp, err := f.client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("error fetching feed: %w", err)
		return result
	}
	defer resp.Body.Close()

	// Handle 304 Not Modified
	if resp.StatusCode == http.StatusNotModified {
		f.logger.Printf("Feed %s not modified since last fetch", feed.URL)
		return result
	}

	// Update cache with new headers
	f.cache.Store(cacheKey, cacheEntry{
		lastModified: resp.Header.Get("Last-Modified"),
		etag:         resp.Header.Get("ETag"),
		timestamp:    time.Now(),
	})

	// Parse feed
	parsedFeed, err := f.parser.Parse(resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("error parsing feed: %w", err)
		return result
	}

	// Get latest entry timestamp from database
	var latestTimestampStr sql.NullString
	err = f.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(published_at), '') FROM entries WHERE feed_id = ?`,
		feed.ID,
	).Scan(&latestTimestampStr)

	var latestTimestamp time.Time
	if err != nil && err != sql.ErrNoRows {
		f.logger.Printf("Warning: error getting latest timestamp for feed %s: %v", feed.URL, err)
	} else if latestTimestampStr.Valid && latestTimestampStr.String != "" {
		latestTimestamp, err = time.Parse("2006-01-02 15:04:05", latestTimestampStr.String)
		if err != nil {
			f.logger.Printf("Warning: error parsing timestamp %s for feed %s: %v",
				latestTimestampStr.String, feed.URL, err)
		}
	}

	// Process entries
	var newEntries []Entry
	for _, item := range parsedFeed.Items {
		pubDate := item.PublishedParsed
		if pubDate == nil {
			pubDate = item.UpdatedParsed
		}
		if pubDate == nil {
			now := time.Now()
			pubDate = &now
		}

		// Skip entries older than latest timestamp if we have one
		if !latestTimestamp.IsZero() && pubDate.Before(latestTimestamp) {
			continue
		}

		// Get or create favicon
		faviconFile, err := f.faviconSvc.GetFavicon(parsedFeed.Link)
		if err != nil {
			f.logger.Printf("Error getting favicon for %s: %v", parsedFeed.Link, err)
			faviconFile = "default.ico"
		}

		entry := Entry{
			FeedID:      feed.ID,
			Title:       item.Title,
			URL:         item.Link,
			Content:     item.Description,
			GUID:        item.GUID,
			PublishedAt: *pubDate,
			FaviconURL:  "/static/favicons/" + faviconFile,
		}
		newEntries = append(newEntries, entry)
	}

	result.Entries = newEntries
	return result
}

func (f *Fetcher) saveFeedEntries(ctx context.Context, result FetchResult) error {
	if len(result.Entries) == 0 {
		// Update last_fetched time even if no new entries
		_, err := f.db.ExecContext(ctx,
			"UPDATE feeds SET last_fetched = DATETIME(?) WHERE id = ?",
			time.Now().UTC().Format("2006-01-02 15:04:05"), result.Feed.ID,
		)
		return err
	}

	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update feed last_fetched time
	_, err = tx.ExecContext(ctx,
		"UPDATE feeds SET last_fetched = DATETIME(?) WHERE id = ?",
		time.Now().UTC().Format("2006-01-02 15:04:05"), result.Feed.ID,
	)
	if err != nil {
		return err
	}

	// Prepare statement for inserting entries
	stmt, err := tx.PrepareContext(ctx, `
    INSERT INTO entries (
        feed_id, title, url, content, guid, 
        published_at, favicon_url
    )
    VALUES (?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(url) DO UPDATE SET
        title = excluded.title,
        content = excluded.content,
        published_at = excluded.published_at
        WHERE excluded.published_at > published_at
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Insert entries
	for _, entry := range result.Entries {
		_, err = stmt.ExecContext(ctx,
			entry.FeedID,
			entry.Title,
			entry.URL,
			entry.Content,
			entry.GUID,
			entry.PublishedAt.UTC().Format("2006-01-02 15:04:05"),
			entry.FaviconURL,
		)
		if err != nil {
			f.logger.Printf("Error inserting entry %s: %v", entry.URL, err)
			continue
		}
	}

	// Clean old entries
	var maxPosts int
	err = f.db.QueryRowContext(ctx,
		"SELECT COALESCE(CAST(value AS INTEGER), 33) FROM settings WHERE key = 'max_posts'",
	).Scan(&maxPosts)
	if err != nil {
		maxPosts = 33 // Default value
	}

	// Delete old entries more efficiently
	_, err = tx.ExecContext(ctx, `
        DELETE FROM entries 
        WHERE id IN (
            SELECT id FROM entries 
            WHERE feed_id = ? 
            ORDER BY published_at DESC
            LIMIT -1 OFFSET ?
        )
    `, result.Feed.ID, maxPosts)
	if err != nil {
		return err
	}

	return tx.Commit()
}
