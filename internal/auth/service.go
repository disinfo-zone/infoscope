// Save as: internal/auth/service.go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	maxLoginAttempts = 5
	lockoutDuration  = 15 * time.Minute
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

// Common weak passwords that should be rejected
var commonPasswords = map[string]bool{
	"password":     true,
	"password123":  true,
	"password123!": true,
	"admin":        true,
	"admin123":     true,
	"123456":       true,
	"12345678":     true,
	"qwerty":       true,
	"qwerty123":    true,
	"letmein":      true,
	"welcome":      true,
	"welcome123":   true,
	"monkey":       true,
	"dragon":       true,
	"princess":     true,
	"sunshine":     true,
	"football":     true,
	"baseball":     true,
	"superman":     true,
	"iloveyou":     true,
	"trustno1":     true,
	"abc123":       true,
	"password1":    true,
	"changeme":     true,
	"master":       true,
	"hello":        true,
	"guest":        true,
	"test":         true,
	"test123":      true,
	"admin1":       true,
	"root":         true,
	"user":         true,
	"user123":      true,
}

// isCommonPassword checks if the password is in the list of common weak passwords
func isCommonPassword(password string) bool {
	return commonPasswords[strings.ToLower(password)]
}

// calculateEntropy estimates password entropy based on character set diversity and length
func calculateEntropy(password string) float64 {
	if len(password) == 0 {
		return 0
	}

	var charSetSize float64 = 0

	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		if unicode.IsLower(char) && !hasLower {
			hasLower = true
			charSetSize += 26
		}
		if unicode.IsUpper(char) && !hasUpper {
			hasUpper = true
			charSetSize += 26
		}
		if unicode.IsDigit(char) && !hasDigit {
			hasDigit = true
			charSetSize += 10
		}
		if (unicode.IsPunct(char) || unicode.IsSymbol(char)) && !hasSpecial {
			hasSpecial = true
			charSetSize += 32 // Common special characters
		}
	}

	if charSetSize == 0 {
		return 0
	}

	// Entropy = log2(charSetSize^length)
	return float64(len(password)) * math.Log2(charSetSize)
}

// validatePasswordStrength checks if the password meets the defined criteria.
func (s *Service) validatePasswordStrength(password string) error {
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters long")
	}

	hasUpper := false
	hasLower := false
	hasDigit := false
	hasSpecial := false

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return errors.New("password must include at least one uppercase letter")
	}
	if !hasLower {
		return errors.New("password must include at least one lowercase letter")
	}
	if !hasDigit {
		return errors.New("password must include at least one digit")
	}
	if !hasSpecial {
		return errors.New("password must include at least one special character")
	}

	// Check against common passwords
	if isCommonPassword(password) {
		return errors.New("password is too common, please choose a different one")
	}

	// Add entropy check
	if calculateEntropy(password) < 50 {
		return errors.New("password is too predictable, please use a more complex combination")
	}

	return nil
}

// CreateUser creates a new admin user with a hashed password
func (s *Service) CreateUser(db *sql.DB, username, password string) error {
	// Validate password strength
	if err := s.validatePasswordStrength(password); err != nil {
		return err
	}

	// Convert username to lowercase for case-insensitivity
	lowerUsername := strings.ToLower(username)

	// Hash password with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Insert user into database
	_, err = db.Exec(
		"INSERT INTO admin_users (username, password_hash) VALUES (?, ?)",
		lowerUsername, string(hash), // Use lowerUsername
	)
	return err
}

