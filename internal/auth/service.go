// Save as: internal/auth/service.go
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

// CreateUser creates a new admin user with a hashed password
func (s *Service) CreateUser(db *sql.DB, username, password string) error {
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
		"SELECT id, password_hash FROM admin_users WHERE username = ?",
		lowerUsername, // Use lowerUsername
	).Scan(&user.ID, &user.PasswordHash) // Scan into User struct fields

	if err != nil {
		if err == sql.ErrNoRows { // Check specifically for ErrNoRows
			return nil, ErrInvalidCredentials
		}
		// Log or return other errors appropriately
		return nil, err // Return the actual db error for other cases
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash), // Use field from User struct
		[]byte(password),
	); err != nil {
		return nil, ErrInvalidCredentials
	}

	// Create new session
	session := &Session{
		UserID:    user.ID, // Use field from User struct
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
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
	err := db.QueryRow(
		"SELECT id, username, password_hash, created_at FROM admin_users WHERE id = ?",
		userID,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found") // Or a more specific error
		}
		return nil, err
	}
	return &user, nil
}

// UpdatePassword updates the password for a given user ID
func (s *Service) UpdatePassword(db *sql.DB, userID int64, newPassword string) error {
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
