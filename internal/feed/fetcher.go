// Save as: internal/feed/fetcher.go
package feed

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"infoscope/internal/database"
	"infoscope/internal/favicon"
	securitynet "infoscope/internal/security/netutil"

	"github.com/mmcdole/gofeed"
)

type Fetcher struct {
	db           *sql.DB
	logger       *log.Logger
	parser       *gofeed.Parser
	client       *http.Client
	faviconSvc   *favicon.Service
	cache        *sync.Map // Add in-memory cache
	filterEngine *FilterEngine
}

func NewFetcher(db *sql.DB, logger *log.Logger, faviconSvc *favicon.Service) *Fetcher {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Fetcher{
		db:     db,
		logger: logger,
		parser: gofeed.NewParser(),
		client: &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			return nil
		}},
		faviconSvc:   faviconSvc,
		cache:        &sync.Map{},
		filterEngine: NewFilterEngine(db),
	}
}

// formattedTimestamp returns the current time formatted for database storage
func (f *Fetcher) formattedTimestamp() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

// formatTimestamp formats a given time for database storage
func (f *Fetcher) formatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
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

	// Concurrency limiter
	concurrency := f.getConcurrencyLimit(ctx)
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)

	// Fetch feeds concurrently with limiter
	for _, feed := range feeds {
		wg.Add(1)
		sem <- struct{}{}
		go func(feed Feed) {
			defer wg.Done()
			defer func() { <-sem }()
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

	// SSRF hardening: pre-validate destination
	req, err := http.NewRequestWithContext(ctx, "GET", feed.URL, nil)
	if err != nil {
		result.Error = fmt.Errorf("error creating request: %w", err)
		return result
	}

	// Identify our client
	req.Header.Set("User-Agent", "Infoscope/0.3")

	// Add conditional GET headers if we have cached data
	var condLastMod, condETag string
	if exists {
		entry := cached.(cacheEntry)
		condLastMod = entry.lastModified
		condETag = entry.etag
	} else {
		// Fallback to persisted headers from DB
		var dbLastMod, dbETag sql.NullString
		if err := f.db.QueryRowContext(ctx, "SELECT last_modified, etag FROM feeds WHERE id = ?", feed.ID).Scan(&dbLastMod, &dbETag); err == nil {
			if dbLastMod.Valid {
				condLastMod = strings.TrimSpace(dbLastMod.String)
			}
			if dbETag.Valid {
				condETag = strings.TrimSpace(dbETag.String)
			}
		}
	}
	if condLastMod != "" {
		req.Header.Set("If-Modified-Since", condLastMod)
	}
	if condETag != "" {
		req.Header.Set("If-None-Match", condETag)
	}

	// Resolve host and block private/reserved ranges (allow loopback for tests)
	if host := req.URL.Hostname(); host != "" {
		if ip := net.ParseIP(host); ip != nil {
			if securitynet.IsPrivateIP(ip) && !ip.IsLoopback() {
				result.Error = fmt.Errorf("destination resolves to private/reserved address")
				return result
			}
		} else {
			if addrs, err := net.LookupIP(host); err == nil {
				for _, a := range addrs {
					if securitynet.IsPrivateIP(a) && !a.IsLoopback() {
						result.Error = fmt.Errorf("destination resolves to private/reserved address")
						return result
					}
				}
			}
		}
	}

	resp, err := f.client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("error fetching feed: %w", err)
		return result
	}
	defer resp.Body.Close()

	// Handle non-success statuses other than 304
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotModified) {
		result.Error = fmt.Errorf("unexpected response status %d", resp.StatusCode)
		return result
	}

	// Handle 304 Not Modified
	if resp.StatusCode == http.StatusNotModified {
		f.logger.Printf("Feed %s not modified since last fetch", feed.URL)
		// Propagate any new validator headers if sent; else use conditional ones
		lm := resp.Header.Get("Last-Modified")
		if lm == "" {
			lm = condLastMod
		}
		et := resp.Header.Get("ETag")
		if et == "" {
			et = condETag
		}
		result.LastModified = lm
		result.ETag = et
		// Update cache timestamp
		f.cache.Store(cacheKey, cacheEntry{lastModified: lm, etag: et, timestamp: time.Now()})
		return result
	}

	// Update cache with new headers
	f.cache.Store(cacheKey, cacheEntry{
		lastModified: resp.Header.Get("Last-Modified"),
		etag:         resp.Header.Get("ETag"),
		timestamp:    time.Now(),
	})

	result.LastModified = resp.Header.Get("Last-Modified")
	result.ETag = resp.Header.Get("ETag")

	// Parse feed with a reasonable size limit (5MB) to avoid huge downloads
	const maxFeedBytes = 5 << 20
	limited := io.LimitReader(resp.Body, maxFeedBytes)
	parsedFeed, err := f.parser.Parse(limited)
	if err != nil {
		result.Error = fmt.Errorf("error parsing feed: %w", err)
		return result
	}
	if parsedFeed == nil {
		result.Error = fmt.Errorf("error parsing feed: empty document")
		return result
	}

	// Store the parsed feed title in the result for later use
	result.FeedTitle = parsedFeed.Title

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

	// Fetch favicon only once per feed
	faviconFile := "default.ico"
	if parsedFeed.Link != "" {
		if ff, ferr := f.faviconSvc.GetFavicon(parsedFeed.Link); ferr == nil && ff != "" {
			faviconFile = ff
		} else if ferr != nil {
			f.logger.Printf("Error getting favicon for %s: %v", parsedFeed.Link, ferr)
		}
	}

	// Process entries
	var newEntries = make([]Entry, 0, len(parsedFeed.Items))
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
	// Apply filters to entries before saving
	filteredEntries, filteredCount := f.applyFilters(ctx, result.Entries, result.Feed.Category, result.Feed.Tags)

	// Log filtering statistics
	if filteredCount > 0 {
		f.logger.Printf("Filtered %d out of %d entries from feed %s",
			filteredCount, len(result.Entries), result.Feed.URL)
	}

	// Update result with filtered entries
	result.Entries = filteredEntries

	if len(result.Entries) == 0 {
		// Update last_fetched time and validator headers even if no entries
		if result.FeedTitle != "" {
			_, err := f.db.ExecContext(ctx,
				"UPDATE feeds SET last_fetched = DATETIME(?), last_modified = COALESCE(NULLIF(?, ''), last_modified), etag = COALESCE(NULLIF(?, ''), etag), title = CASE WHEN title_manually_edited = 1 THEN title ELSE ? END WHERE id = ?",
				f.formattedTimestamp(), result.LastModified, result.ETag, result.FeedTitle, result.Feed.ID,
			)
			return err
		} else {
			_, err := f.db.ExecContext(ctx,
				"UPDATE feeds SET last_fetched = DATETIME(?), last_modified = COALESCE(NULLIF(?, ''), last_modified), etag = COALESCE(NULLIF(?, ''), etag) WHERE id = ?",
				f.formattedTimestamp(), result.LastModified, result.ETag, result.Feed.ID,
			)
			return err
		}
	}

	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update feed last_fetched time, validators, and title (if not manually edited)
	if result.FeedTitle != "" {
		_, err = tx.ExecContext(ctx,
			"UPDATE feeds SET last_fetched = DATETIME(?), last_modified = COALESCE(NULLIF(?, ''), last_modified), etag = COALESCE(NULLIF(?, ''), etag), title = CASE WHEN title_manually_edited = 1 THEN title ELSE ? END WHERE id = ?",
			f.formattedTimestamp(), result.LastModified, result.ETag, result.FeedTitle, result.Feed.ID,
		)
	} else {
		_, err = tx.ExecContext(ctx,
			"UPDATE feeds SET last_fetched = DATETIME(?), last_modified = COALESCE(NULLIF(?, ''), last_modified), etag = COALESCE(NULLIF(?, ''), etag) WHERE id = ?",
			f.formattedTimestamp(), result.LastModified, result.ETag, result.Feed.ID,
		)
	}
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
			f.formatTimestamp(entry.PublishedAt),
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

