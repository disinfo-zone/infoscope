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

type Config struct {
	UseHTTPS               bool
	DisableTemplateUpdates bool
	WebPath                string
	ProductionMode         bool // Ensure this field is present if used for conditional logging
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

// registerTemplateFuncs defines functions available to templates.
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
	}
}

// extractWebContent extracts embedded web content to the configured WebPath.
func (s *Server) extractWebContent(forceUpdate bool) error {
	if !s.config.ProductionMode {
		s.logger.Printf("Checking web content in %s (force update: %v)...", s.config.WebPath, forceUpdate)
	}

	dirsToCreate := []string{
		filepath.Join(s.config.WebPath, "templates", "admin"),
		filepath.Join(s.config.WebPath, "static", "favicons"),
		filepath.Join(s.config.WebPath, "static", "images"), // Ensure base images dir also exists
	}
	for _, dir := range dirsToCreate {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return fs.WalkDir(webContent, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking embedded content at %s: %w", path, err)
		}
		if path == "." {
			return nil
		}
		localPath := filepath.Join(s.config.WebPath, path)
		if d.IsDir() {
			if err := os.MkdirAll(localPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", localPath, err)
			}
			return nil
		}

		needsUpdate := forceUpdate
		if !needsUpdate {
			localStat, statErr := os.Stat(localPath)
			if os.IsNotExist(statErr) {
				needsUpdate = true
			} else if statErr != nil {
				return fmt.Errorf("failed to stat local file %s: %w", localPath, statErr)
			} else {
				embeddedFile, openErr := webContent.Open(path)
				if openErr != nil {
					return fmt.Errorf("failed to open embedded file %s: %w", path, openErr)
				}
				defer embeddedFile.Close()
				embeddedStat, statErrIn := embeddedFile.Stat()
				if statErrIn != nil {
					return fmt.Errorf("failed to stat embedded file %s: %w", path, statErrIn)
				}
				if localStat.Size() != embeddedStat.Size() {
					needsUpdate = true
				}
			}
		}

		if needsUpdate {
			if !s.config.ProductionMode {
				s.logger.Printf("Extracting/updating %s to %s", path, localPath)
			}
			content, readErr := fs.ReadFile(webContent, path)
			if readErr != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", path, readErr)
			}
			if writeErr := os.WriteFile(localPath, content, 0644); writeErr != nil {
				return fmt.Errorf("failed to write file %s: %w", localPath, writeErr)
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
	// Pass config.ProductionMode to NewImageHandler
	imageHandler, err := NewImageHandler(db, logger, csrfManager, baseImageUploadDir, config.ProductionMode)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize image handler: %w", err)
	}

	s := &Server{
		db:            db,
		logger:        logger,
		auth:          auth.NewService(),
		feedService:   feedService,
		imageHandler:  imageHandler,
		csrf:          csrfManager,
		config:        config, // config now includes ProductionMode
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

	fileServer := http.FileServer(http.Dir(filepath.Join(s.config.WebPath, "static")))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

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
	mux.HandleFunc("/admin", s.requireAuth(s.handleAdmin))
	mux.HandleFunc("/admin/", s.requireAuth(s.handleAdmin))
	mux.HandleFunc("/click", s.handleClick)
	mux.HandleFunc("/click/", s.handleClick)
	mux.HandleFunc("/admin/upload-favicon", s.requireAuth(s.imageHandler.HandleFaviconUpload))
	mux.HandleFunc("/admin/upload-meta-image", s.requireAuth(s.imageHandler.HandleMetaImageUpload))

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
	// Error logs should generally not be conditional
	s.logger.Printf("404 error for path: %s", r.URL.Path)
	data := struct{ CSRFToken string; Data any }{CSRFToken: s.csrf.Token(w, r), Data: nil}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if err := s.renderTemplate(w, r, "404.html", data); err != nil {
		s.logger.Printf("Error rendering 404 template: %v", err)
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
		data := struct{ CSRFToken string; UserID int64 }{CSRFToken: token, UserID: session.UserID}
		ctx = context.WithValue(ctx, contextKeyTemplateData, data)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) Start(addr string) error {
	s.logger.Printf("Starting server on %s", addr) // This can remain, it's a startup message.
	return http.ListenAndServe(addr, s.Routes())
}
```

Note: The `Config` struct in `server.go` might not have `ProductionMode` field. If it's missing, it should be added from `internal/config/config.go`'s `Config` struct definition or by ensuring `server.Config` mirrors `config.Config`. For this step, I've assumed `server.Config` will have `ProductionMode`. If not, the `s.config.ProductionMode` checks would fail to compile, and `config.ProductionMode` (from the `config` parameter of `NewServer`) should be used directly or stored appropriately. The `config` parameter passed to `NewServer` *is* the one from `infoscope/internal/config`, which has `ProductionMode`. So, `config.ProductionMode` should be used when initializing `ImageHandler` and for conditional logs within `NewServer` before `s.config` is fully set, or ensure `s.config = config` happens early. My generated code for `NewServer` sets `s.config = config` early enough.

The `ensure base images dir also exists` comment was added to `extractWebContent` to ensure the `static/images` path is created if it doesn't exist, as `favicons` is a subdirectory of it.

Now, let's update `image_handler.go`.
