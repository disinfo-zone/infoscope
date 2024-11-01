// internal/database/schema.go
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const schema = `
-- Settings table with type validation
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    type TEXT CHECK(type IN ('string', 'int', 'bool')) NOT NULL,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Feeds table with optimized columns and constraints
CREATE TABLE IF NOT EXISTS feeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT UNIQUE NOT NULL,
    title TEXT,
    last_fetched TIMESTAMP,
    last_modified TEXT,
    etag TEXT,
    status TEXT NOT NULL CHECK(status IN ('active', 'error', 'disabled')) DEFAULT 'active',
    error_count INTEGER DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Entries table with optimized structure and constraints
CREATE TABLE IF NOT EXISTS entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    guid TEXT,
    published_at TIMESTAMP NOT NULL,
    favicon_url TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid),
    UNIQUE(url),
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
);

-- Blacklist table with timestamps
CREATE TABLE IF NOT EXISTS blacklist (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT UNIQUE NOT NULL,
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Admin users table with enhanced security
CREATE TABLE IF NOT EXISTS admin_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    last_login TIMESTAMP,
    login_attempts INTEGER DEFAULT 0,
    locked_until TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Sessions table with proper cleanup support
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    FOREIGN KEY (user_id) REFERENCES admin_users(id) ON DELETE CASCADE
);

-- Clicks table with optimized structure
CREATE TABLE IF NOT EXISTS clicks (
    entry_id INTEGER NOT NULL,
    click_count INTEGER DEFAULT 0,
    last_clicked TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (entry_id),
    FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
);

-- Click stats with atomic updates
CREATE TABLE IF NOT EXISTS click_stats (
    key TEXT PRIMARY KEY,
    value INTEGER NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create optimized indexes
CREATE INDEX IF NOT EXISTS idx_feeds_status ON feeds(status, last_fetched);
CREATE INDEX IF NOT EXISTS idx_feeds_error ON feeds(error_count) WHERE error_count > 0;
CREATE INDEX IF NOT EXISTS idx_entries_feed_date ON entries(feed_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_entries_date ON entries(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_clicks_count ON clicks(click_count DESC, last_clicked DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);
`

const versionSchema = `
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_version (version) VALUES (1);
`

// DB represents our database connection and operations
type DB struct {
	*sql.DB
}

// Configuration for the database
type Config struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultConfig returns the default database configuration
func DefaultConfig() Config {
	return Config{
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: time.Hour,
		ConnMaxIdleTime: 5 * time.Minute,
	}
}

// NewDB creates a new database connection with optimized settings
func NewDB(dbPath string, cfg Config) (*DB, error) {
	// Add query parameters to optimize SQLite performance
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON&_synchronous=NORMAL", dbPath)

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("error connecting to database: %w", err)
	}

	// Create schema
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("error creating schema: %w", err)
	}

	return &DB{db}, nil
}

// createSchema initializes the database schema
func createSchema(db *sql.DB) error {
	// First execute PRAGMA statements outside of transaction
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=10000",
		"PRAGMA temp_store=MEMORY",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("error setting pragma: %w", err)
		}
	}

	// Create version table first
	if _, err := db.Exec(versionSchema); err != nil {
		return fmt.Errorf("error creating version table: %w", err)
	}

	// Get current version
	var currentVersion int
	err := db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&currentVersion)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("error getting schema version: %w", err)
	}

	// If we're already at version 1, return
	if currentVersion >= 1 {
		return nil
	}

	// Now create tables within a transaction
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Execute schema
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("error executing schema: %w", err)
	}

	// Insert default settings
	defaultSettings := []struct {
		key, value, valueType string
	}{
		{"max_posts", "33", "int"},
		{"update_interval", "900", "int"},
		{"site_title", "infoscope_", "string"},
		{"header_link_url", "https://disinfo.zone", "string"},
		{"header_link_text", "<< disinfo.zone", "string"},
		{"footer_link_url", "https://disinfo.zone", "string"},
		{"footer_link_text", "<< disinfo.zone", "string"},
		{"footer_image_url", "infoscope.png", "string"},
		{"footer_image_height", "100px", "string"},
		{"tracking_code", "", "string"},
	}

	for _, setting := range defaultSettings {
		_, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO settings (key, value, type) VALUES (?, ?, ?)",
			setting.key, setting.value, setting.valueType)
		if err != nil {
			return fmt.Errorf("error inserting default setting %s: %w", setting.key, err)
		}
	}

	// Initialize click stats
	_, err = tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO click_stats (key, value) VALUES ('total_clicks', 0)")
	if err != nil {
		return fmt.Errorf("error initializing click stats: %w", err)
	}

	return tx.Commit()
}

// Cleanup performs maintenance operations
func (db *DB) Cleanup(ctx context.Context) error {
	queries := []string{
		// Clean up expired sessions
		"DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP",

		// Reset failed login attempts after lockout period
		"UPDATE admin_users SET login_attempts = 0 WHERE locked_until < CURRENT_TIMESTAMP",

		// Clean up old entries beyond retention limit
		`DELETE FROM entries WHERE id IN (
			SELECT e.id
			FROM entries e
			JOIN feeds f ON e.feed_id = f.id
			WHERE e.id NOT IN (
				SELECT e2.id
				FROM entries e2
				WHERE e2.feed_id = f.id
				ORDER BY published_at DESC
				LIMIT (SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'max_posts')
			)
		)`,

		// Optimize database
		"PRAGMA optimize",
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("error beginning cleanup transaction: %w", err)
	}
	defer tx.Rollback()

	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("error executing cleanup query: %w", err)
		}
	}

	return tx.Commit()
}
