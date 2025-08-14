// internal/auth/auth.go
package auth

import (
	"database/sql" // Added for sql.NullTime
	"errors"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionExpired     = errors.New("session expired")
	ErrAccountLocked      = errors.New("account locked, try again later") // Added for lockout
)

type User struct {
	ID            int64
	Username      string
	PasswordHash  string
	CreatedAt     time.Time
	LoginAttempts int          // Added for lockout
	LockedUntil   sql.NullTime // Added for lockout
}

type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CleanExpiredSessions removes all expired sessions
func CleanExpiredSessions(db *sql.DB) error {
	_, err := db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now())
	return err
}

func (s *Session) IsExpired() bool {
	now := time.Now()
	return s.ExpiresAt.Before(now) || s.ExpiresAt.Equal(now)
}
