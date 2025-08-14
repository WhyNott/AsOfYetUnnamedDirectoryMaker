package utils

import (
	"net/http"
)

// Context keys - should match the ones in middleware.go
type contextKey string

const (
	UserEmailKey      contextKey = "user_email"
	CSRFTokenKey      contextKey = "csrf_token"
	AuthenticatedKey  contextKey = "authenticated"
	DirectoryIDKey    contextKey = "directory_id"
	IsSuperAdminKey   contextKey = "is_super_admin"
	IsModeratorKey    contextKey = "is_moderator"
	UserTypeKey       contextKey = "user_type"
)

// GetUserEmail extracts user email from request context
func GetUserEmail(r *http.Request) (string, bool) {
	userEmail, ok := r.Context().Value(UserEmailKey).(string)
	return userEmail, ok && userEmail != ""
}

// GetCSRFToken extracts CSRF token from request context
func GetCSRFToken(r *http.Request) (string, bool) {
	token, ok := r.Context().Value(CSRFTokenKey).(string)
	return token, ok && token != ""
}

// IsAuthenticated checks if user is authenticated
func IsAuthenticated(r *http.Request) bool {
	authenticated, ok := r.Context().Value(AuthenticatedKey).(bool)
	return ok && authenticated
}

// GetDirectoryID extracts directory ID from query parameter or returns default
func GetDirectoryID(r *http.Request) string {
	directoryID := r.URL.Query().Get("dir")
	if directoryID == "" {
		return "default" // Fall back to default directory
	}
	return directoryID
}

// GetUserType extracts user type from request context
func GetUserType(r *http.Request) (string, bool) {
	userType, ok := r.Context().Value(UserTypeKey).(string)
	return userType, ok && userType != ""
}

// IsSuperAdmin checks if user is super admin from context
func IsSuperAdmin(r *http.Request) bool {
	isSuperAdmin, ok := r.Context().Value(IsSuperAdminKey).(bool)
	return ok && isSuperAdmin
}

// IsModerator checks if user is moderator from context  
func IsModerator(r *http.Request) bool {
	isModerator, ok := r.Context().Value(IsModeratorKey).(bool)
	return ok && isModerator
}