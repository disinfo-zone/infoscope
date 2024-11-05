package main

import (
	"flag"
	"fmt"
	"infoscope/internal/config"
	"infoscope/internal/database"
	"infoscope/internal/favicon"
	"infoscope/internal/feed"
	"infoscope/internal/server"
	"log"
	"os"
	"path/filepath"
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
	noTemplateUpdates = flag.Bool("no-template-updates", false, "Disable automatic template updates")
	webPath           = flag.String("web", "", "Path to web content directory (default: web or INFOSCOPE_WEB_PATH)")
)

func main() {
	// Parse command line flags
	flag.Parse()

	// Check if version flag is set
	if *version {
		fmt.Printf("Infoscope version %s\n", Version)
		return
	}

	// Setup logging
	logger := log.New(os.Stdout, "infoscope: ", log.LstdFlags|log.Lshortfile)

	// Get base configuration from environment
	cfg := config.GetConfig()

	// Override with command line flags if provided
	if *port > 0 {
		cfg.Port = *port
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}
	if *dataPath != "" {
		cfg.DataPath = *dataPath
	}
	if *webPath != "" {
		cfg.WebPath = *webPath
	}

	// Disable template updates if flag is set
	cfg.DisableTemplateUpdates = *noTemplateUpdates

	// Set production mode
	cfg.ProductionMode = *prodMode

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

	// Create required directories with configured web path
	requiredDirs := []string{
		filepath.Join(cfg.WebPath, "static"),
		filepath.Join(cfg.WebPath, "static", "favicons"),
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
