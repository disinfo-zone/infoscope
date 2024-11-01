// internal/feed/validation.go
package feed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/mmcdole/gofeed"
)

var (
	ErrInvalidURL = errors.New("invalid feed URL")
	ErrTimeout    = errors.New("feed fetch timeout")
	ErrNotAFeed   = errors.New("URL does not point to a valid feed")
)

type FeedValidationResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	ItemCount   int    `json:"itemCount"`
	LastUpdated string `json:"lastUpdated,omitempty"`
	FeedType    string `json:"feedType,omitempty"` // RSS, Atom, etc.
}

func ValidateFeedURL(feedURL string) (*FeedValidationResult, error) {
	// Parse URL
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	// Check scheme
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: must use HTTP or HTTPS", ErrInvalidURL)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create feed parser
	fp := gofeed.NewParser()

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Try to fetch and parse feed
	feed, err := fp.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		// Try to determine if it's a timeout or invalid feed
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}

		// Check if the URL is reachable
		resp, err := client.Get(feedURL)
		if err != nil {
			return nil, fmt.Errorf("could not reach URL: %v", err)
		}
		defer resp.Body.Close()

		return nil, ErrNotAFeed
	}

	// Create validation result
	result := &FeedValidationResult{
		Title:       feed.Title,
		Description: feed.Description,
		ItemCount:   len(feed.Items),
		FeedType:    feed.FeedType,
	}

	// Set last updated if available
	if feed.UpdatedParsed != nil {
		result.LastUpdated = feed.UpdatedParsed.Format("January 2, 2006")
	} else if len(feed.Items) > 0 && feed.Items[0].PublishedParsed != nil {
		result.LastUpdated = feed.Items[0].PublishedParsed.Format("January 2, 2006")
	}

	return result, nil
}
