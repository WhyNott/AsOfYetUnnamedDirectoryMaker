package models

import "time"

// User represents a user in the system
type User struct {
	Email       string    `json:"email"`
	UserType    string    `json:"user_type"`
	CreatedAt   time.Time `json:"created_at"`
	LastLoginAt time.Time `json:"last_login_at"`
}

// UserType constants
const (
	UserTypeSuperAdmin = "super_admin"
	UserTypeAdmin      = "admin"
	UserTypeModerator  = "moderator"
	UserTypeUser       = "user"
)

// SessionData represents session information
type SessionData struct {
	UserEmail     string    `json:"user_email"`
	CSRFToken     string    `json:"csrf_token"`
	Authenticated bool      `json:"authenticated"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// IsExpired checks if the session has expired
func (s *SessionData) IsExpired(maxAge int) bool {
	return time.Since(s.CreatedAt) > time.Duration(maxAge)*time.Second
}

// IsValid checks if the session is valid
func (s *SessionData) IsValid() bool {
	return s.Authenticated && !s.ExpiresAt.Before(time.Now())
}