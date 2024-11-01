// Save as: internal/auth/service.go
package auth

import (
	"database/sql"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Authenticate(db *sql.DB, username, password string) (*Session, error) {
	var user struct {
		id           int64
		passwordHash string
	}

	err := db.QueryRow(
		"SELECT id, password_hash FROM admin_users WHERE username = ?",
		username,
	).Scan(&user.id, &user.passwordHash)

	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(user.passwordHash),
		[]byte(password),
	); err != nil {
		return nil, ErrInvalidCredentials
	}

	// Create new session
	session := &Session{
		UserID:    user.id,
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
