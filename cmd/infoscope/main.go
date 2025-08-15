package main

import (
	"flag"
	"fmt"
	"infoscope/internal/auth"
	"infoscope/internal/config"
	"infoscope/internal/database"
	"infoscope/internal/favicon"
	"infoscope/internal/feed"
	"infoscope/internal/server"
	"log"
	"net/http" // Added import
	"os"
	"path/filepath"
	"time"
)

var (
	// Version will be set during build
	Version = "dev"

	// Command line flags
	port              = flag.Int("port", 0, "Port to run the server on (default: 8080 or INFOSCOPE_PORT)")
	dbPath            = flag.String("db", "", "Path to database file (default: data/infoscope.db or INFOSCOPE_DB_PATH)")
	dataPath          = flag.String("data", "", "Path to data directory (default: data or INFOSCOPE_DATA_PATH)")
	version           = flag.Bool("version", false, "Print version information")
	prodMode          = flag.Bool("prod", false, "Enable production mode (HTTPS-only features including strict CSRF)")
	noTemplateUpdates    = flag.Bool("no-template-updates", false, "Disable automatic template updates")
	forceTemplateUpdates = flag.Bool("force-template-updates", false, "Force template updates even when disabled")
	webPath              = flag.String("web", "", "Path to web content directory (default: web or INFOSCOPE_WEB_PATH)")
	healthcheck          = flag.Bool("healthcheck", false, "Perform a health check and exit")
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Get base configuration from environment first to determine port for healthcheck
	cfg := config.GetConfig()
	// Override with command line flags if provided for port (needed for healthcheck URL)
	if *port > 0 {
		cfg.Port = *port
	}

	if *healthcheck {
		// Perform health check
		healthURL := fmt.Sprintf("http://localhost:%d/healthz", cfg.Port)
		resp, err := http.Get(healthURL)
		if err != nil {
			log.Printf("Health check failed: %v", err)
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			// Optionally, read and print body if needed for more detailed status
			// body, _ := io.ReadAll(resp.Body)
			// fmt.Printf("Health check OK: %s", body)
			os.Exit(0)
		} else {
			log.Printf("Health check failed: status code %d", resp.StatusCode)
			os.Exit(1)
		}
		return // Should not be reached due to os.Exit
	}

	// Check if version flag is set
	if *version {
		fmt.Printf("Infoscope version %s\n", Version)
		return
	}

	// Setup logging
	logger := log.New(os.Stdout, "infoscope: ", log.LstdFlags|log.Lshortfile)
	// Re-apply other config overrides now that we're past healthcheck

	// Get base configuration from environment (already done for healthcheck port)
	// cfg := config.GetConfig() // This would reset cfg if uncommented

	// Override with command line flags if provided (port already handled)
	// if *port > 0 {
	// 	cfg.Port = *port
	// }
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	if *dataPath != "" {
		cfg.DataPath = *dataPath
	}
	if *webPath != "" {
		cfg.WebPath = *webPath
	}

	// Handle template update flags
	cfg.DisableTemplateUpdates = *noTemplateUpdates
	
	// Force updates override disable setting
	if *forceTemplateUpdates {
		cfg.DisableTemplateUpdates = false
	}

	// Determine if the -prod flag was explicitly set
	prodFlagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "prod" {
			prodFlagSet = true
		}
	})

	if prodFlagSet {
		cfg.ProductionMode = *prodMode
	}
	// Otherwise, cfg.ProductionMode (already set by config.GetConfig()) is used.

	// Log startup configuration
	logger.Printf("Starting Infoscope v%s", Version)
	logger.Printf("Port: %d", cfg.Port)
	logger.Printf("Database: %s", cfg.DBPath)
	logger.Printf("Data directory: %s", cfg.DataPath)
	logger.Printf("Mode: %s", map[bool]string{true: "production", false: "development"}[cfg.ProductionMode])

	// Create database directory
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		logger.Fatalf("Failed to create database directory: %v", err)
	}

	// Initialize database
	dbConfig := database.DefaultConfig()
	db, err := database.NewDB(cfg.DBPath, dbConfig)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Start periodic cleanup of expired sessions
	go func() {
		ticker := time.NewTicker(6 * time.Hour) // Cleanup every 6 hours
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logger.Println("Cleaning up expired sessions...")
				if err := auth.CleanExpiredSessions(db.DB); err != nil {
					logger.Printf("Error cleaning expired sessions: %v", err)
				}
				// Add a way to stop this goroutine if the app had a global done channel
				// case <-ctx.Done():
				// logger.Println("Stopping session cleanup goroutine.")
				// return
			}
		}
	}()

	// Create required directories with configured web path
	requiredDirs := []string{
		filepath.Join(cfg.WebPath, "static"),
		filepath.Join(cfg.WebPath, "static", "images"),
		filepath.Join(cfg.WebPath, "static", "images", "favicon"),
		filepath.Join(cfg.WebPath, "templates"),
		filepath.Join(cfg.WebPath, "templates", "admin"),
	}
	for _, dir := range requiredDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Initialize favicon service with configured path
	faviconSvc, err := favicon.NewService(filepath.Join(cfg.WebPath, "static", "favicons"))
	if err != nil {
		logger.Fatalf("Failed to initialize favicon service: %v", err)
	}
	// Initialize feed service
	feedService := feed.NewService(db.DB, logger, faviconSvc)
	feedService.Start()
	defer feedService.Stop()

	// Initialize server with configuration
	srv, err := server.NewServer(db.DB, logger, feedService, server.Config{
		UseHTTPS:               cfg.ProductionMode,
		DisableTemplateUpdates: cfg.DisableTemplateUpdates,
		WebPath:                cfg.WebPath,
		ProductionMode:         cfg.ProductionMode,
	})
	if err != nil {
		logger.Fatalf("Failed to initialize server: %v", err)
	}

	// Start the server
	addr := fmt.Sprintf(":%d", cfg.Port)
	logger.Printf("Server listening on %s", addr)
	if err := srv.Start(addr); err != nil {
		logger.Fatalf("Server error: %v", err)
	}
}
