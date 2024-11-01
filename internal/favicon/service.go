// Save as: internal/favicon/service.go
package favicon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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

	return &Service{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		storageDir:  storageDir,
		failedHosts: sync.Map{}, // Initialize the map
	}, nil
}

func (s *Service) GetFavicon(siteURL string) (string, error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return "default.ico", nil
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
	resp, err := s.client.Get(siteURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
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

func (s *Service) downloadFavicon(url string) ([]byte, error) {
	resp, err := s.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
