// internal/server/templates.go
package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

//go:embed web/templates web/static
var rawContent embed.FS

// Initialize virtual filesystem during init
var webContent fs.FS

func init() {
	var err error
	webContent, err = fs.Sub(rawContent, "web")
	if err != nil {
		panic(fmt.Sprintf("failed to create virtual filesystem: %v", err))
	}
}

// extractWebContent extracts all embedded web content to disk
func (s *Server) extractWebContent(forceUpdate bool) error {
	s.logger.Printf("Checking web content...")

	// Use configured web path
	dirs := []string{
		filepath.Join(s.config.WebPath, "templates"),
		filepath.Join(s.config.WebPath, "templates/admin"),
		filepath.Join(s.config.WebPath, "static"),
		filepath.Join(s.config.WebPath, "static/favicons"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return fs.WalkDir(webContent, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}

		// Use configured web path for local files
		localPath := filepath.Join(s.config.WebPath, path)

		if d.IsDir() {
			return os.MkdirAll(localPath, 0755)
		}

		needsUpdate := forceUpdate
		if !needsUpdate {
			if stat, err := os.Stat(localPath); err != nil {
				needsUpdate = true
			} else {
				embeddedFile, _ := webContent.Open(path)
				if embeddedInfo, err := embeddedFile.Stat(); err == nil {
					needsUpdate = embeddedInfo.Size() != stat.Size()
				}
				embeddedFile.Close()
			}
		}

		if needsUpdate {
			content, err := fs.ReadFile(webContent, path)
			if err != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, err)
			}
			if err := os.WriteFile(localPath, content, 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", localPath, err)
			}
			s.logger.Printf("Updated: %s", localPath)
		}
		return nil
	})
}

func (s *Server) registerTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatTimeInZone": func(tz string, t time.Time) string {
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return t.UTC().Format("02/01/06 15:04")
			}
			return t.In(loc).Format("02/01/06 15:04")
		},
		"time": func(layout, value string) time.Time {
			t, err := time.Parse(layout, value)
			if err != nil {
				return time.Time{}
			}
			return t.UTC()
		},
	}
}
