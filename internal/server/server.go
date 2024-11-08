// internal/server/server.go
package server

import (
	"context"
	"database/sql"
	"fmt"
	"infoscope/internal/auth"
	"infoscope/internal/feed"
	"log"
	"net/http"
	"path/filepath"
)

type Config struct {
	UseHTTPS               bool
	DisableTemplateUpdates bool
	WebPath                string
}

type Server struct {
	db           *sql.DB
	logger       *log.Logger
	auth         *auth.Service
	settings     *SettingsManager
	feedService  *feed.Service
	imageHandler *ImageHandler
	csrf         *CSRF
	config       Config
}

func NewServer(db *sql.DB, logger *log.Logger, feedService *feed.Service, config Config) (*Server, error) {
	// Initialize image handler
	imageHandler, err := NewImageHandler(db, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize image handler: %w", err)
	}

	// Initialize CSRF with configuration
	csrfConfig := DefaultConfig()
	csrfConfig.Secure = config.UseHTTPS

	// Create server instance
	s := &Server{
		db:           db,
		logger:       logger,
		auth:         auth.NewService(),
		settings:     NewSettingsManager(),
		feedService:  feedService,
		imageHandler: imageHandler,
		csrf:         NewCSRF(csrfConfig),
		config:       config,
	}

	// Extract web content if needed, force update if not disabled
	if err := s.extractWebContent(!config.DisableTemplateUpdates); err != nil {
		return nil, fmt.Errorf("failed to extract web content: %w", err)
	}

	s.logger.Printf("Server initialized successfully")
	return s, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Serve static files
	fileServer := http.FileServer(http.Dir(filepath.Join(s.config.WebPath, "static")))
	mux.Handle("/static/", http.StripPrefix("/static/", fileServer))

	// Setup endpoints
	mux.HandleFunc("/setup", s.handleSetup)
	mux.HandleFunc("/setup/", s.handleSetup)

	// Admin routes
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
	mux.HandleFunc("/admin", s.requireAuth(s.handleAdmin))
	mux.HandleFunc("/admin/", s.requireAuth(s.handleAdmin))

	// Click tracking
	mux.HandleFunc("/click", s.handleClick)
	mux.HandleFunc("/click/", s.handleClick)

	// Handle root and all unmatched paths
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			s.handle404(w, r)
			return
		}
		s.handleIndex(w, r)
	})

	return mux
}

// handle 404 pages for unspecified html routes
func (s *Server) handle404(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("404 error for path: %s", r.URL.Path)

	// Create template data
	data := struct {
		CSRFToken string
		Data      any
	}{
		CSRFToken: s.csrf.Token(w, r),
		Data:      nil,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)

	if err := s.renderTemplate(w, r, "404.html", data); err != nil {
		s.logger.Printf("Error rendering 404 template: %v", err)
		http.Error(w, "404 Page Not Found", http.StatusNotFound)
	}
}

// requireAuth wraps handlers with authentication and CSRF token injection
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check authentication
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Validate session and get user ID
		session, err := s.auth.ValidateSession(s.db, cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		// Create new context with user ID
		ctx := context.WithValue(r.Context(), contextKeyUserID, session.UserID)

		// Get CSRF token
		token := s.csrf.Token(w, r)

		// Add template data
		data := struct {
			CSRFToken string
			UserID    int64
		}{
			CSRFToken: token,
			UserID:    session.UserID,
		}

		// Add template data to context
		ctx = context.WithValue(ctx, contextKeyTemplateData, data)

		// Call next handler with updated context
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) Start(addr string) error {
	s.logger.Printf("Starting server on %s", addr)
	return http.ListenAndServe(addr, s.Routes())
}
