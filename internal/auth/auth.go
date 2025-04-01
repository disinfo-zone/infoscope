// internal/auth/auth.go
package auth

import (
	"database/sql"
	"errors"
	"time"
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

func (s *Session) IsExpired() bool {
	return s.ExpiresAt.Before(time.Now())
}
