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

	"directoryCommunityWebsite/utils"
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

		ctx := context.WithValue(r.Context(), utils.UserEmailKey, sessionData.UserEmail)
		ctx = context.WithValue(ctx, utils.CSRFTokenKey, sessionData.CSRFToken)
		ctx = context.WithValue(ctx, utils.AuthenticatedKey, true)

		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// TemplateContextMiddleware adds common template data to the request context
func (app *App) TemplateContextMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// Get directory ID
		directoryID := r.URL.Query().Get("dir")
		if directoryID == "" {
			directoryID = "default"
		}
		ctx = context.WithValue(ctx, utils.DirectoryIDKey, directoryID)
		
		// Check if user is authenticated
		userEmail, isAuthenticated := ctx.Value(utils.UserEmailKey).(string)
		if isAuthenticated {
			// Get user permissions
			isAdmin, _ := app.IsAdmin(userEmail)
			isDirectoryOwner, _ := app.IsDirectoryOwner(directoryID, userEmail)
			isModerator, _ := app.IsModerator(userEmail, directoryID)
			
			// Get user type
			userType, _ := app.GetUserType(userEmail, directoryID)
			
			// Add to context
			ctx = context.WithValue(ctx, utils.IsAdminKey, isAdmin)
			ctx = context.WithValue(ctx, utils.IsModeratorKey, isModerator)
			ctx = context.WithValue(ctx, utils.UserTypeKey, userType)
			
			// Add a computed "IsDirectoryOwner" context value
			ctx = context.WithValue(ctx, utils.IsDirectoryOwnerKey, isDirectoryOwner)
		}
		
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// StaticCacheMiddleware adds cache headers for static assets
func StaticCacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get file extension
		path := r.URL.Path
		
		// Set cache headers based on file type
		if strings.HasSuffix(path, ".css") || strings.HasSuffix(path, ".js") {
			// CSS and JS files - cache for 1 week
			w.Header().Set("Cache-Control", "public, max-age=604800")
			w.Header().Set("Vary", "Accept-Encoding")
		} else if strings.HasSuffix(path, ".png") || strings.HasSuffix(path, ".jpg") || 
			     strings.HasSuffix(path, ".jpeg") || strings.HasSuffix(path, ".gif") || 
			     strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".ico") {
			// Images - cache for 1 month
			w.Header().Set("Cache-Control", "public, max-age=2592000")
		} else if strings.HasSuffix(path, ".woff") || strings.HasSuffix(path, ".woff2") || 
			     strings.HasSuffix(path, ".ttf") || strings.HasSuffix(path, ".otf") {
			// Fonts - cache for 1 year
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		} else {
			// Other static files - cache for 1 day
			w.Header().Set("Cache-Control", "public, max-age=86400")
		}
		
		// Add ETag for better caching
		if stat, err := http.Dir("./static/").Open(strings.TrimPrefix(path, "/static/")); err == nil {
			if fileInfo, err := stat.Stat(); err == nil {
				etag := fmt.Sprintf(`"%x-%x"`, fileInfo.ModTime().Unix(), fileInfo.Size())
				w.Header().Set("ETag", etag)
				
				// Check if client has the file cached
				if match := r.Header.Get("If-None-Match"); match == etag {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
			stat.Close()
		}
		
		next.ServeHTTP(w, r)
	})
}

// DirectoryAuthMiddleware checks if user has access to a specific directory
func (app *App) DirectoryAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get user email from context (set by AuthMiddleware)
		userEmail, ok := r.Context().Value(utils.UserEmailKey).(string)
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

		// Check if user is admin
		isAdmin, err := app.IsAdmin(userEmail)
		if err != nil {
			log.Printf("Error checking admin status: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// If not admin, check directory ownership
		if !isAdmin {
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
		ctx := context.WithValue(r.Context(), utils.DirectoryIDKey, directoryID)
		ctx = context.WithValue(ctx, utils.IsAdminKey, isAdmin)

		log.Printf("Directory access granted: user=%s, directory=%s, admin=%v", userEmail, directoryID, isAdmin)

		// Store directory in request for handlers to use
		r.Header.Set("X-Directory-ID", directory.ID)
		r.Header.Set("X-Directory-Path", directory.DatabasePath)

		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// AdminMiddleware requires admin privileges
func (app *App) AdminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userEmail, ok := r.Context().Value(utils.UserEmailKey).(string)
		if !ok {
			http.Error(w, "User not authenticated", http.StatusUnauthorized)
			return
		}

		isAdmin, err := app.IsAdmin(userEmail)
		if err != nil {
			log.Printf("Error checking admin status: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if !isAdmin {
			http.Error(w, "Access denied - admin privileges required", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), utils.IsAdminKey, true)
		ctx = context.WithValue(ctx, utils.UserTypeKey, UserTypeOwner)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// ModeratorMiddleware requires moderator privileges for a directory
func (app *App) ModeratorMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userEmail, ok := r.Context().Value(utils.UserEmailKey).(string)
		if !ok {
			http.Error(w, "User not authenticated", http.StatusUnauthorized)
			return
		}
		
		directoryID := GetCurrentDirectoryID(r)
		
		// Get user type for this directory
		userType, err := app.GetUserType(userEmail, directoryID)
		if err != nil {
			log.Printf("Error checking user type: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		
		// Allow admins, owners, and moderators
		if userType != UserTypeAdmin && userType != UserTypeOwner && userType != UserTypeModerator {
			http.Error(w, "Access denied - moderator privileges required", http.StatusForbidden)
			return
		}
		
		ctx := context.WithValue(r.Context(), utils.UserTypeKey, userType)
		ctx = context.WithValue(ctx, utils.DirectoryIDKey, directoryID)
		
		if userType == UserTypeAdmin {
			ctx = context.WithValue(ctx, utils.IsAdminKey, true)
		} else if userType == UserTypeOwner {
			// Directory owners don't need special context flags
		} else if userType == UserTypeModerator {
			ctx = context.WithValue(ctx, utils.IsModeratorKey, true)
		}
		
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// AdminOrModeratorMiddleware requires admin or moderator privileges
func (app *App) AdminOrModeratorMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userEmail, ok := r.Context().Value(utils.UserEmailKey).(string)
		if !ok {
			http.Error(w, "User not authenticated", http.StatusUnauthorized)
			return
		}
		
		directoryID := GetCurrentDirectoryID(r)
		
		// Get user type for this directory
		userType, err := app.GetUserType(userEmail, directoryID)
		if err != nil {
			log.Printf("Error checking user type: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		
		// Allow super admins, admins, and moderators
		if userType == "" {
			http.Error(w, "Access denied - admin or moderator privileges required", http.StatusForbidden)
			return
		}
		
		ctx := context.WithValue(r.Context(), utils.UserTypeKey, userType)
		ctx = context.WithValue(ctx, utils.DirectoryIDKey, directoryID)
		
		switch userType {
		case UserTypeAdmin:
			ctx = context.WithValue(ctx, utils.IsAdminKey, true)
		case UserTypeModerator:
			ctx = context.WithValue(ctx, utils.IsModeratorKey, true)
		}
		
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (app *App) CSRFMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
			expectedToken, ok := r.Context().Value(utils.CSRFTokenKey).(string)
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