func (s *Service) Authenticate(db *sql.DB, username, password string) (*Session, error) {
	var user User // Changed from anonymous struct to User type
	// Convert username to lowercase for case-insensitive lookup
	lowerUsername := strings.ToLower(username)

	err := db.QueryRow(
		"SELECT id, password_hash, login_attempts, locked_until FROM admin_users WHERE username = ?",
		lowerUsername,
	).Scan(&user.ID, &user.PasswordHash, &user.LoginAttempts, &user.LockedUntil)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// Check if account is locked
	if user.LockedUntil.Valid && user.LockedUntil.Time.After(time.Now()) {
		return nil, ErrAccountLocked
	}

	// Compare password
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		// Password incorrect, increment attempts and potentially lock
		user.LoginAttempts++
		var newLockedUntil sql.NullTime
		if user.LoginAttempts >= maxLoginAttempts {
			newLockedUntil.Time = time.Now().Add(lockoutDuration)
			newLockedUntil.Valid = true
		}

		_, updateErr := db.Exec(
			"UPDATE admin_users SET login_attempts = ?, locked_until = ? WHERE id = ?",
			user.LoginAttempts, newLockedUntil, user.ID,
		)
		if updateErr != nil {
			// Log this error, but return invalid credentials to the user
			log.Printf("Error updating login attempts for user %d: %v", user.ID, updateErr)
		}
		return nil, ErrInvalidCredentials
	}

	// Password is correct, reset attempts and unlock if necessary
	if user.LoginAttempts > 0 || (user.LockedUntil.Valid && user.LockedUntil.Time.After(time.Now())) {
		_, resetErr := db.Exec(
			"UPDATE admin_users SET login_attempts = 0, locked_until = NULL WHERE id = ?",
			user.ID,
		)
		if resetErr != nil {
			// Log this error, but proceed with login
			log.Printf("Error resetting login attempts for user %d: %v", user.ID, resetErr)
		}
	}

	// Create new session
	session := &Session{
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour), // Consider making session duration configurable
	}

	// Generate random session ID
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}
	session.ID = sessionID

	// Save session to database
	_, err = db.Exec(
		"INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		session.ID, session.UserID, session.CreatedAt, session.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// GetUserByID retrieves a user by their ID
func (s *Service) GetUserByID(db *sql.DB, userID int64) (*User, error) {
	var user User
	// Also fetch login_attempts and locked_until if they are relevant for GetUserByID,
	// though typically they are more for the authentication flow.
	// For now, keeping it as is, assuming GetUserByID is for general user data retrieval.
	err := db.QueryRow(
		"SELECT id, username, password_hash, created_at, login_attempts, locked_until FROM admin_users WHERE id = ?",
		userID,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt, &user.LoginAttempts, &user.LockedUntil)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	return &user, nil
}

// UpdatePassword updates the password for a given user ID
func (s *Service) UpdatePassword(db *sql.DB, userID int64, newPassword string) error {
	// Validate new password strength
	if err := s.validatePasswordStrength(newPassword); err != nil {
		return err
	}

	// Hash the new password
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Update the password hash in the database using a transaction
	tx, err := db.BeginTx(context.Background(), nil) // Start transaction
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if commit doesn't happen

	result, err := tx.ExecContext(context.Background(), // Use transaction
		"UPDATE admin_users SET password_hash = ? WHERE id = ?",
		string(hash), userID,
	)
	if err != nil {
		return fmt.Errorf("failed to execute update: %w", err) // Return specific error
	}

	// Check if any row was actually updated
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("could not verify password update: %w", err)
	}
	if rowsAffected == 0 {
		return errors.New("password update failed: user not found or no changes made")
	}

	log.Printf("[AuthService] Attempting to commit password update for user %d", userID) // Log before commit

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		log.Printf("[AuthService] FAILED to commit password update for user %d: %v", userID, err) // Log commit error
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("[AuthService] Successfully committed password update for user %d", userID) // Log after successful commit

	return nil // Return nil only if exec succeeded, rows were affected, and commit succeeded
}

func (s *Service) ValidateSession(db *sql.DB, sessionID string) (*Session, error) {
	var session Session
	err := db.QueryRow(
		`SELECT id, user_id, created_at, expires_at 
         FROM sessions 
         WHERE id = ? AND expires_at > ?`,
		sessionID, time.Now(),
	).Scan(&session.ID, &session.UserID, &session.CreatedAt, &session.ExpiresAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	return &session, nil
}

func (s *Service) InvalidateSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}
