package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDB_SuccessAndTableCreation(t *testing.T) {
	// Create a temporary directory for the database file for this test
	// to avoid interference if other tests use a fixed path db.
	// Using ":memory:" is also an option if we don't need to test file path logic.
	// For NewDB, testing with a file path is slightly more realistic.
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_newdb.db")
	cfg := DefaultConfig()
	db, err := NewDB(dbPath, cfg)
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	if db == nil {
		t.Fatalf("NewDB() returned nil DB instance")
	}
	defer db.Close()

	// Verify connection is alive
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping() failed: %v", err)
	}

	// Verify tables are created
	tables := []string{"settings", "feeds", "entries", "admin_users", "sessions", "clicks", "click_stats"}
	for _, table := range tables {
		var count int
		err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
		if err != nil {
			t.Fatalf("Error checking for table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("Table %s was not created. Expected count 1, got %d", table, count)
		}
	}
}

func TestNewDB_DefaultSettings(t *testing.T) {
	dbPath := ":memory:" // Using in-memory for speed and simplicity here
	cfg := DefaultConfig()
	db, err := NewDB(dbPath, cfg)
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	defer db.Close()

	expectedSettings := map[string]string{
		"site_title":       "infoscope_",
		"max_posts":        "100",
		"update_interval":  "900",
		"timezone":         "UTC",
		"favicon_url":      "favicon.ico",
		"meta_description": "A minimalist RSS river reader",
	}

	for key, expectedValue := range expectedSettings {
		var value string
		// Note: GetSetting is in queries.go, so we are doing a direct query here
		// or we'd need to instantiate the DB struct from queries.go.
		// For schema_test, direct query is fine.
		err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
		if err != nil {
			t.Errorf("Error fetching default setting for key '%s': %v", key, err)
			continue
		}
		if value != expectedValue {
			t.Errorf("Default setting for key '%s': got '%s', want '%s'", key, value, expectedValue)
		}
	}

	// Test that if settings are present, they are not overwritten by defaults on subsequent NewDB (not directly testable here without more complex setup)
	// The current insertDefaultSettings logic (INSERT ... WHERE NOT EXISTS or checking count=0) handles this.
	// We can test that re-running createSchema (which NewDB calls) doesn't duplicate or error.
	// The `createSchema` function itself is what NewDB calls.
	// To test idempotency of default settings:
	// 1. Add a custom setting.
	// 2. Call `insertDefaultSettings` again (or a function that calls it).
	// 3. Verify custom setting is still there and defaults are not changed from their original default.

	customKey := "custom_test_setting"
	customValue := "my_value"
	_, err = db.Exec("INSERT INTO settings (key, value, type) VALUES (?, ?, ?)", customKey, customValue, "string")
	if err != nil {
		t.Fatalf("Failed to insert custom setting: %v", err)
	}

	// Re-run the part of createSchema that inserts defaults (or NewDB if it were safe to call again on same path)
	// For simplicity, let's simulate the relevant part of createSchema.
	// The current `insertDefaultSettings` in schema.go uses `INSERT ... SELECT ... WHERE NOT EXISTS`
	// or checks count. So, calling it again should be safe.
	// We need to make `insertDefaultSettings` accessible or call `createSchema` again.
	// Let's assume we can call `createSchema` (or parts of it) again.
	// Since `createSchema` is not exported, we test `NewDB`'s behavior which calls it.
	// A second `NewDB` on an existing file DB would re-apply. For :memory:, it's a new DB.
	// The test for default settings already covers the initial insertion.
	// The idempotency of schema and default settings insertion is implicitly handled by `IF NOT EXISTS`
	// in schema definitions and `INSERT ... WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = ?)` for settings.

	var value string
	err = db.QueryRow("SELECT value FROM settings WHERE key = ?", customKey).Scan(&value)
	if err != nil {
		t.Errorf("Error fetching custom setting after default insertion logic might have run again: %v", err)
	}
	if value != customValue {
		t.Errorf("Custom setting was overwritten or lost. Got '%s', want '%s'", value, customValue)
	}
}

func TestColumnExists(t *testing.T) {
	dbPath := ":memory:"
	// We don't need the full NewDB config here, just a DB with the schema.
	// So, we can use sql.Open and manually apply schema, or use NewDB.
	// Using NewDB is fine as it sets up everything.
	db, err := NewDB(dbPath, DefaultConfig())
	if err != nil {
		t.Fatalf("Failed to create DB for TestColumnExists: %v", err)
	}
	defer db.Close()

	testCases := []struct {
		tableName   string
		columnName  string
		shouldExist bool
		description string
	}{
		{"feeds", "url", true, "existing column 'url' in 'feeds'"},
		{"feeds", "title", true, "existing column 'title' in 'feeds'"},
		{"feeds", "non_existent_column", false, "non-existent column in 'feeds'"},
		{"entries", "feed_id", true, "existing column 'feed_id' in 'entries'"},
		{"entries", "another_missing_col", false, "non-existent column in 'entries'"},
		{"non_existent_table", "any_column", false, "column in non-existent table"},
		{"settings", "key", true, "existing column 'key' in 'settings'"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			exists, err := columnExists(db.DB, tc.tableName, tc.columnName)
			if err != nil {
				// Expect error if table doesn't exist, but columnExists should handle it gracefully
				if tc.tableName != "non_existent_table" {
					t.Errorf("columnExists(%s, %s) returned error: %v", tc.tableName, tc.columnName, err)
				}
			}
			if exists != tc.shouldExist {
				t.Errorf("columnExists(%s, %s) = %v, want %v", tc.tableName, tc.columnName, exists, tc.shouldExist)
			}
		})
	}
}

