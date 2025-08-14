package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"directoryCommunityWebsite/utils"
	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type App struct {
	DB                  *sql.DB
	SessionStore        *sessions.CookieStore
	OAuthConfig         *oauth2.Config
	TwitterOAuthConfig  *oauth2.Config
	Config              *Config
	EncryptionService   *EncryptionService
	DirectoryDBManager  *DirectoryDatabaseManager
	PermissionCache     *PermissionCache
}

type DirectoryEntry struct {
	ID   int    `json:"id"`
	Data string `json:"data"`
}

type CorrectionRequest struct {
	Row    int    `json:"row"`
	Column int    `json:"column"`
	Value  string `json:"value"`
}

type AddRowRequest struct {
	Data []string `json:"data"`
}

type DeleteRowRequest struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

type PreviewRequest struct {
	SheetURL string `json:"sheet_url"`
}

type PreviewResponse struct {
	Columns   []string `json:"columns"`
	RowCount  int      `json:"row_count"`
	SheetName string   `json:"sheet_name"`
}

func main() {
	config, err := LoadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	// Initialize structured logging
	InitializeLogger(config)

	sessionStore := sessions.NewCookieStore(config.SessionSecret)
	sessionStore.MaxAge(config.SessionMaxAge)
	sessionStore.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   config.SessionMaxAge,
		HttpOnly: true,
		Secure:   config.Environment == "production",
		SameSite: http.SameSiteLaxMode, // Changed to Lax to allow OAuth redirects
	}

	AppLogger.WithFields(map[string]interface{}{
		"max_age": config.SessionMaxAge,
		"secure":  config.Environment == "production",
		"environment": config.Environment,
	}).Info("Session store configured")

	app := &App{
		Config:            config,
		SessionStore:      sessionStore,
		EncryptionService: NewEncryptionService(config.EncryptionKey),
		OAuthConfig: &oauth2.Config{
			ClientID:     config.GoogleClientID,
			ClientSecret: config.GoogleClientSecret,
			RedirectURL:  config.RedirectURL,
			Scopes: []string{
				"https://www.googleapis.com/auth/spreadsheets",
				"https://www.googleapis.com/auth/userinfo.email",
			},
			Endpoint: google.Endpoint,
		},
	}
	
	// Initialize Twitter OAuth config if configured
	app.TwitterOAuthConfig = app.TwitterConfig()

	app.DB, err = sql.Open("sqlite3", config.DatabasePath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	app.DB.SetMaxOpenConns(25)
	app.DB.SetMaxIdleConns(25)
	app.DB.SetConnMaxLifetime(5 * time.Minute)
	defer app.DB.Close()

	if err := app.initDatabase(); err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	// Initialize directory database manager and permission cache
	app.DirectoryDBManager = NewDirectoryDatabaseManager(app)
	app.PermissionCache = NewPermissionCache()

	r := mux.NewRouter()

	r.Use(app.RecoveryMiddleware)
	r.Use(app.LoggingMiddleware)
	r.Use(app.SpecialRateLimitMiddleware())

	r.HandleFunc("/", app.handleHome).Methods("GET")
	//r.HandleFunc("/test", app.handleTest).Methods("GET")                // Simple test endpoint
	//r.HandleFunc("/admin-direct", app.handleAdminDirect).Methods("GET") // Bypass OAuth for testing
	r.HandleFunc("/login", app.handleLoginPage).Methods("GET")
	r.HandleFunc("/logout", app.handleLogout).Methods("GET")
	r.HandleFunc("/auth/google", app.handleLogin).Methods("GET")  // Renamed Google-specific login
	r.HandleFunc("/auth/callback", app.handleAuthCallback).Methods("GET")
	r.HandleFunc("/auth/twitter", app.handleTwitterLogin).Methods("GET")
	r.HandleFunc("/auth/twitter/callback", app.handleTwitterCallback).Methods("GET")
	r.HandleFunc("/debug-auth", app.handleDebugAuth).Methods("GET")
	r.HandleFunc("/debug-middleware", app.AuthMiddleware(app.handleDebugMiddleware)).Methods("GET")
	r.HandleFunc("/owner", app.AuthMiddleware(app.DirectoryAuthMiddleware(app.handleAdmin))).Methods("GET")
	r.HandleFunc("/import", app.AuthMiddleware(app.CSRFMiddleware(app.handleImport))).Methods("POST")
	r.HandleFunc("/api/preview-sheet", app.AuthMiddleware(app.CSRFMiddleware(app.handlePreviewSheet))).Methods("POST")
	r.HandleFunc("/api/directory", app.handleGetDirectory).Methods("GET")
	r.HandleFunc("/api/columns", app.handleGetColumns).Methods("GET")
	r.HandleFunc("/api/user-directories", app.AuthMiddleware(app.handleGetUserDirectories)).Methods("GET")
	r.HandleFunc("/api/corrections", app.AuthMiddleware(app.CSRFMiddleware(app.handleCorrection))).Methods("POST")
	r.HandleFunc("/api/add-row", app.AuthMiddleware(app.CSRFMiddleware(app.handleAddRow))).Methods("POST")
	r.HandleFunc("/api/delete-row", app.AuthMiddleware(app.CSRFMiddleware(app.handleDeleteRow))).Methods("DELETE")
	r.HandleFunc("/download/directory.db", app.handleDownloadDB).Methods("GET")
	
	// Admin routes (platform-wide)
	r.HandleFunc("/admin", app.AuthMiddleware(app.AdminMiddleware(app.handleSuperAdmin))).Methods("GET")
	r.HandleFunc("/api/admin/directories", app.AuthMiddleware(app.AdminMiddleware(app.handleGetAllDirectories))).Methods("GET")
	r.HandleFunc("/api/admin/create-directory", app.AuthMiddleware(app.AdminMiddleware(app.CSRFMiddleware(app.handleCreateDirectory)))).Methods("POST")
	r.HandleFunc("/api/admin/delete-directory", app.AuthMiddleware(app.AdminMiddleware(app.CSRFMiddleware(app.handleDeleteDirectory)))).Methods("DELETE")
	
	// Moderator management routes
	r.HandleFunc("/api/moderators", app.AuthMiddleware(app.AdminOrModeratorMiddleware(app.handleGetModerators))).Methods("GET")
	r.HandleFunc("/api/moderators/appoint", app.AuthMiddleware(app.AdminOrModeratorMiddleware(app.CSRFMiddleware(app.handleAppointModerator)))).Methods("POST")
	r.HandleFunc("/api/moderators/remove", app.AuthMiddleware(app.AdminOrModeratorMiddleware(app.CSRFMiddleware(app.handleRemoveModerator)))).Methods("DELETE")
	r.HandleFunc("/api/moderators/permissions", app.AuthMiddleware(app.ModeratorMiddleware(app.handleGetModeratorPermissions))).Methods("GET")
	r.HandleFunc("/api/moderators/hierarchy", app.AuthMiddleware(app.ModeratorMiddleware(app.handleGetModeratorHierarchy))).Methods("GET")
	
	// Change approval routes
	r.HandleFunc("/api/changes/pending", app.AuthMiddleware(app.ModeratorMiddleware(app.handleGetPendingChanges))).Methods("GET")
	r.HandleFunc("/api/changes/approve", app.AuthMiddleware(app.ModeratorMiddleware(app.CSRFMiddleware(app.handleApproveChange)))).Methods("POST")
	
	// Moderator dashboard
	r.HandleFunc("/moderator", app.AuthMiddleware(app.ModeratorMiddleware(app.handleModeratorDashboard))).Methods("GET")

	r.PathPrefix("/static/").Handler(StaticCacheMiddleware(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/")))))

	AppLogger.WithField("port", config.Port).Info("Server starting")
	if err := http.ListenAndServe(":"+config.Port, r); err != nil {
		AppLogger.WithError(err).Fatal("Server failed to start")
	}
}

func (app *App) initDatabase() error {
	query := `
		CREATE TABLE IF NOT EXISTS directory (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			data TEXT NOT NULL
		);
		
		CREATE TABLE IF NOT EXISTS admin_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email TEXT NOT NULL,
			sheet_url TEXT,
			token TEXT,
			directory_id TEXT DEFAULT 'default',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS directories (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			database_path TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS directory_owners (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			directory_id TEXT NOT NULL,
			user_email TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'owner',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (directory_id) REFERENCES directories(id)
		);
		
		CREATE TABLE IF NOT EXISTS admins (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email TEXT NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		
		CREATE TABLE IF NOT EXISTS moderators (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email TEXT NOT NULL,
			username TEXT NOT NULL,
			auth_provider TEXT NOT NULL DEFAULT 'google', -- 'google' or 'twitter'
			directory_id TEXT NOT NULL,
			appointed_by TEXT NOT NULL, -- email of the user who appointed them
			appointed_by_type TEXT NOT NULL DEFAULT 'admin', -- 'admin' or 'moderator'
			is_active BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (directory_id) REFERENCES directories(id),
			UNIQUE(user_email, directory_id)
		);
		
		CREATE TABLE IF NOT EXISTS moderator_hierarchy (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_moderator_email TEXT NOT NULL,
			child_moderator_email TEXT NOT NULL,
			directory_id TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (directory_id) REFERENCES directories(id),
			UNIQUE(parent_moderator_email, child_moderator_email, directory_id)
		);
		
		CREATE TABLE IF NOT EXISTS moderator_domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			moderator_email TEXT NOT NULL,
			directory_id TEXT NOT NULL,
			row_filter TEXT, -- JSON array of row IDs or filter conditions
			can_edit BOOLEAN DEFAULT TRUE,
			can_approve BOOLEAN DEFAULT FALSE,
			requires_approval BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (directory_id) REFERENCES directories(id),
			UNIQUE(moderator_email, directory_id)
		);
		
		CREATE TABLE IF NOT EXISTS pending_changes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			directory_id TEXT NOT NULL,
			row_id INTEGER NOT NULL,
			column_name TEXT NOT NULL,
			old_value TEXT,
			new_value TEXT NOT NULL,
			change_type TEXT NOT NULL, -- 'edit', 'add', 'delete'
			submitted_by TEXT NOT NULL, -- moderator email
			status TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'approved', 'rejected'
			reviewed_by TEXT, -- approver email
			reviewed_at DATETIME,
			reason TEXT, -- reason for rejection or notes
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (directory_id) REFERENCES directories(id)
		);
		
		CREATE TABLE IF NOT EXISTS user_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_email TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL,
			auth_provider TEXT NOT NULL, -- 'google' or 'twitter'
			provider_id TEXT NOT NULL, -- unique ID from the auth provider
			avatar_url TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := app.DB.Exec(query); err != nil {
		return err
	}

	// Add directory_id column to admin_sessions if it doesn't exist (migration)
	_, err := app.DB.Exec(`ALTER TABLE admin_sessions ADD COLUMN directory_id TEXT DEFAULT 'default'`)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		log.Printf("Note: Could not add directory_id column to admin_sessions: %v", err)
		// This is not a fatal error, column might already exist
	}

	// Migrate to multi-directory system if needed
	return app.MigrateToMultiDirectory()
}

func (app *App) handleTest(w http.ResponseWriter, r *http.Request) {
	log.Printf("Test endpoint called")
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("Test endpoint working - no redirects here!"))
}

func (app *App) handleAdminDirect(w http.ResponseWriter, r *http.Request) {
	log.Printf("Direct admin access - bypassing OAuth for testing")
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`
		<html>
		<head><title>Direct Admin Access</title></head>
		<body>
			<h1>Direct Admin Access (Testing)</h1>
			<p>This bypasses OAuth for debugging purposes.</p>
			<p><a href="/owner">Try Directory Owner Panel (with OAuth)</a></p>
			<p><a href="/login">Try Login</a></p>
			<p><a href="/logout">Logout</a></p>
		</body>
		</html>
	`))
}

func (app *App) handleDebugAuth(w http.ResponseWriter, r *http.Request) {
	session, err := app.SessionStore.Get(r, "auth-session")
	if err != nil {
		fmt.Fprintf(w, "Session error: %v\n", err)
		return
	}

	sessionDataJSON, ok := session.Values["session_data"].(string)
	if !ok || sessionDataJSON == "" {
		fmt.Fprintf(w, "No session data found\n")
		return
	}

	var sessionData SessionData
	if err := json.Unmarshal([]byte(sessionDataJSON), &sessionData); err != nil {
		fmt.Fprintf(w, "Failed to unmarshal session data: %v\n", err)
		return
	}

	fmt.Fprintf(w, "Session valid:\n")
	fmt.Fprintf(w, "User: %s\n", sessionData.UserEmail)
	fmt.Fprintf(w, "Authenticated: %t\n", sessionData.Authenticated)
	fmt.Fprintf(w, "Created: %s\n", sessionData.CreatedAt)
}

func (app *App) handleDebugMiddleware(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Middleware test:\n")
	
	// Check if AuthMiddleware set the context
	userEmail, ok := r.Context().Value(utils.UserEmailKey).(string)
	fmt.Fprintf(w, "UserEmailKey in context: %t\n", ok)
	if ok {
		fmt.Fprintf(w, "User email: %s\n", userEmail)
	}
	
	// Test utils.RequireAuthentication 
	userEmail2, ok2 := utils.RequireAuthentication(w, r)
	if !ok2 {
		fmt.Fprintf(w, "utils.RequireAuthentication failed\n")
		return
	}
	fmt.Fprintf(w, "utils.RequireAuthentication succeeded: %s\n", userEmail2)
}
