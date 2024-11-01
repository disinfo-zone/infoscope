// internal/auth/auth.go
package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionExpired     = errors.New("session expired")
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CreateUser creates a new admin user with a hashed password
func CreateUser(db *sql.DB, username, password string) error {
	// Hash password with bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Insert user into database
	_, err = db.Exec(
		"INSERT INTO admin_users (username, password_hash) VALUES (?, ?)",
		username, string(hash),
	)
	return err
}

// Authenticate verifies username and password, returns a new session if successful
func Authenticate(db *sql.DB, username, password string) (*Session, error) {
	var user User
	err := db.QueryRow(
		"SELECT id, password_hash FROM admin_users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.PasswordHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash),
		[]byte(password),
	); err != nil {
		return nil, ErrInvalidCredentials
	}

	// Generate session
	return createSession(db, user.ID)
}

// createSession creates a new session for the user
func createSession(db *sql.DB, userID int64) (*Session, error) {
	// Generate random session ID
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	sessionID := base64.URLEncoding.EncodeToString(b)

	// Set session expiration
	expiresAt := time.Now().Add(24 * time.Hour)

	// Insert session into database
	_, err := db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		sessionID, userID, expiresAt,
	)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:        sessionID,
		UserID:    userID,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateSession checks if a session is valid and not expired
func ValidateSession(db *sql.DB, sessionID string) (*Session, error) {
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

// InvalidateSession removes a session from the database
func InvalidateSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

// CleanExpiredSessions removes all expired sessions
func CleanExpiredSessions(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now())
	return err
}
