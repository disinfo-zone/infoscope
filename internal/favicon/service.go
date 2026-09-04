// Save as: internal/favicon/service.go
package favicon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	securitynet "infoscope/internal/security/netutil"

	"golang.org/x/net/html"
)

type Service struct {
	client      *http.Client
	storageDir  string
	failedHosts sync.Map // Add this line to track failed hosts
}

func NewService(storageDir string) (*Service, error) {
	// Ensure storage directory exists
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create favicon storage directory: %w", err)
	}

	transport := &http.Transport{
		DialContext:           securitynet.PublicOnlyDialContext(&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &Service{
		client: &http.Client{
			Timeout:       10 * time.Second,
			Transport:     transport,
			CheckRedirect: securitynet.CheckRedirect(5),
		},
		storageDir:  storageDir,
		failedHosts: sync.Map{}, // Initialize the map
	}, nil
}

func (s *Service) GetFavicon(siteURL string) (string, error) {
	u, err := url.Parse(siteURL)
	if err != nil || securitynet.ValidateHTTPURL(u) != nil {
		return "default.ico", fmt.Errorf("invalid site URL")
	}

	// Check if this host has failed before
	if _, failed := s.failedHosts.Load(u.Host); failed {
		return "default.ico", nil
	}

	// Generate a consistent filename based on the domain
	hash := sha256.Sum256([]byte(u.Host))
	filename := hex.EncodeToString(hash[:8]) + ".ico"
	filepath := filepath.Join(s.storageDir, filename)

	// Check if we already have this favicon
	if _, err := os.Stat(filepath); err == nil {
		// Favicon exists
		return filename, nil
	}

	// Try different methods to get the favicon
	var faviconData []byte
	methods := []func(string) ([]byte, error){
		s.getFaviconFromHTML,
		s.getFaviconFromRoot,
	}

	var lastError error
	for _, method := range methods {
		if data, err := method(siteURL); err == nil && len(data) > 0 {
			faviconData = data
			break
		} else {
			lastError = err
		}
	}

	if len(faviconData) == 0 {
		// Mark this host as failed
		s.failedHosts.Store(u.Host, true)
		if lastError != nil {
			return "default.ico", fmt.Errorf("failed to fetch favicon for %s: %w", siteURL, lastError)
		}
		return "default.ico", fmt.Errorf("no favicon found for %s", siteURL)
	}

	// Save the favicon
	if err := os.WriteFile(filepath, faviconData, 0644); err != nil {
		s.failedHosts.Store(u.Host, true)
		return "default.ico", fmt.Errorf("failed to save favicon: %w", err)
	}

	return filename, nil
}

func (s *Service) getFaviconFromHTML(siteURL string) ([]byte, error) {
	parsed, err := url.Parse(siteURL)
	if err != nil {
		return nil, err
	}
	if err := securitynet.ValidateHTTPURL(parsed); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", siteURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Infoscope/0.3")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d", resp.StatusCode)
	}

	const maxHTMLBytes = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxHTMLBytes {
		return nil, fmt.Errorf("HTML response exceeds %d bytes", maxHTMLBytes)
	}
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	var faviconURL string
	var findFavicon func(*html.Node)
	findFavicon = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "link" {
			var rel, href string
			for _, a := range n.Attr {
				switch a.Key {
				case "rel":
					rel = strings.ToLower(a.Val)
				case "href":
					href = a.Val
				}
			}
			if (rel == "icon" || rel == "shortcut icon") && href != "" {
				faviconURL = href
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findFavicon(c)
		}
	}
	findFavicon(doc)

	if faviconURL == "" {
		return nil, fmt.Errorf("no favicon found in HTML")
	}

	// Resolve relative URLs
	base, err := url.Parse(siteURL)
	if err != nil {
		return nil, err
	}
	resolved, err := base.Parse(faviconURL)
	if err != nil {
		return nil, err
	}

	return s.downloadFavicon(resolved.String())
}

func (s *Service) getFaviconFromRoot(siteURL string) ([]byte, error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return nil, err
	}

	faviconURL := fmt.Sprintf("%s://%s/favicon.ico", u.Scheme, u.Host)
	return s.downloadFavicon(faviconURL)
}

func (s *Service) downloadFavicon(urlStr string) ([]byte, error) {
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	if err := securitynet.ValidateHTTPURL(parsed); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Infoscope/0.3")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d", resp.StatusCode)
	}

	const maxFaviconBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFaviconBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFaviconBytes {
		return nil, fmt.Errorf("favicon exceeds %d bytes", maxFaviconBytes)
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png", "image/gif", "image/jpeg", "image/webp", "image/x-icon", "image/vnd.microsoft.icon":
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported favicon content type %q", contentType)
	}
}
