package auth

import (
	"database/sql"
	"testing"

	"infoscope/internal/database"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB initializes an in-memory SQLite database and applies the schema.
func setupTestDB(t *testing.T) *sql.DB {
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
	_, err = db.Exec(database.Indexes)
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

func TestCreateUser_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "SecurePass123!"

	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Verify user exists in DB
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", username).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query created user: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 user, got %d", count)
	}
}

func TestCreateUser_LowercaseUsername(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "TestUser"
	password := "SecurePass123!"

	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Verify username is stored in lowercase
	var storedUsername string
	err = db.QueryRow("SELECT username FROM admin_users WHERE username = ?", "testuser").Scan(&storedUsername)
	if err != nil {
		t.Fatalf("Failed to query created user: %v", err)
	}
	if storedUsername != "testuser" {
		t.Errorf("Expected lowercase username 'testuser', got '%s'", storedUsername)
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "SecurePass123!"

	// Create first user
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("First CreateUser failed: %v", err)
	}

	// Try to create duplicate
	err = service.CreateUser(db, username, password)
	if err == nil {
		t.Fatal("Expected error for duplicate username, got nil")
	}
}

func TestCreateUser_WeakPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"

	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "too_short",
			password: "Short1!",
			wantErr:  true,
		},
		{
			name:     "no_uppercase",
			password: "securepass123!",
			wantErr:  true,
		},
		{
			name:     "no_lowercase",
			password: "SECUREPASS123!",
			wantErr:  true,
		},
		{
			name:     "no_digit",
			password: "SecurePassword!",
			wantErr:  true,
		},
		{
			name:     "no_special",
			password: "SecurePassword123",
			wantErr:  true,
		},
		{
			name:     "common_password",
			password: "Password123!",
			wantErr:  true,
		},
		{
			name:     "valid_password",
			password: "MySecureP@ssw0rd!",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.CreateUser(db, username+"_"+tt.name, tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateUser() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthenticate_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "SecurePass123!"

	// Create user first
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	// Authenticate
	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if session == nil || session.ID == "" {
		t.Error("Expected valid session with non-empty ID")
	}

	// Verify session in DB
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", session.ID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query session: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 session, got %d", count)
	}
}

func TestAuthenticate_IncorrectPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "SecurePass123!"
	wrongPassword := "WrongPassword123!"

	// Create user first
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Try to authenticate with wrong password
	_, err = service.Authenticate(db, username, wrongPassword)
	if err == nil {
		t.Fatal("Expected error for incorrect password, got nil")
	}
}

func TestAuthenticate_NonExistentUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()

	_, err := service.Authenticate(db, "nonexistent", "password")
	if err == nil {
		t.Fatal("Expected error for non-existent user, got nil")
	}
}

func TestValidateSession_Service_Valid(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "SecurePass123!"
	// Create user and authenticate
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	} // Validate session
	validatedSession, err := service.ValidateSession(db, session.ID)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if validatedSession.ID != session.ID {
		t.Errorf("Expected session ID '%s', got '%s'", session.ID, validatedSession.ID)
	}
}

func TestValidateSession_Service_InvalidOrNonExistent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()

	_, err := service.ValidateSession(db, "invalid-session-id")
	if err == nil {
		t.Fatal("Expected error for invalid session, got nil")
	}
}

func TestInvalidateSession_Service(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "SecurePass123!"
	// Create user and authenticate
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	// Invalidate session
	err = service.InvalidateSession(db, session.ID)
	if err != nil {
		t.Fatalf("InvalidateSession failed: %v", err)
	}

	// Try to validate the invalidated session
	_, err = service.ValidateSession(db, session.ID)
	if err == nil {
		t.Fatal("Expected error for invalidated session, got nil")
	}
}
