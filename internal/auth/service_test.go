package auth

import (
	"database/sql"
	"os"
	"testing"
	"time"

	"infoscope/internal/database" // Assuming schema is accessible here

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// setupTestDB initializes an in-memory SQLite database and applies the schema.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper() // Marks this function as a test helper

	// Using a unique name for each in-memory DB to ensure isolation if tests run concurrently
	// or if a single test function needs multiple DBs (though less common).
	// Or, simply use ":memory:" if you ensure tests clean up or don't interfere.
	// For simplicity in this context, ":memory:" is fine as each test function will get its own.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory database: %v", err)
	}

	// Apply the schema
	// Assuming 'database.Schema' and 'database.Indexes' are exported constants
	// from your database package.
	// You might need to adjust this part based on how your schema is defined and applied.
	// For example, if you have a function like `database.InitializeSchema(db *sql.DB)`.
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

	// It's good practice to set PRAGMAs for foreign keys if your schema uses them,
	// especially for testing data integrity.
	_, err = db.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		db.Close()
		t.Fatalf("Failed to enable foreign keys: %v", err)
	}

	return db
}

// TestMain can be used for package-level setup/teardown, but for in-memory DBs
// that are recreated for each test, it might not be strictly necessary unless
// there's other global setup.
func TestMain(m *testing.M) {
	// Optional: Any package-level setup
	exitCode := m.Run()
	// Optional: Any package-level teardown
	os.Exit(exitCode)
}

func TestCreateUser_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "testuser"
	password := "Password123"

	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Verify user exists in DB
	var storedUsername string
	var storedPasswordHash string
	err = db.QueryRow("SELECT username, password_hash FROM admin_users WHERE username = ?", "testuser").Scan(&storedUsername, &storedPasswordHash)
	if err != nil {
		t.Fatalf("Failed to query user: %v", err)
	}

	if storedUsername != username { // Though we test lowercase separately, good to check
		t.Errorf("Expected username %s, got %s", username, storedUsername)
	}
	if len(storedPasswordHash) == 0 {
		t.Errorf("Password hash is empty")
	}
}

func TestCreateUser_LowercaseUsername(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "TestUserUPPER"
	password := "Password123"
	expectedUsername := "testuserupper"

	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", expectedUsername).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query user by lowercase username: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected user to be stored with lowercase username '%s', count was %d", expectedUsername, count)
	}

	err = db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", username).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query user by original username: %v", err)
	}
	if count != 1 { // service.go converts to lower before storing
		t.Errorf("Expected user to be found with original username '%s' due to lowercase conversion, count was %d", username, count)
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "duplicateuser"
	password := "Password123"

	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("First CreateUser failed: %v", err)
	}

	err = service.CreateUser(db, username, "OtherPassword123")
	if err == nil {
		t.Fatalf("Expected CreateUser to fail for duplicate username, but it succeeded")
	}
	// Note: The exact error type/message depends on DB driver and constraints.
	// For SQLite, a UNIQUE constraint violation usually returns a specific error.
	// We might need to check for `sqlite3.ErrorConstraintUnique` or a similar error.
	// For now, just checking for `err != nil` is a basic test.
}

func TestCreateUser_WeakPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	service := NewService()
	username := "weakpassuser"
	testCases := []struct {
		name     string
		password string
		wantErr  string
	}{
		{"too short", "short", "password must be at least 10 characters long"},
		{"no uppercase", "password123", "password must include at least one uppercase letter"},
		{"no lowercase", "PASSWORD123", "password must include at least one lowercase letter"},
		{"no digit", "PasswordAbc", "password must include at least one digit"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := service.CreateUser(db, username+tc.name, tc.password)
			if err == nil {
				t.Errorf("Expected error for password '%s', but got nil", tc.password)
			} else if err.Error() != tc.wantErr {
				t.Errorf("Expected error message '%s', but got '%s'", tc.wantErr, err.Error())
			}
		})
	}
}

// Placeholder for Authenticate tests
func TestAuthenticate_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "authuser"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("Failed to create user for auth test: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if session == nil {
		t.Fatalf("Authenticate returned nil session")
	}
	if session.UserID == 0 {
		t.Errorf("Session UserID is not set")
	}
	if session.ID == "" {
		t.Errorf("Session ID is not set")
	}
	if session.ExpiresAt.Before(time.Now()) {
		t.Errorf("Session expiration is not in the future")
	}

	// Verify session in DB
	var dbSessionID string
	err = db.QueryRow("SELECT id FROM sessions WHERE user_id = (SELECT id FROM admin_users WHERE username = ?)", username).Scan(&dbSessionID)
	if err != nil {
		t.Fatalf("Failed to query session from DB: %v", err)
	}
	if dbSessionID != session.ID {
		t.Errorf("Session ID in DB (%s) does not match returned session ID (%s)", dbSessionID, session.ID)
	}
}

