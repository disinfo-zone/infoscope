// internal/server/server.go
package server

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"infoscope/internal/auth"
	"infoscope/internal/feed"
)

//go:embed web/templates web/static
var rawContent embed.FS

// webContent holds the virtual filesystem for web assets.
var webContent fs.FS

func init() {
	var err error
	webContent, err = fs.Sub(rawContent, "web")
	if err != nil {
		panic(fmt.Sprintf("failed to create virtual filesystem for web content: %v", err))
	}
}

// safeHTML allows embedding raw HTML content in templates.
// Use with caution and only with trusted HTML.
func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

type Config struct {
	UseHTTPS               bool
	DisableTemplateUpdates bool
	WebPath                string
	ProductionMode         bool
}

type Server struct {
	db            *sql.DB
	logger        *log.Logger
	auth          *auth.Service
	feedService   *feed.Service
	imageHandler  *ImageHandler
	csrf          *CSRF
	config        Config
	templateCache map[string]*template.Template
}

func (s *Server) registerTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"formatTimeInZone": func(tz string, t time.Time) string {
			loc, err := time.LoadLocation(tz)
			if err != nil {
				s.logger.Printf("Error loading timezone '%s': %v. Falling back to UTC.", tz, err)
				return t.UTC().Format("02/01/06 15:04")
			}
			return t.In(loc).Format("02/01/06 15:04")
		},
		"time": func(layout, value string) time.Time {
			t, err := time.Parse(layout, value)
			if err != nil {
				s.logger.Printf("Error parsing time value '%s' with layout '%s': %v", value, layout, err)
				return time.Time{}
			}
			return t.UTC()
		},
		"safeHTML": safeHTML,
	}
}

func (s *Server) extractWebContent(forceUpdate bool) error {
	if !s.config.ProductionMode {
		s.logger.Printf("Checking web content (force update: %v)...", forceUpdate)
	}

	dirs := []string{
		filepath.Join(s.config.WebPath, "templates"),
		filepath.Join(s.config.WebPath, "templates/admin"),
		filepath.Join(s.config.WebPath, "static"),
		filepath.Join(s.config.WebPath, "static/favicons"),
		filepath.Join(s.config.WebPath, "static/images"),
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
		if path == "." { // Skip the root of embed.FS
			return nil
		}

		localPath := filepath.Join(s.config.WebPath, path)

		if d.IsDir() {
			// Ensure directory exists, especially if it's empty in embed.FS
			return os.MkdirAll(localPath, 0755)
		}

		needsUpdate := forceUpdate
		if !needsUpdate {
			localStat, err := os.Stat(localPath)
			if os.IsNotExist(err) {
				needsUpdate = true
			} else if err != nil {
				return fmt.Errorf("failed to stat local file %s: %w", localPath, err)
			} else {
				embeddedFile, openErr := webContent.Open(path)
				if openErr != nil {
					return fmt.Errorf("failed to open embedded file %s: %w", path, openErr)
				}
				defer embeddedFile.Close()
				embeddedInfo, statErr := embeddedFile.Stat()
				if statErr != nil {
					return fmt.Errorf("failed to stat embedded file %s: %w", path, statErr)
				}
				if embeddedInfo.Size() != localStat.Size() || embeddedInfo.ModTime().After(localStat.ModTime()) {
					needsUpdate = true
				}
			}
		}
		if needsUpdate {
			content, readErr := fs.ReadFile(webContent, path)
			if readErr != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, readErr)
			}
			if writeErr := os.WriteFile(localPath, content, 0644); writeErr != nil {
				return fmt.Errorf("failed to write file %s: %w", localPath, writeErr)
			}
			if !s.config.ProductionMode {
				s.logger.Printf("Extracted/Updated: %s", localPath)
			}
		}
		return nil
	})
}

