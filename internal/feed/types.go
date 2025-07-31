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
	Category    string    `json:"category,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
}

type Entry struct {
	ID          int64     `json:"id"`
	FeedID      int64     `json:"feedId"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Content     string    `json:"content,omitempty"`
	GUID        string    `json:"guid,omitempty"`
	PublishedAt time.Time `json:"publishedAt"`
	FaviconURL  string    `json:"faviconUrl,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type FetchResult struct {
	Feed      Feed
	Entries   []Entry
	FeedTitle string
	Error     error
}
