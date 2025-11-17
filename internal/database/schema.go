// internal/database/schema.go
// Database schema and migration logic for Infoscope RSS Reader
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// internal/database/schema.go

const Schema = `
-- Settings table
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT,
    type TEXT,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Feeds table
CREATE TABLE IF NOT EXISTS feeds (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    url TEXT UNIQUE NOT NULL,
    title TEXT,
    status TEXT DEFAULT 'pending',
    error_count INTEGER DEFAULT 0,
    last_error TEXT,
    last_fetched TIMESTAMP,
    last_modified TEXT,
    etag TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Entries table
CREATE TABLE IF NOT EXISTS entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL,
    title TEXT NOT NULL,
    url TEXT NOT NULL UNIQUE,
    content TEXT,
    guid TEXT,
    published_at TIMESTAMP NOT NULL,
    favicon_url TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE
);

-- Admin users table
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

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    ip_address TEXT,
    user_agent TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    FOREIGN KEY (user_id) REFERENCES admin_users(id) ON DELETE CASCADE
);

-- Click tracking table
CREATE TABLE IF NOT EXISTS clicks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id INTEGER NOT NULL,
    click_count INTEGER DEFAULT 1,
    last_clicked TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE,
    UNIQUE(entry_id)
);

-- Click stats table
CREATE TABLE IF NOT EXISTS click_stats (
    key TEXT PRIMARY KEY,
    value INTEGER NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Entry filters table
CREATE TABLE IF NOT EXISTS entry_filters (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    pattern TEXT NOT NULL,
    pattern_type TEXT NOT NULL CHECK(pattern_type IN ('keyword', 'regex')),
    target_type TEXT NOT NULL DEFAULT 'title' CHECK(target_type IN ('title', 'content', 'feed_tags', 'feed_category')),
    case_sensitive BOOLEAN NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Filter groups table
CREATE TABLE IF NOT EXISTS filter_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('keep', 'discard')),
    is_active BOOLEAN NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 0,
    apply_to_category TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Filter group rules table (junction table)
CREATE TABLE IF NOT EXISTS filter_group_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL,
    filter_id INTEGER NOT NULL,
    operator TEXT NOT NULL CHECK(operator IN ('AND', 'OR')) DEFAULT 'AND',
    position INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (group_id) REFERENCES filter_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (filter_id) REFERENCES entry_filters(id) ON DELETE CASCADE
);

-- Tags table for feed taxonomy
CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL COLLATE NOCASE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Feed tags junction table for many-to-many relationship
CREATE TABLE IF NOT EXISTS feed_tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (feed_id) REFERENCES feeds(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE,
    UNIQUE(feed_id, tag_id)
);`

const Indexes = `
-- Feed indexes
CREATE INDEX IF NOT EXISTS idx_feeds_status ON feeds(status, last_fetched);
CREATE INDEX IF NOT EXISTS idx_feeds_error ON feeds(error_count) WHERE error_count > 0;

-- Entry indexes
CREATE INDEX IF NOT EXISTS idx_entries_feed_date ON entries(feed_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_entries_published ON entries(published_at DESC);

-- Click tracking indexes
CREATE INDEX IF NOT EXISTS idx_clicks_count ON clicks(click_count DESC);
CREATE INDEX IF NOT EXISTS idx_clicks_date ON clicks(last_clicked DESC);

-- Session index
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);

-- Filter indexes
CREATE INDEX IF NOT EXISTS idx_entry_filters_type ON entry_filters(pattern_type);
CREATE INDEX IF NOT EXISTS idx_filter_groups_active_priority ON filter_groups(is_active, priority);
CREATE INDEX IF NOT EXISTS idx_filter_group_rules_group_position ON filter_group_rules(group_id, position);

-- Tag indexes
CREATE INDEX IF NOT EXISTS idx_tags_name ON tags(name COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_feed_tags_feed ON feed_tags(feed_id);
CREATE INDEX IF NOT EXISTS idx_feed_tags_tag ON feed_tags(tag_id);`

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
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON&_synchronous=NORMAL",
		dbPath)

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