func TestAuthenticate_IncorrectPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "authwrongpass"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	_, err = service.Authenticate(db, username, "WrongPassword123")
	if err == nil {
		t.Fatalf("Expected Authenticate to fail for incorrect password, but it succeeded")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("Expected error ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthenticate_NonExistentUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	_, err := service.Authenticate(db, "nonexistentuser", "Password123")
	if err == nil {
		t.Fatalf("Expected Authenticate to fail for non-existent user, but it succeeded")
	}
	if err != ErrInvalidCredentials {
		t.Errorf("Expected error ErrInvalidCredentials, got %v", err)
	}
}


func TestAuthenticate_AccountLockout(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "locktestuser"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("Failed to create user for lockout test: %v", err)
	}

	// N-1 failed attempts
	for i := 0; i < maxLoginAttempts-1; i++ {
		_, err = service.Authenticate(db, username, "WrongPassword")
		if err != ErrInvalidCredentials {
			t.Fatalf("Attempt %d: Expected ErrInvalidCredentials, got %v", i+1, err)
		}
	}
	
	// Check login_attempts before locking attempt
	var attemptsBeforeLock int
	err = db.QueryRow("SELECT login_attempts FROM admin_users WHERE username = ?", username).Scan(&attemptsBeforeLock)
	if err != nil {
		t.Fatalf("Failed to query login_attempts: %v", err)
	}
	if attemptsBeforeLock != maxLoginAttempts-1 {
		t.Fatalf("Expected %d login attempts before locking, got %d", maxLoginAttempts-1, attemptsBeforeLock)
	}


	// Nth failed attempt - should lock the account
	_, err = service.Authenticate(db, username, "WrongPasswordAgain")
	if err != ErrInvalidCredentials { // Still returns InvalidCredentials on the attempt that locks
		t.Fatalf("Nth attempt: Expected ErrInvalidCredentials, got %v", err)
	}
	
	var attemptsAfterLock int
	var lockedUntil sql.NullTime
	err = db.QueryRow("SELECT login_attempts, locked_until FROM admin_users WHERE username = ?", username).Scan(&attemptsAfterLock, &lockedUntil)
	if err != nil {
		t.Fatalf("Failed to query user after locking attempt: %v", err)
	}
	if attemptsAfterLock != maxLoginAttempts {
		t.Errorf("Expected login_attempts to be %d after lock, got %d", maxLoginAttempts, attemptsAfterLock)
	}
	if !lockedUntil.Valid || lockedUntil.Time.Before(time.Now().Add(lockoutDuration-time.Second*5))) { // allow small delta
		t.Errorf("Expected account to be locked for approx %v, locked_until: %v", lockoutDuration, lockedUntil)
	}


	// Attempt to authenticate against a locked account
	_, err = service.Authenticate(db, username, password)
	if err != ErrAccountLocked {
		t.Fatalf("Expected ErrAccountLocked for locked account, got %v", err)
	}

	// Test successful login resets attempts (need to bypass current lock for test or wait)
	// For testing, we can manually reset locked_until or advance time.
	// Here, let's manually reset locked_until to the past.
	_, err = db.Exec("UPDATE admin_users SET locked_until = ? WHERE username = ?", time.Now().Add(-1*time.Minute), username)
	if err != nil {
		t.Fatalf("Failed to manually unlock account for testing: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed after manual unlock: %v", err)
	}
	if session == nil {
		t.Fatalf("Authenticate returned nil session after manual unlock")
	}

	var finalAttempts int
	var finalLockedUntil sql.NullTime
	err = db.QueryRow("SELECT login_attempts, locked_until FROM admin_users WHERE username = ?", username).Scan(&finalAttempts, &finalLockedUntil)
	if err != nil {
		t.Fatalf("Failed to query user after successful login: %v", err)
	}
	if finalAttempts != 0 {
		t.Errorf("Expected login_attempts to be 0 after successful login, got %d", finalAttempts)
	}
	if finalLockedUntil.Valid { // Should be NULL
		t.Errorf("Expected locked_until to be NULL after successful login, got %v", finalLockedUntil.Time)
	}
}

func TestUpdatePassword_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "updatepassuser"
	oldPassword := "OldPassword123"
	newPassword := "NewPassword456"

	err := service.CreateUser(db, username, oldPassword)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	var user User
	err = db.QueryRow("SELECT id FROM admin_users WHERE username = ?", username).Scan(&user.ID)
	if err != nil {
		t.Fatalf("Failed to get user ID: %v", err)
	}

	err = service.UpdatePassword(db, user.ID, newPassword)
	if err != nil {
		t.Fatalf("UpdatePassword failed: %v", err)
	}

	// Try to authenticate with new password
	session, err := service.Authenticate(db, username, newPassword)
	if err != nil {
		t.Fatalf("Authenticate with new password failed: %v", err)
	}
	if session == nil {
		t.Fatalf("Authenticate with new password returned nil session")
	}

	// Try to authenticate with old password (should fail)
	_, err = service.Authenticate(db, username, oldPassword)
	if err != ErrInvalidCredentials {
		t.Fatalf("Authenticate with old password should have failed with ErrInvalidCredentials, got %v", err)
	}
}

