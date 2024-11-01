// internal/server/server.go
package server

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"infoscope/internal/auth"
	"infoscope/internal/feed"
	"log"
	"net/http"
)

type Server struct {
	db           *sql.DB
	logger       *log.Logger
	auth         *auth.Service
	settings     *SettingsManager
	feedService  *feed.Service
	imageHandler *ImageHandler
	csrfManager  *CSRFManager
}

func NewServer(db *sql.DB, logger *log.Logger, feedService *feed.Service) (*Server, error) {
	imageHandler, err := NewImageHandler(db, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create image handler: %w", err)
	}

	return &Server{
		db:           db,
		logger:       logger,
		auth:         auth.NewService(),
		settings:     NewSettingsManager(),
		feedService:  feedService,
		imageHandler: imageHandler,
		csrfManager:  NewCSRFManager(),
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Setup route - must be checked before other routes
	mux.HandleFunc("/setup", s.handleSetup)

	// Public routes
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Auth routes
	mux.HandleFunc("/admin/login", s.setCSRFToken(s.handleLogin))
	mux.HandleFunc("/admin/logout", s.handleLogout)

	// Protected admin routes with auth middleware
	mux.HandleFunc("/admin", s.requireAuth(s.handleAdmin))
	mux.HandleFunc("/admin/feeds", s.requireAuth(s.handleFeeds))
	mux.HandleFunc("/admin/settings", s.requireAuth(s.handleSettings))
	mux.HandleFunc("/admin/upload-image", s.requireAuth(s.imageHandler.HandleUpload))
	mux.HandleFunc("/admin/backup", s.requireAuth(s.handleBackup))
	mux.HandleFunc("/admin/feeds/validate", s.requireAuth(s.handleFeedValidation))
	mux.HandleFunc("/admin/metrics", s.requireAuth(s.handleMetrics))

	// Click Tracking
	mux.HandleFunc("/click", s.handleClick)

	// Create middleware chain using http.Handler interface
	var handler http.Handler = mux
	handler = s.dbMetricsMiddleware(handler)
	handler = s.csrfManager.CSRFMiddleware(handler)

	return handler
}

// requireAuth wraps handlers with session authentication
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// First check authentication
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

		// Add user info to context
		ctx := context.WithValue(r.Context(), contextKeyUserID, session.UserID)

		// Set CSRF token
		token, err := s.csrfManager.SetCSRFToken(w) // Capture both return values
		if err != nil {
			s.logger.Printf("Error setting CSRF token: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Add CSRF meta to context
		meta := template.HTML(fmt.Sprintf(`<meta name="csrf-token" content="%s">`, token))
		ctx = context.WithValue(ctx, contextKeyCSRFMeta, meta)

		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) setCSRFToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			token, err := s.csrfManager.SetCSRFToken(w) // Capture both return values
			if err != nil {
				s.logger.Printf("Error setting CSRF token: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}

			meta := template.HTML(fmt.Sprintf(`<meta name="csrf-token" content="%s">`, token))
			ctx := context.WithValue(r.Context(), contextKeyCSRFMeta, meta)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	}
}

func (s *Server) Start(addr string) error {
	// Check if setup is needed
	isFirstRun, err := IsFirstRun(s.db)
	if err != nil {
		return fmt.Errorf("failed to check first run status: %w", err)
	}
	if isFirstRun {
		s.logger.Println("First run detected - setup required")
	}

	s.logger.Printf("Starting server on %s", addr)
	return http.ListenAndServe(addr, s.Routes())
}
