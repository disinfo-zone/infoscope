// Save as: internal/feed/types.go
package feed

import (
	"time"
)

type Feed struct {
	ID          int64     `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	LastFetched time.Time `json:"lastFetched"`
}

type Entry struct {
	ID          int64     `json:"id"`
	FeedID      int64     `json:"feedId"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
	FaviconURL  string    `json:"faviconURL"`
}

type FetchResult struct {
	Feed    Feed
	Entries []Entry
	Error   error
}
