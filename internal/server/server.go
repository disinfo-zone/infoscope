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

	// Setup route - must be checked before other routes
	mux.HandleFunc("/setup", s.handleSetup)

	// Public routes
	mux.HandleFunc("/", s.handleIndex)
	staticPath := filepath.Join(s.config.WebPath, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticPath))))

	// Auth routes
	mux.HandleFunc("/admin/login", s.handleLogin)
	mux.HandleFunc("/admin/logout", s.handleLogout)

	// Admin dashboard route (authentication handled inside)
	mux.HandleFunc("/admin", s.handleAdmin)

	// Protected admin routes (middleware enforced)
	mux.HandleFunc("/admin/feeds", s.requireAuth(s.handleFeeds))
	mux.HandleFunc("/admin/settings", s.requireAuth(s.handleSettings))
	mux.HandleFunc("/admin/upload-image", s.requireAuth(s.imageHandler.HandleUpload))
	mux.HandleFunc("/admin/backup", s.requireAuth(s.handleBackup))
	mux.HandleFunc("/admin/feeds/validate", s.requireAuth(s.handleFeedValidation))
	mux.HandleFunc("/admin/metrics", s.requireAuth(s.handleMetrics))

	// Click Tracking
	mux.HandleFunc("/click", s.handleClick)

	// Apply CSRF middleware to all routes except /setup
	handler := http.Handler(mux)
	handler = s.csrf.MiddlewareExceptPaths(handler, []string{"/setup"})

	return handler
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
