// internal/feed/validation.go
package feed

import (
	"context"
	"errors"
	"fmt"
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
	// Parse URL
	u, err := url.Parse(feedURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	// Check scheme
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: must use HTTP or HTTPS", ErrInvalidURL)
	}

	// SSRF hardening: block private/reserved ranges (but allow loopback for local testing)
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if securitynet.IsPrivateIP(ip) && !ip.IsLoopback() {
			return nil, fmt.Errorf("%w: URL resolves to private/reserved address", ErrInvalidURL)
		}
	} else {
		addrs, err := net.LookupIP(host)
		if err == nil {
			for _, a := range addrs {
				if securitynet.IsPrivateIP(a) && !a.IsLoopback() {
					return nil, fmt.Errorf("%w: URL resolves to private/reserved address", ErrInvalidURL)
				}
			}
		}
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create feed parser
	fp := gofeed.NewParser()
	// Use a custom HTTP client with sane timeouts and HTTP/2
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   5,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create HTTP client with timeout
	client := &http.Client{Timeout: 10 * time.Second, Transport: transport}

	// Try to fetch and parse feed
	// gofeed uses http.DefaultClient internally unless we override Fetcher.
	// Since we already perform network checks and will explicitly fetch via client,
	// keep using ParseURLWithContext but rely on external reachability checks on error.
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

	// Include a sample of the newest item for UI preview
	if len(feed.Items) > 0 {
		item := feed.Items[0]
		result.SampleItemTitle = item.Title
		result.SampleItemURL = item.Link
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
			// Basic whitespace normalization
			if len(sampleBody) > 2000 {
				sampleBody = sampleBody[:2000]
			}
			result.SampleItemContent = sampleBody
		}
	}

	return result, nil
}