// getConcurrencyLimit determines the number of concurrent feed fetches.
// It consults the settings table (key: 'feed_concurrency') if present, otherwise
// falls back to a value based on the number of CPUs.
func (f *Fetcher) getConcurrencyLimit(ctx context.Context) int {
	// Reasonable default tuned to IO-bound workload
	defaultLimit := runtime.NumCPU() * 4
	if defaultLimit < 4 {
		defaultLimit = 4
	}
	if defaultLimit > 32 {
		defaultLimit = 32
	}

	var s sql.NullString
	if err := f.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'feed_concurrency'").Scan(&s); err != nil {
		return defaultLimit
	}
	if !s.Valid {
		return defaultLimit
	}
	n, err := strconv.Atoi(strings.TrimSpace(s.String))
	if err != nil {
		return defaultLimit
	}
	if n < 1 {
		return 1
	}
	if n > 128 {
		return 128
	}
	return n
}

// applyFilters applies the filtering system to a list of entries
func (f *Fetcher) applyFilters(ctx context.Context, entries []Entry, feedCategory string, feedTags []string) ([]Entry, int) {
	if len(entries) == 0 {
		return entries, 0
	}

	var filteredEntries []Entry
	var filteredCount int

	for _, entry := range entries {
		// Convert feed Entry to database Entry for filtering
		dbEntry := &database.Entry{
			ID:          entry.ID,
			FeedID:      entry.FeedID,
			Title:       entry.Title,
			URL:         entry.URL,
			Content:     entry.Content,
			PublishedAt: entry.PublishedAt,
			FaviconURL:  entry.FaviconURL,
		}

		decision, err := f.filterEngine.FilterEntry(ctx, dbEntry, feedCategory, feedTags)
		if err != nil {
			// Log error but don't fail the entire process
			f.logger.Printf("Error applying filters to entry '%s': %v", entry.Title, err)
			// Default to keeping the entry if filtering fails
			filteredEntries = append(filteredEntries, entry)
			continue
		}

		switch decision {
		case FilterKeep:
			filteredEntries = append(filteredEntries, entry)
		case FilterDiscard:
			filteredCount++
			f.logger.Printf("Filtered out entry: %s", entry.Title)
		}
	}

	return filteredEntries, filteredCount
}
