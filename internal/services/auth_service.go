package services

import (
	"database/sql"
	"directoryCommunityWebsite/internal/models"
)

// AuthService handles authentication business logic
type AuthService struct {
	db *sql.DB
}

// NewAuthService creates a new authentication service
func NewAuthService(db *sql.DB) *AuthService {
	return &AuthService{db: db}
}

// GetUserType returns the user type for a given email and directory
func (s *AuthService) GetUserType(email, directoryID string) (string, error) {
	// Implementation would check super admin, directory owner, and moderator status
	// This is a placeholder for the business logic
	return models.UserTypeUser, nil
}

// IsAuthenticated checks if a user is authenticated
func (s *AuthService) IsAuthenticated(sessionData *models.SessionData) bool {
	return sessionData != nil && sessionData.IsValid()
}

// ValidateSession validates a session
func (s *AuthService) ValidateSession(sessionData *models.SessionData) error {
	if sessionData == nil {
		return ErrInvalidSession
	}
	
	if !sessionData.IsValid() {
		return ErrExpiredSession
	}
	
	return nil
}

// Error definitions
var (
	ErrInvalidSession = NewError("invalid session")
	ErrExpiredSession = NewError("session expired")
)

// Error represents a service error
type Error struct {
	message string
}

func NewError(message string) *Error {
	return &Error{message: message}
}

func (e *Error) Error() string {
	return e.message
}