func createSchema(db *sql.DB) error {
	// Keep existing pragma optimizations
	if _, err := db.Exec(`
        PRAGMA journal_mode=WAL;
        PRAGMA foreign_keys=OFF;
        PRAGMA synchronous=NORMAL;
        PRAGMA cache_size=10000;
        PRAGMA temp_store=MEMORY;
    `); err != nil {
		return fmt.Errorf("error setting pragmas: %w", err)
	}

	// Start transaction for table creation
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()

	// Create tables within transaction
	if _, err := tx.Exec(Schema); err != nil {
		return fmt.Errorf("error executing schema: %w", err)
	}

	// Commit transaction to ensure tables are created
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing schema: %w", err)
	}

	// Check and add columns if missing
	columnUpdates := []struct {
		table, column, definition string
	}{
		{"entries", "content", "TEXT"},
		{"entries", "guid", "TEXT"},
		{"feeds", "status", "TEXT DEFAULT 'pending'"},
		{"feeds", "error_count", "INTEGER DEFAULT 0"},
		{"feeds", "last_error", "TEXT"},
		{"settings", "timezone", "TEXT DEFAULT 'UTC'"},
		{"settings", "favicon_url", "TEXT DEFAULT 'favicon.ico'"},
	}

	for _, col := range columnUpdates {
		exists, err := columnExists(db, col.table, col.column)
		if err != nil {
			return fmt.Errorf("error checking column %s.%s: %w", col.table, col.column, err)
		}
		if !exists {
			_, err := db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
				col.table, col.column, col.definition))
			if err != nil {
				return fmt.Errorf("error adding column %s.%s: %w", col.table, col.column, err)
			}
		}
	}

	// Keep existing migrations
	if err := performMigrations(db); err != nil {
		return fmt.Errorf("error performing migrations: %w", err)
	}

	// Migrate filter tables
	if err := migrateFilterTables(db); err != nil {
		return fmt.Errorf("error migrating filter tables: %w", err)
	}

	// Migrate taxonomy tables (categories and tags)
	if err := migrateTaxonomyTables(db); err != nil {
		return fmt.Errorf("error migrating taxonomy tables: %w", err)
	}

	// Create indexes after tables are committed
	if _, err := db.Exec(Indexes); err != nil {
		return fmt.Errorf("error creating indexes: %w", err)
	}

	// Initialize default settings
	if err := insertDefaultSettings(db); err != nil {
		return fmt.Errorf("error inserting default settings: %w", err)
	}

	return nil
}

func performMigrations(db *sql.DB) error {
	// Migrate 'feeds' table
	if err := migrateFeedsTable(db); err != nil {
		return err
	}

	// Migrate 'settings' table
	if err := migrateSettingsTable(db); err != nil {
		return err
	}

	return nil
}

