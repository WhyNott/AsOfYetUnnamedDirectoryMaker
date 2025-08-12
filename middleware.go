package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type contextKey string

const (
	UserEmailKey     contextKey = "user_email"
	CSRFTokenKey     contextKey = "csrf_token"
	AuthenticatedKey contextKey = "authenticated"
	DirectoryIDKey   contextKey = "directory_id"
	IsSuperAdminKey  contextKey = "is_super_admin"
)

func (app *App) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapper := &responseWriterWrapper{ResponseWriter: w, statusCode: 200}
		
		next.ServeHTTP(wrapper, r)

		duration := time.Since(start)
		
		AppLogger.WithFields(map[string]interface{}{
			"method":      r.Method,
			"path":        r.URL.Path,
			"duration_ms": duration.Milliseconds(),
			"status_code": wrapper.statusCode,
			"remote_addr": r.RemoteAddr,
			"user_agent":  r.UserAgent(),
		}).Info("HTTP request completed")
	})
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (app *App) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("AuthMiddleware called for: %s", r.URL.Path)

		session, err := app.SessionStore.Get(r, "auth-session")
		if err != nil {
			log.Printf("Failed to get session in auth middleware for %s: %v", r.URL.Path, err)
			// If session is corrupted, redirect to login to start fresh
			log.Printf("Redirecting to /login from AuthMiddleware")
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		sessionDataJSON, ok := session.Values["session_data"].(string)
		if !ok || sessionDataJSON == "" {
			// No session data, redirect to login
			log.Printf("No session data found for %s, redirecting to /login", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		var sessionData SessionData
		if err := json.Unmarshal([]byte(sessionDataJSON), &sessionData); err != nil {
			log.Printf("Failed to unmarshal session data: %v", err)
			// Clear corrupted session data
			delete(session.Values, "session_data")
			session.Save(r, w)
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		if sessionData.IsExpired(app.Config.SessionMaxAge) {
			log.Println("Session expired")
			delete(session.Values, "session_data")
			session.Save(r, w)
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		if !sessionData.Authenticated {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		ctx := context.WithValue(r.Context(), UserEmailKey, sessionData.UserEmail)
		ctx = context.WithValue(ctx, CSRFTokenKey, sessionData.CSRFToken)
		ctx = context.WithValue(ctx, AuthenticatedKey, true)

		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// DirectoryAuthMiddleware checks if user has access to a specific directory
func (app *App) DirectoryAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user email from context (set by AuthMiddleware)
		userEmail, ok := r.Context().Value(UserEmailKey).(string)
		if !ok {
			http.Error(w, "User not authenticated", http.StatusUnauthorized)
			return
		}

		// Extract directory ID from URL parameter
		directoryID := r.URL.Query().Get("dir")
		if directoryID == "" {
			// For now, default to "default" directory for backward compatibility
			directoryID = "default"
		}

		// Check if user is super admin
		isSuperAdmin, err := app.IsSuperAdmin(userEmail)
		if err != nil {
			log.Printf("Error checking super admin status: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// If not super admin, check directory ownership
		if !isSuperAdmin {
			hasAccess, err := app.IsDirectoryOwner(directoryID, userEmail)
			if err != nil {
				log.Printf("Error checking directory ownership: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
				return
			}
			if !hasAccess {
				http.Error(w, "Access denied - you don't have permission to access this directory", http.StatusForbidden)
				return
			}
		}

		// Verify directory exists
		directory, err := app.GetDirectory(directoryID)
		if err != nil {
			if err.Error() == "directory not found" {
				http.Error(w, "Directory not found", http.StatusNotFound)
				return
			}
			log.Printf("Error getting directory: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Add directory info to context
		ctx := context.WithValue(r.Context(), DirectoryIDKey, directoryID)
		ctx = context.WithValue(ctx, IsSuperAdminKey, isSuperAdmin)

		log.Printf("Directory access granted: user=%s, directory=%s, super_admin=%v", userEmail, directoryID, isSuperAdmin)

		// Store directory in request for handlers to use
		r.Header.Set("X-Directory-ID", directory.ID)
		r.Header.Set("X-Directory-Path", directory.DatabasePath)

		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// SuperAdminMiddleware requires super admin privileges
func (app *App) SuperAdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userEmail, ok := r.Context().Value(UserEmailKey).(string)
		if !ok {
			http.Error(w, "User not authenticated", http.StatusUnauthorized)
			return
		}

		isSuperAdmin, err := app.IsSuperAdmin(userEmail)
		if err != nil {
			log.Printf("Error checking super admin status: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if !isSuperAdmin {
			http.Error(w, "Access denied - super admin privileges required", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), IsSuperAdminKey, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (app *App) CSRFMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
			expectedToken, ok := r.Context().Value(CSRFTokenKey).(string)
			if !ok {
				http.Error(w, "CSRF token not found in session", http.StatusForbidden)
				return
			}

			var providedToken string
			if r.Header.Get("Content-Type") == "application/json" {
				providedToken = r.Header.Get("X-CSRF-Token")
			} else {
				providedToken = r.FormValue("csrf_token")
			}

			if providedToken != expectedToken {
				log.Printf("CSRF token mismatch. Expected: %s, Got: %s", expectedToken, providedToken)
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}
		}

		next.ServeHTTP(w, r)
	}
}

func ValidateEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email) && len(email) <= 255
}

func ValidateSheetURL(url string) bool {
	if len(url) > 2048 {
		return false
	}

	sheetRegex := regexp.MustCompile(`^https://docs\.google\.com/spreadsheets/d/[a-zA-Z0-9-_]+`)
	return sheetRegex.MatchString(url)
}

func SanitizeInput(input string) string {
	input = strings.TrimSpace(input)
	input = html.EscapeString(input)
	if len(input) > 1000 {
		input = input[:1000]
	}
	return input
}

func ValidateRowData(data []string) error {
	if len(data) == 0 {
		return &ValidationError{"Row data cannot be empty"}
	}

	if len(data) > 50 {
		return &ValidationError{"Row cannot have more than 50 columns"}
	}

	for i, cell := range data {
		if len(cell) > 1000 {
			return &ValidationError{fmt.Sprintf("Cell %d exceeds maximum length of 1000 characters", i)}
		}
	}

	return nil
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func (app *App) RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				AppLogger.WithFields(map[string]interface{}{
					"method":     r.Method,
					"path":       r.URL.Path,
					"panic":      fmt.Sprintf("%v", err),
					"remote_addr": r.RemoteAddr,
				}).Error("Panic recovered in HTTP handler")
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
