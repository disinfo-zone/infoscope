package auth

import (
	"database/sql"
	"testing"
	"time"

	"infoscope/internal/database" // Assuming schema is accessible here

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// setupTestDBForAuth initializes an in-memory SQLite database for auth tests.
func setupTestDBForAuth(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}

	// Apply the schema
	_, err = db.Exec(database.Schema)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to apply schema: %v", err)
	}
	_, err = db.Exec(database.Indexes) // Apply indexes as well
	if err != nil {
		db.Close()
		t.Fatalf("Failed to apply indexes: %v", err)
	}

	_, err = db.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		db.Close()
		t.Fatalf("Failed to enable foreign keys: %v", err)
	}
	return db
}

func TestCleanExpiredSessions(t *testing.T) {
	db := setupTestDBForAuth(t)
	defer db.Close()

	// Pre-requisite: Need a user to associate sessions with
	_, err := db.Exec("INSERT INTO admin_users (username, password_hash) VALUES (?, ?)", "testuser", "somehash")
	if err != nil {
		t.Fatalf("Failed to create dummy user: %v", err)
	}
	var userID int64
	err = db.QueryRow("SELECT id FROM admin_users WHERE username = ?", "testuser").Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to get dummy user ID: %v", err)
	}

	sessions := []struct {
		id        string
		userID    int64
		createdAt time.Time
		expiresAt time.Time
		isExpired bool
	}{
		{"valid_session_1", userID, time.Now(), time.Now().Add(1 * time.Hour), false},
		{"expired_session_1", userID, time.Now().Add(-2 * time.Hour), time.Now().Add(-1 * time.Hour), true},
		{"valid_session_2", userID, time.Now(), time.Now().Add(24 * time.Hour), false},
		{"expired_session_2", userID, time.Now().Add(-48 * time.Hour), time.Now().Add(-24 * time.Hour), true},
		{"boundary_expired", userID, time.Now().Add(-1 * time.Second), time.Now().Add(-1 * time.Millisecond), true},
	}

	stmt, err := db.Prepare("INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)")
	if err != nil {
		t.Fatalf("Failed to prepare session insert statement: %v", err)
	}
	defer stmt.Close()

	for _, s := range sessions {
		_, err := stmt.Exec(s.id, s.userID, s.createdAt, s.expiresAt)
		if err != nil {
			t.Fatalf("Failed to insert session %s: %v", s.id, err)
		}
	}

	// Call CleanExpiredSessions
	err = CleanExpiredSessions(db)
	if err != nil {
		t.Fatalf("CleanExpiredSessions failed: %v", err)
	}

	// Verify that only non-expired sessions remain
	rows, err := db.Query("SELECT id FROM sessions")
	if err != nil {
		t.Fatalf("Failed to query sessions after cleanup: %v", err)
	}
	defer rows.Close()

	remainingSessions := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("Failed to scan session ID: %v", err)
		}
		remainingSessions[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Error iterating over remaining sessions: %v", err)
	}

	expectedRemainingCount := 0
	for _, s := range sessions {
		if !s.isExpired {
			expectedRemainingCount++
			if !remainingSessions[s.id] {
				t.Errorf("Expected session %s to remain, but it was deleted", s.id)
			}
		} else {
			if remainingSessions[s.id] {
				t.Errorf("Expected expired session %s to be deleted, but it remained", s.id)
			}
		}
	}

	if len(remainingSessions) != expectedRemainingCount {
		t.Errorf("Expected %d sessions to remain, but got %d", expectedRemainingCount, len(remainingSessions))
	}
}

// IsExpired is a method on the Session struct, which is simple enough that it's often
// implicitly tested by other session logic (like ValidateSession).
// However, a direct test can be added for completeness if desired.
func TestSession_IsExpired(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		name      string
		session   Session
		isExpired bool
	}{
		{
			name:      "active session",
			session:   Session{ExpiresAt: now.Add(1 * time.Hour)},
			isExpired: false,
		},
		{
			name:      "expired session",
			session:   Session{ExpiresAt: now.Add(-1 * time.Hour)},
			isExpired: true,
		},
		{
			name:      "session expiring now",
			session:   Session{ExpiresAt: now},
			isExpired: true, // IsExpired uses Before, so 'now' is considered expired relative to 'now'
		},
		{
			name:      "session expiring just a moment ago",
			session:   Session{ExpiresAt: now.Add(-1 * time.Nanosecond)},
			isExpired: true,
		},
		{
			name:      "session expiring in a moment",
			session:   Session{ExpiresAt: now.Add(1 * time.Nanosecond)},
			isExpired: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.session.IsExpired(); got != tc.isExpired {
				t.Errorf("Session.IsExpired() for %s: got %v, want %v (ExpiresAt: %v, Now: %v)",
					tc.name, got, tc.isExpired, tc.session.ExpiresAt, now)
			}
		})
	}
}
