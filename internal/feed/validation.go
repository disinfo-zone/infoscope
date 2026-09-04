// internal/feed/validation.go
package feed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	securitynet "infoscope/internal/security/netutil"

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
	// Sample of the most recent item for preview in UI
	SampleItemTitle     string `json:"sampleItemTitle,omitempty"`
	SampleItemURL       string `json:"sampleItemURL,omitempty"`
	SampleItemPublished string `json:"sampleItemPublished,omitempty"`
	SampleItemContent   string `json:"sampleItemContent,omitempty"`
}

func ValidateFeedURL(feedURL string) (*FeedValidationResult, error) {
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if err := securitynet.ValidateHTTPURL(u); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transport := &http.Transport{
		DialContext:           securitynet.PublicOnlyDialContext(&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     transport,
		CheckRedirect: securitynet.CheckRedirect(5),
	}
	return validateFeedURLWithClient(ctx, feedURL, client)
}

const maxValidationFeedBytes = 5 << 20

// validateFeedURLWithClient is a test seam; production callers use the
// public-only client constructed by ValidateFeedURL.
func validateFeedURLWithClient(ctx context.Context, feedURL string, client *http.Client) (*FeedValidationResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	req.Header.Set("User-Agent", "Infoscope/0.5")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("could not reach URL: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrNotAFeed
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxValidationFeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("could not read URL: %v", err)
	}
	if len(body) > maxValidationFeedBytes {
		return nil, fmt.Errorf("%w: feed exceeds %d bytes", ErrNotAFeed, maxValidationFeedBytes)
	}
	feed, err := gofeed.NewParser().Parse(bytes.NewReader(body))
	if err != nil || feed == nil {
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

	// Include a sample of the newest item for UI preview
	if len(feed.Items) > 0 {
		item := feed.Items[0]
		result.SampleItemTitle = item.Title
		rawLink := item.Link
		if rawLink == "" {
			rawLink = item.GUID
		}
		if sanitized, err := sanitizeEntryURL(rawLink, feed.Link, feedURL); err == nil {
			result.SampleItemURL = sanitized
		}
		if item.PublishedParsed != nil {
			result.SampleItemPublished = item.PublishedParsed.Format(time.RFC1123Z)
		}
		// Prefer Content, fall back to Description
		sampleBody := item.Content
		if sampleBody == "" {
			sampleBody = item.Description
		}
		// Trim excessive whitespace and shorten overly long previews
		if sampleBody != "" {
			runes := []rune(sampleBody)
			if len(runes) > 2000 {
				sampleBody = string(runes[:2000])
			}
			result.SampleItemContent = sampleBody
		}
	}

	return result, nil
}