func migrateFeedsTable(db *sql.DB) error {
	expectedColumns := []struct {
		name         string
		columnType   string
		defaultValue string
		hasDefault   bool // Indicates if the column should have a default value
	}{
		{"status", "TEXT", "'pending'", true},
		{"error_count", "INTEGER", "0", true},
		{"last_error", "TEXT", "NULL", true},
		{"last_fetched", "TIMESTAMP", "NULL", true},
		{"updated_at", "TIMESTAMP", "", false},          // No default value for 'updated_at'
		{"title_manually_edited", "BOOLEAN", "0", true}, // Track if title was manually edited
	}

	for _, col := range expectedColumns {
		exists, err := columnExists(db, "feeds", col.name)
		if err != nil {
			return err
		}

		if !exists {
			var alterStmt string
			if col.hasDefault && col.defaultValue != "" {
				// Use DEFAULT clause for constant values
				alterStmt = fmt.Sprintf(
					"ALTER TABLE feeds ADD COLUMN %s %s DEFAULT %s",
					col.name, col.columnType, col.defaultValue,
				)
			} else {
				// Add column without DEFAULT clause
				alterStmt = fmt.Sprintf(
					"ALTER TABLE feeds ADD COLUMN %s %s",
					col.name, col.columnType,
				)
			}

			if _, err := db.Exec(alterStmt); err != nil {
				return fmt.Errorf("error adding column '%s' to 'feeds' table: %w", col.name, err)
			}

			// Handle special cases after adding the column
			switch col.name {
			case "updated_at":
				// Update existing rows to set 'updated_at' to the current timestamp
				if _, err := db.Exec("UPDATE feeds SET updated_at = CURRENT_TIMESTAMP"); err != nil {
					return fmt.Errorf("error setting 'updated_at' for existing rows: %w", err)
				}

				// Create a trigger to update 'updated_at' on future updates
				triggerStmt := `
                CREATE TRIGGER IF NOT EXISTS feeds_updated_at_trigger
                AFTER UPDATE ON feeds
                FOR EACH ROW
                BEGIN
                    UPDATE feeds SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
                END;`
				if _, err := db.Exec(triggerStmt); err != nil {
					return fmt.Errorf("error creating trigger for 'updated_at': %w", err)
				}
			}
		}
	}

	return nil
}

func columnExists(db *sql.DB, tableName, columnName string) (bool, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s);", tableName)
	rows, err := db.Query(query)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt_value sql.NullString
		var pk int

		err = rows.Scan(&cid, &name, &ctype, &notnull, &dflt_value, &pk)
		if err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}

	return false, nil
}

func insertDefaultSettings(db *sql.DB) error {
	defaultSettings := map[string]string{
		"site_title":          "infoscope_",
		"max_posts":           "100",
		"update_interval":     "900",
		"header_link_text":    "",
		"header_link_url":     "",
		"footer_link_text":    "",
		"footer_link_url":     "",
		"footer_image_url":    "",
		"footer_image_height": "",
		"tracking_code":       "",
		"timezone":            "UTC",
		"favicon_url":         "favicon.ico",
		"meta_description":    "A minimalist RSS river reader",
		"meta_image_url":      "",
		"site_url":            "",
		"show_blog_name":      "false",
		"show_body_text":      "false",
		"body_text_length":    "200",
		// Themes
		"theme":        "terminal",
		"public_theme": "terminal",
		"admin_theme":  "terminal",
		// Public theme selection
		"allow_public_theme_selection": "false",
		"public_available_themes":      "aurora,latex,prose,sage,terminal",
		// Auto-backup defaults
		"backup_enabled":        "false",
		"backup_interval_hours": "24",
		"backup_retention_days": "30",
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction: %w", err)
	}
	defer tx.Rollback()

	// Check if settings table is empty
	var count int
	err = tx.QueryRow("SELECT COUNT(*) FROM settings").Scan(&count)
	if err != nil {
		return fmt.Errorf("error checking settings count: %w", err)
	}

	if count == 0 {
		// Insert default settings
		stmt, err := tx.Prepare("INSERT INTO settings (key, value) VALUES (?, ?)")
		if err != nil {
			return fmt.Errorf("error preparing statement: %w", err)
		}
		defer stmt.Close()

		for key, value := range defaultSettings {
			_, err = stmt.Exec(key, value)
			if err != nil {
				return fmt.Errorf("error inserting default setting %s: %w", key, err)
			}
		}
	} else {
		// Update existing settings with new defaults if they don't exist
		stmt, err := tx.Prepare(`INSERT INTO settings (key, value) 
            SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = ?)`)
		if err != nil {
			return fmt.Errorf("error preparing update statement: %w", err)
		}
		defer stmt.Close()

		for key, value := range defaultSettings {
			_, err = stmt.Exec(key, value, key)
			if err != nil {
				return fmt.Errorf("error updating setting %s: %w", key, err)
			}
		}
	}

	return tx.Commit()
}

