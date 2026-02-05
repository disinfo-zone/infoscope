package feed

import (
	"fmt"
	"net/url"
	"strings"
)

func sanitizeEntryURL(rawURL, feedLink, feedURL string) (string, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return "", fmt.Errorf("empty url")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}

	if parsed.Scheme != "" && parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}

	if parsed.Scheme == "" {
		base := pickEntryBaseURL(feedLink, feedURL)
		if base == nil {
			return "", fmt.Errorf("missing base url")
		}
		parsed = base.ResolveReference(parsed)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("missing host")
	}

	return parsed.String(), nil
}

func pickEntryBaseURL(feedLink, feedURL string) *url.URL {
	for _, candidate := range []string{feedLink, feedURL} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		base, err := url.Parse(candidate)
		if err != nil {
			continue
		}
		if (base.Scheme == "http" || base.Scheme == "https") && base.Host != "" {
			return base
		}
	}
	return nil
}
