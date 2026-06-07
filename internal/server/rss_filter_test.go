package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seedFeedWithTagEntry inserts an active feed with a category, a single tag and
// one entry, for taxonomy-filtering tests.
func seedFeedWithTagEntry(t *testing.T, ts *testServer, feedURL, title, category, tag, entryTitle, entryURL string) {
	t.Helper()
	res, err := ts.db.Exec(`INSERT INTO feeds (url, title, status, category) VALUES (?, ?, 'active', ?)`, feedURL, title, category)
	if err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	if _, err := ts.db.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag); err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	var tagID int64
	if err := ts.db.QueryRow(`SELECT id FROM tags WHERE name = ? COLLATE NOCASE`, tag).Scan(&tagID); err != nil {
		t.Fatalf("get tag: %v", err)
	}
	if _, err := ts.db.Exec(`INSERT OR IGNORE INTO feed_tags (feed_id, tag_id) VALUES (?, ?)`, feedID, tagID); err != nil {
		t.Fatalf("link tag: %v", err)
	}
	// Store the timestamp in the same canonical format the fetcher uses so
	// SQLite's datetime() can parse it.
	publishedAt := time.Now().UTC().Format("2006-01-02 15:04:05")
	if _, err := ts.db.Exec(`INSERT INTO entries (feed_id, title, url, published_at, favicon_url) VALUES (?, ?, ?, ?, ?)`,
		feedID, entryTitle, entryURL, publishedAt, ""); err != nil {
		t.Fatalf("insert entry: %v", err)
	}
}

func TestRSSRespectsCategoryAndTagFilters(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	if _, err := ts.db.Exec(`INSERT INTO settings (key, value, type) VALUES ('site_url', 'https://demo.example', 'string')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		t.Fatalf("set site_url: %v", err)
	}

	seedFeedWithTagEntry(t, ts, "https://a.example/feed", "Alpha", "Tech", "english", "Alpha Tech Story", "https://a.example/1")
	seedFeedWithTagEntry(t, ts, "https://b.example/feed", "Beta", "Photo", "french", "Beta Photo Story", "https://b.example/1")

	handler := ts.server.Routes()
	get := func(target string) string {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: expected 200, got %d", target, rr.Code)
		}
		return rr.Body.String()
	}

	// Unfiltered: both entries present.
	all := get("/rss.xml")
	if !strings.Contains(all, "Alpha Tech Story") || !strings.Contains(all, "Beta Photo Story") {
		t.Fatalf("unfiltered RSS should contain both entries")
	}

	// Category filter restricts to matching feeds and is reflected in title + self link.
	tech := get("/rss.xml?category=Tech")
	if !strings.Contains(tech, "Alpha Tech Story") {
		t.Errorf("category=Tech should include Alpha")
	}
	if strings.Contains(tech, "Beta Photo Story") {
		t.Errorf("category=Tech should exclude Beta")
	}
	if !strings.Contains(tech, "category: Tech") {
		t.Errorf("filtered feed title should mention 'category: Tech'")
	}
	if !strings.Contains(tech, "/rss.xml?category=Tech") {
		t.Errorf("self link should include the category query")
	}

	// Tag filter is case-insensitive (tags are stored COLLATE NOCASE).
	fr := get("/rss.xml?tag=FRENCH")
	if !strings.Contains(fr, "Beta Photo Story") {
		t.Errorf("tag=FRENCH should include Beta (case-insensitive)")
	}
	if strings.Contains(fr, "Alpha Tech Story") {
		t.Errorf("tag=FRENCH should exclude Alpha")
	}
}