func TestUpdatePassword_WeakPassword(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "updateweakpass"
	initialPassword := "StrongPassword123"
	err := service.CreateUser(db, username, initialPassword)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	var userID int64
	err = db.QueryRow("SELECT id FROM admin_users WHERE username = ?", username).Scan(&userID)
	if err != nil {
		t.Fatalf("Failed to query user ID: %v", err)
	}
	
	weakPassword := "weak"
	expectedErr := "password must be at least 10 characters long"
	err = service.UpdatePassword(db, userID, weakPassword)
	if err == nil {
		t.Fatalf("Expected error for weak password, but got nil")
	} else if err.Error() != expectedErr {
		t.Errorf("Expected error message '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestUpdatePassword_NonExistentUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	nonExistentUserID := int64(9999)
	newPassword := "ValidPassword123"

	err := service.UpdatePassword(db, nonExistentUserID, newPassword)
	if err == nil {
		t.Fatalf("Expected UpdatePassword to fail for non-existent user, but it succeeded")
	}
	// This error comes from the row counting in UpdatePassword
	expectedErrPrefix := "password update failed: user not found" 
	if err.Error()[:len(expectedErrPrefix)] != expectedErrPrefix {
		t.Errorf("Expected error message prefix '%s', got '%s'", expectedErrPrefix, err.Error())
	}
}


func TestValidateSession_Service_Valid(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "validsessionuser"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	validatedSession, err := service.ValidateSession(db, session.ID)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}
	if validatedSession == nil {
		t.Fatalf("ValidateSession returned nil session")
	}
	if validatedSession.ID != session.ID || validatedSession.UserID != session.UserID {
		t.Errorf("Validated session does not match original session")
	}
}

func TestValidateSession_Service_InvalidOrNonExistent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	_, err := service.ValidateSession(db, "nonexistentsessionid")
	if err == nil {
		t.Fatalf("Expected ValidateSession to fail for invalid ID, but it succeeded")
	}
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}
}

func TestValidateSession_Service_Expired(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "expiredsessionuser"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	// Manually expire the session in the database
	_, err = db.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?", time.Now().Add(-1*time.Hour), session.ID)
	if err != nil {
		t.Fatalf("Failed to manually expire session: %v", err)
	}

	_, err = service.ValidateSession(db, session.ID)
	if err == nil {
		t.Fatalf("Expected ValidateSession to fail for expired session, but it succeeded")
	}
	if err != ErrSessionNotFound { // The query specifically asks for expires_at > time.Now()
		t.Errorf("Expected ErrSessionNotFound for expired session, got %v", err)
	}
}

func TestInvalidateSession_Service(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "invalidatesessionuser"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	session, err := service.Authenticate(db, username, password)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	err = service.InvalidateSession(db, session.ID)
	if err != nil {
		t.Fatalf("InvalidateSession failed: %v", err)
	}

	// Try to validate the invalidated session
	_, err = service.ValidateSession(db, session.ID)
	if err == nil {
		t.Fatalf("Expected ValidateSession to fail after InvalidateSession, but it succeeded")
	}
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound after invalidation, got %v", err)
	}
}

// TODO: Add tests for GetUserByID if its functionality is critical beyond what Authenticate tests
// For now, GetUserByID is mostly used internally by Authenticate or similar flows,
// and its direct testing might be redundant if covered by those.
// If it were to be exposed or used independently more, direct tests would be more important.
// The prompt implies testing service methods, and GetUserByID is one, but its usage in tests
// is mostly as a helper or implicitly via other auth flows.
// For completeness, a simple test for GetUserByID:

func TestGetUserByID_Success(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	username := "getuserbyidtest"
	password := "Password123"
	err := service.CreateUser(db, username, password)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	var expectedUserID int64
	err = db.QueryRow("SELECT id FROM admin_users WHERE username = ?", username).Scan(&expectedUserID)
	if err != nil {
		t.Fatalf("Failed to get created user ID: %v", err)
	}
	
	user, err := service.GetUserByID(db, expectedUserID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if user == nil {
		t.Fatalf("GetUserByID returned nil user")
	}
	if user.ID != expectedUserID {
		t.Errorf("GetUserByID returned user with ID %d, expected %d", user.ID, expectedUserID)
	}
	if user.Username != username {
		t.Errorf("GetUserByID returned user with username %s, expected %s", user.Username, username)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	service := NewService()

	nonExistentID := int64(12345)
	_, err := service.GetUserByID(db, nonExistentID)
	if err == nil {
		t.Fatalf("Expected GetUserByID to fail for non-existent ID, but it succeeded")
	}
	// The error message is "user not found" as defined in GetUserByID
	if err.Error() != "user not found" {
		t.Errorf("Expected error 'user not found', got '%v'", err.Error())
	}
}

// Note: generateSessionID is not exported and is an internal detail, so it's not directly tested.
// Its correct functioning is implicitly tested by successful session creation and validation.
```