func migrateSettingsTable(db *sql.DB) error {
	expectedColumns := []struct {
		name         string
		columnType   string
		defaultValue string
		hasDefault   bool // Indicates if the column should have a default value
	}{
		{"type", "TEXT", "'string'", true}, // Assuming default type is 'string'
	}

	for _, col := range expectedColumns {
		exists, err := columnExists(db, "settings", col.name)
		if err != nil {
			return err
		}

		if !exists {
			var alterStmt string
			if col.hasDefault && col.defaultValue != "" {
				// Use DEFAULT clause for constant values
				alterStmt = fmt.Sprintf(
					"ALTER TABLE settings ADD COLUMN %s %s DEFAULT %s",
					col.name, col.columnType, col.defaultValue,
				)
			} else {
				// Add column without DEFAULT clause
				alterStmt = fmt.Sprintf(
					"ALTER TABLE settings ADD COLUMN %s %s",
					col.name, col.columnType,
				)
			}

			if _, err := db.Exec(alterStmt); err != nil {
				return fmt.Errorf("error adding column '%s' to 'settings' table: %w", col.name, err)
			}

			// Optionally, update existing rows to set default values
			if col.hasDefault && col.defaultValue != "" {
				updateStmt := fmt.Sprintf(
					"UPDATE settings SET %s = %s WHERE %s IS NULL",
					col.name, col.defaultValue, col.name,
				)
				if _, err := db.Exec(updateStmt); err != nil {
					return fmt.Errorf("error setting default value for '%s' in 'settings' table: %w", col.name, err)
				}
			}
		}
	}

	return nil
}

// migrateFilterTables ensures all filter-related tables exist with proper triggers
func migrateFilterTables(db *sql.DB) error {
	// Add new columns to entry_filters if they don't exist
	targetTypeExists, err := columnExists(db, "entry_filters", "target_type")
	if err != nil {
		return fmt.Errorf("error checking target_type column: %w", err)
	}
	if !targetTypeExists {
		_, err := db.Exec("ALTER TABLE entry_filters ADD COLUMN target_type TEXT NOT NULL DEFAULT 'title' CHECK(target_type IN ('title', 'content', 'feed_tags', 'feed_category'))")
		if err != nil {
			return fmt.Errorf("error adding target_type column: %w", err)
		}
	}

	// Add new columns to filter_groups if they don't exist
	categoryExists, err := columnExists(db, "filter_groups", "apply_to_category")
	if err != nil {
		return fmt.Errorf("error checking apply_to_category column: %w", err)
	}
	if !categoryExists {
		_, err := db.Exec("ALTER TABLE filter_groups ADD COLUMN apply_to_category TEXT")
		if err != nil {
			return fmt.Errorf("error adding apply_to_category column: %w", err)
		}
	}

	// Create triggers for updated_at columns on filter tables
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS entry_filters_updated_at_trigger
		AFTER UPDATE ON entry_filters
		FOR EACH ROW
		BEGIN
			UPDATE entry_filters SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;`,

		`CREATE TRIGGER IF NOT EXISTS filter_groups_updated_at_trigger
		AFTER UPDATE ON filter_groups
		FOR EACH ROW
		BEGIN
			UPDATE filter_groups SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END;`,
	}

	for _, trigger := range triggers {
		if _, err := db.Exec(trigger); err != nil {
			return fmt.Errorf("error creating filter trigger: %w", err)
		}
	}

	return nil
}

// migrateTaxonomyTables adds category column to feeds table and ensures tag tables exist
func migrateTaxonomyTables(db *sql.DB) error {
	// Add category column to feeds table if it doesn't exist
	exists, err := columnExists(db, "feeds", "category")
	if err != nil {
		return fmt.Errorf("error checking category column: %w", err)
	}

	if !exists {
		_, err := db.Exec("ALTER TABLE feeds ADD COLUMN category TEXT")
		if err != nil {
			return fmt.Errorf("error adding category column: %w", err)
		}
	}

	return nil
}
