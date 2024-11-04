// internal/server/server.go
package server

import (
	"context"
	"database/sql"
	"infoscope/internal/auth"
	"infoscope/internal/feed"
	"log"
	"net/http"
)

type Config struct {
	UseHTTPS bool
}

type Server struct {
	db           *sql.DB
	logger       *log.Logger
	auth         *auth.Service
	settings     *SettingsManager
	feedService  *feed.Service
	imageHandler *ImageHandler
	csrf         *CSRF
}

func NewServer(db *sql.DB, logger *log.Logger, feedService *feed.Service, config Config) (*Server, error) {
	// Initialize image handler
	imageHandler, err := NewImageHandler(db, logger)
	if err != nil {
		return nil, err
	}

	// Initialize CSRF with configuration
	csrfConfig := DefaultConfig()
	csrfConfig.Secure = config.UseHTTPS

	return &Server{
		db:           db,
		logger:       logger,
		auth:         auth.NewService(),
		settings:     NewSettingsManager(),
		feedService:  feedService,
		imageHandler: imageHandler,
		csrf:         NewCSRF(csrfConfig),
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Setup route - must be checked before other routes
	mux.Handle("/setup", s.csrf.Middleware(http.HandlerFunc(s.handleSetup)))

	// Public routes
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

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

	// Apply middleware
	var handler http.Handler = mux
	handler = s.csrf.Middleware(handler)

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
