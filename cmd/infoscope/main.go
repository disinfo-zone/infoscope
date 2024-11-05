package main

import (
	"context"
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
	port     = flag.Int("port", 0, "Port to run the server on (default: 8080 or INFOSCOPE_PORT)")
	dbPath   = flag.String("db", "", "Path to database file (default: data/infoscope.db or INFOSCOPE_DB_PATH)")
	dataPath = flag.String("data", "", "Path to data directory (default: data or INFOSCOPE_DATA_PATH)")
	version  = flag.Bool("version", false, "Print version information")
	prodMode = flag.Bool("prod", false, "Enable production mode (HTTPS-only features including strict CSRF)")
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

	// Set production mode
	cfg.ProductionMode = *prodMode

	// Log startup configuration
	logger.Printf("Starting Infoscope v%s", Version)
	logger.Printf("Port: %d", cfg.Port)
	logger.Printf("Database: %s", cfg.DBPath)
	logger.Printf("Data directory: %s", cfg.DataPath)
	logger.Printf("Mode: %s", map[bool]string{true: "production", false: "development"}[cfg.ProductionMode])

	// Create necessary directories
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		logger.Fatalf("Failed to create database directory: %v", err)
	}

	// Initialize database with optimized configuration
	dbConfig := database.DefaultConfig()
	db, err := database.NewDB(cfg.DBPath, dbConfig)
	if err != nil {
		logger.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Create static directories
	staticDirs := []string{
		filepath.Join("web", "static"),
		filepath.Join("web", "static", "favicons"),
	}
	for _, dir := range staticDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			logger.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Initialize favicon service
	faviconSvc, err := favicon.NewService(filepath.Join("web", "static", "favicons"))
	if err != nil {
		logger.Fatalf("Failed to initialize favicon service: %v", err)
	}

	// Initialize feed service
	feedService := feed.NewService(db.DB, logger, faviconSvc)
	feedService.Start()
	defer feedService.Stop()

	// Do initial feed fetch
	if err := feedService.UpdateFeeds(context.Background()); err != nil {
		logger.Printf("Initial feed update failed: %v", err)
	}

	// Initialize server with configuration
	srv, err := server.NewServer(db.DB, logger, feedService, server.Config{
		UseHTTPS: cfg.ProductionMode,
	})
	if err != nil {
		logger.Fatalf("Failed to initialize server: %v", err)
	}

	// Start server
	logger.Printf("Starting server on port %d", cfg.Port)
	if err := srv.Start(cfg.GetAddress()); err != nil {
		logger.Fatalf("Server error: %v", err)
	}
}
