package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleFeedAPIDeleteRemovesAssociatedData(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close(t)

	res, err := ts.db.Exec("INSERT INTO feeds (url, title) VALUES (?, ?)", "http://example.com/delete", "Delete Feed")
	if err != nil {
		t.Fatalf("Failed to insert feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	_, err = ts.db.Exec("INSERT INTO entries (feed_id, title, url, published_at) VALUES (?, ?, ?, ?)",
		feedID, "Entry", "http://example.com/entry", time.Now())
	if err != nil {
		t.Fatalf("Failed to insert entry: %v", err)
	}

	_, err = ts.db.Exec("INSERT INTO tags (name) VALUES (?)", "DeleteTag")
	if err != nil {
		t.Fatalf("Failed to insert tag: %v", err)
	}
	var tagID int64
	if err := ts.db.QueryRow("SELECT id FROM tags WHERE name = ?", "DeleteTag").Scan(&tagID); err != nil {
		t.Fatalf("Failed to query tag id: %v", err)
	}
	if _, err := ts.db.Exec("INSERT INTO feed_tags (feed_id, tag_id) VALUES (?, ?)", feedID, tagID); err != nil {
		t.Fatalf("Failed to insert feed tag association: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/api/feeds/%d", feedID), nil)
	token := ts.server.csrf.Token(httptest.NewRecorder(), req)
	req.AddCookie(&http.Cookie{Name: ts.server.csrf.config.Cookie, Value: token})
	req.Header.Set(ts.server.csrf.config.Header, token)

	rr := httptest.NewRecorder()
	ts.server.handleFeedAPI(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("Expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	var count int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM feeds WHERE id = ?", feedID).Scan(&count); err == nil && count != 0 {
		t.Errorf("Expected feed to be deleted, found %d rows", count)
	}
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM entries WHERE feed_id = ?", feedID).Scan(&count); err == nil && count != 0 {
		t.Errorf("Expected entries to be deleted, found %d rows", count)
	}
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM feed_tags WHERE feed_id = ?", feedID).Scan(&count); err == nil && count != 0 {
		t.Errorf("Expected feed tags to be deleted, found %d rows", count)
	}
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM tags WHERE name = ?", "DeleteTag").Scan(&count); err == nil && count != 0 {
		t.Errorf("Expected orphaned tag to be deleted, found %d rows", count)
	}
}