func NewServer(db *sql.DB, logger *log.Logger, feedService *feed.Service, config Config) (*Server, error) {
	csrfConfig := DefaultConfig()
	csrfConfig.Secure = config.UseHTTPS
	csrfManager := NewCSRF(csrfConfig)

	baseImageUploadDir := filepath.Join(config.WebPath, "static", "images")
	imageHandler, err := NewImageHandler(db, logger, csrfManager, baseImageUploadDir, config.ProductionMode)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize image handler: %w", err)
	}

	s := &Server{
		db:           db,
		logger:       logger,
		auth:         auth.NewService(),
		feedService:  feedService,
		imageHandler: imageHandler,
		csrf:         csrfManager,
		config:       config,
	}

	if err := s.extractWebContent(!s.config.DisableTemplateUpdates); err != nil {
		return nil, fmt.Errorf("failed to extract web content: %w", err)
	}

	funcMap := s.registerTemplateFuncs()
	templates, err := LoadTemplates(s.config.WebPath, funcMap)
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}
	s.templateCache = templates
	if !s.config.ProductionMode {
		s.logger.Printf("Successfully loaded and cached %d templates.", len(s.templateCache))
	}

	if err := s.initializeTotalClicks(); err != nil {
		return nil, fmt.Errorf("error initializing click counts: %w", err)
	}

	if !s.config.ProductionMode {
		s.logger.Printf("Server initialized successfully")
	}
	return s, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Create a static file server with caching headers for performance optimization
	staticDir := filepath.Join(s.config.WebPath, "static")
	fileServer := http.FileServer(http.Dir(staticDir))

	// Wrap the file server with cache headers for better performance
	cachedFileServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add cache headers based on file type for performance optimization
		if filepath.Ext(r.URL.Path) == ".ico" {
			// Cache favicon files for 1 week
			w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
			w.Header().Set("Expires", time.Now().Add(7*24*time.Hour).Format(http.TimeFormat))
		} else if filepath.Ext(r.URL.Path) == ".css" || filepath.Ext(r.URL.Path) == ".js" {
			// Cache CSS and JS files for 1 day
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Header().Set("Expires", time.Now().Add(24*time.Hour).Format(http.TimeFormat))
		} else {
			// Default cache for other static files (1 hour)
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("Expires", time.Now().Add(time.Hour).Format(http.TimeFormat))
		}

		// Serve the file
		fileServer.ServeHTTP(w, r)
	})

	mux.Handle("/static/", http.StripPrefix("/static/", cachedFileServer))

	mux.HandleFunc("/setup", s.handleSetup)
	mux.HandleFunc("/setup/", s.handleSetup)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/healthz/", s.handleHealthz)
	mux.HandleFunc("/admin/login", s.handleLogin)
	mux.HandleFunc("/admin/login/", s.handleLogin)
	mux.HandleFunc("/admin/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("/admin/logout/", s.requireAuth(s.handleLogout))
	mux.HandleFunc("/admin/settings", s.requireAuth(s.handleSettings))
	mux.HandleFunc("/admin/settings/", s.requireAuth(s.handleSettings))
	mux.HandleFunc("/admin/feeds", s.requireAuth(s.handleFeeds))
	mux.HandleFunc("/admin/feeds/", s.requireAuth(s.handleFeeds))
	mux.HandleFunc("/admin/feeds/validate", s.requireAuth(s.handleFeedValidation))
	mux.HandleFunc("/admin/feeds/validate/", s.requireAuth(s.handleFeedValidation))
	mux.HandleFunc("/admin/backup", s.requireAuth(s.handleBackup))
	mux.HandleFunc("/admin/backup/", s.requireAuth(s.handleBackup))
	mux.HandleFunc("/admin/metrics", s.requireAuth(s.handleMetrics))
	mux.HandleFunc("/admin/metrics/", s.requireAuth(s.handleMetrics))
	mux.HandleFunc("/admin/change-password", s.requireAuth(s.handleChangePassword))
	mux.HandleFunc("/admin/change-password/", s.requireAuth(s.handleChangePassword))
	
	// Filter management routes
	mux.HandleFunc("/admin/filters", s.requireAuth(s.handleFilterRoutes))
	mux.HandleFunc("/admin/filters/", s.requireAuth(s.handleFilterRoutes))
	mux.HandleFunc("/admin/filter-groups", s.requireAuth(s.handleFilterGroupRoutes))
	mux.HandleFunc("/admin/filter-groups/", s.requireAuth(s.handleFilterGroupRoutes))
	mux.HandleFunc("/admin/filter-test", s.requireAuth(s.TestFilter))
	
	mux.HandleFunc("/admin", s.requireAuth(s.handleAdmin))
	mux.HandleFunc("/admin/", s.requireAuth(s.handleAdmin))
	mux.HandleFunc("/click", s.handleClick)
	mux.HandleFunc("/click/", s.handleClick)
	mux.HandleFunc("/admin/upload-image", s.requireAuth(s.imageHandler.HandleUpload))
	mux.HandleFunc("/admin/upload-favicon", s.requireAuth(s.imageHandler.HandleFaviconUpload))
	mux.HandleFunc("/admin/upload-meta-image", s.requireAuth(s.imageHandler.HandleMetaImageUpload))

	mux.HandleFunc("/rss.xml", s.handleRSS)
	mux.HandleFunc("/rss", s.handleRSS)  // New route
	mux.HandleFunc("/feed", s.handleRSS) // New route

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			s.handle404(w, r)
			return
		}
		s.handleIndex(w, r)
	})

	return mux
}

func (s *Server) handle404(w http.ResponseWriter, r *http.Request) {
	if !s.config.ProductionMode {
		s.logger.Printf("404 error for path: %s", r.URL.Path)
	}

	var data interface{} // General purpose data for 404
	// CSRF token is generally not needed for a simple 404 page.
	// If it were, it would be:
	// token := ""
	// if s.csrf != nil { token = s.csrf.Token(w, r) }
	// data = struct { CSRFToken string } { CSRFToken: token }

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	if err := s.renderTemplate(w, r, "404.html", data); err != nil { // renderTemplate is in auth_handlers.go
		s.logger.Printf("Error rendering 404 template: %v", err)
		// Fallback if template rendering fails
		http.Error(w, "404 Page Not Found", http.StatusNotFound)
	}
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		session, err := s.auth.ValidateSession(s.db, cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), contextKeyUserID, session.UserID)
		token := s.csrf.Token(w, r)
		data := struct {
			CSRFToken string
			UserID    int64
		}{CSRFToken: token, UserID: session.UserID}
		ctx = context.WithValue(ctx, contextKeyTemplateData, data)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) Start(addr string) error {
	s.logger.Printf("Starting server on %s", addr)
	return http.ListenAndServe(addr, s.Routes())
}