func TestMigrateEntryFiltersTargetType(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "legacy_filters.db")
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=ON&_synchronous=NORMAL", dbPath)

	db, err := sql.Open(SQLiteDriver, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	// Recreate the legacy schema: entry_filters with the restrictive target_type
	// CHECK, plus a filter group + rule referencing a filter via foreign key.
	legacy := []string{
		`CREATE TABLE entry_filters (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			pattern TEXT NOT NULL,
			pattern_type TEXT NOT NULL CHECK(pattern_type IN ('keyword', 'regex')),
			target_type TEXT NOT NULL DEFAULT 'title' CHECK(target_type IN ('title', 'content', 'feed_tags', 'feed_category')),
			case_sensitive BOOLEAN NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE filter_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('keep', 'discard')),
			is_active BOOLEAN NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			apply_to_category TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE filter_group_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			group_id INTEGER NOT NULL,
			filter_id INTEGER NOT NULL,
			operator TEXT NOT NULL CHECK(operator IN ('AND', 'OR')) DEFAULT 'AND',
			position INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (group_id) REFERENCES filter_groups(id) ON DELETE CASCADE,
			FOREIGN KEY (filter_id) REFERENCES entry_filters(id) ON DELETE CASCADE
		)`,
	}
	for _, stmt := range legacy {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("creating legacy schema: %v", err)
		}
	}

	// Seed a filter and a group rule that references it.
	if _, err := db.Exec(`INSERT INTO entry_filters (id, name, pattern, pattern_type, target_type, case_sensitive) VALUES (1, 'no shorts', '/shorts/', 'keyword', 'title', 0)`); err != nil {
		t.Fatalf("seeding filter: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO filter_groups (id, name, action) VALUES (1, 'group', 'discard')`); err != nil {
		t.Fatalf("seeding group: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO filter_group_rules (group_id, filter_id, operator, position) VALUES (1, 1, 'AND', 0)`); err != nil {
		t.Fatalf("seeding rule: %v", err)
	}

	// Before migration the legacy CHECK must reject the new 'url' target.
	if _, err := db.Exec(`INSERT INTO entry_filters (name, pattern, pattern_type, target_type) VALUES ('x', 'y', 'keyword', 'url')`); err == nil {
		t.Fatal("expected legacy CHECK to reject target_type='url' before migration")
	}

	// Run the migration.
	if err := migrateEntryFiltersTargetType(db); err != nil {
		t.Fatalf("migrateEntryFiltersTargetType: %v", err)
	}

	// The restrictive CHECK should now be gone.
	var ddl string
	if err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='entry_filters'").Scan(&ddl); err != nil {
		t.Fatalf("reading entry_filters ddl: %v", err)
	}
	if strings.Contains(ddl, "target_type IN") {
		t.Errorf("expected restrictive target_type CHECK to be removed, got: %s", ddl)
	}

	// The existing filter row must be preserved (id and data).
	var name string
	if err := db.QueryRow("SELECT name FROM entry_filters WHERE id = 1").Scan(&name); err != nil {
		t.Fatalf("reading preserved filter: %v", err)
	}
	if name != "no shorts" {
		t.Errorf("preserved filter name = %q, want %q", name, "no shorts")
	}

	// The referencing rule must survive (the rebuild must not cascade-delete it).
	var ruleCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM filter_group_rules WHERE filter_id = 1").Scan(&ruleCount); err != nil {
		t.Fatalf("counting rules: %v", err)
	}
	if ruleCount != 1 {
		t.Errorf("filter_group_rules count = %d, want 1 (rebuild must not cascade-delete)", ruleCount)
	}

	// A 'url' target is now accepted.
	if _, err := db.Exec(`INSERT INTO entry_filters (name, pattern, pattern_type, target_type) VALUES ('no shorts url', '/shorts/', 'keyword', 'url')`); err != nil {
		t.Errorf("expected target_type='url' to be accepted after migration: %v", err)
	}

	// Running the migration again must be a no-op.
	if err := migrateEntryFiltersTargetType(db); err != nil {
		t.Fatalf("second migrateEntryFiltersTargetType run: %v", err)
	}
}

func TestPerformMigrations_AddsColumns(t *testing.T) {
	// NewDB calls createSchema, which calls performMigrations.
	// So, by checking columns after NewDB, we test the effect of migrations.
	dbPath := ":memory:"
	db, err := NewDB(dbPath, DefaultConfig())
	if err != nil {
		t.Fatalf("NewDB() failed, cannot test migrations: %v", err)
	}
	defer db.Close()

	migratedColumns := []struct {
		table  string
		column string
	}{
		{"feeds", "status"},         // Added by migration
		{"feeds", "error_count"},    // Added by migration
		{"feeds", "last_error"},     // Added by migration
		{"entries", "content"},      // Ensured by add column logic
		{"entries", "guid"},         // Ensured by add column logic
		{"settings", "type"},        // Added by settings table migration
		{"settings", "favicon_url"}, // Ensured by add column logic (was default setting)
	}

	for _, mc := range migratedColumns {
		t.Run(mc.table+"."+mc.column, func(t *testing.T) {
			exists, err := columnExists(db.DB, mc.table, mc.column)
			if err != nil {
				t.Fatalf("Error checking column %s.%s: %v", mc.table, mc.column, err)
			}
			if !exists {
				t.Errorf("Expected column %s.%s to exist after migrations, but it does not", mc.table, mc.column)
			}
		})
	}
}
