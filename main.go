package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type App struct {
	DB           *sql.DB
	SessionStore *sessions.CookieStore
	OAuthConfig  *oauth2.Config
	Config       *Config
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

	sessionStore := sessions.NewCookieStore(config.SessionSecret)
	sessionStore.MaxAge(config.SessionMaxAge)
	sessionStore.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   config.SessionMaxAge,
		HttpOnly: true,
		Secure:   config.Environment == "production",
		SameSite: http.SameSiteLaxMode, // Changed to Lax to allow OAuth redirects
	}

	log.Printf("Session store configured with MaxAge: %d, Secure: %v", config.SessionMaxAge, config.Environment == "production")

	app := &App{
		Config:       config,
		SessionStore: sessionStore,
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

	r := mux.NewRouter()

	r.Use(app.RecoveryMiddleware)
	r.Use(app.LoggingMiddleware)

	r.HandleFunc("/", app.handleHome).Methods("GET")
	r.HandleFunc("/test", app.handleTest).Methods("GET")                // Simple test endpoint
	r.HandleFunc("/admin-direct", app.handleAdminDirect).Methods("GET") // Bypass OAuth for testing
	r.HandleFunc("/login", app.handleLogin).Methods("GET")
	r.HandleFunc("/logout", app.handleLogout).Methods("GET")
	r.HandleFunc("/auth/callback", app.handleAuthCallback).Methods("GET")
	r.HandleFunc("/admin", app.AuthMiddleware(app.handleAdmin)).Methods("GET")
	r.HandleFunc("/import", app.AuthMiddleware(app.CSRFMiddleware(app.handleImport))).Methods("POST")
	r.HandleFunc("/api/preview-sheet", app.AuthMiddleware(app.CSRFMiddleware(app.handlePreviewSheet))).Methods("POST")
	r.HandleFunc("/api/directory", app.handleGetDirectory).Methods("GET")
	r.HandleFunc("/api/corrections", app.AuthMiddleware(app.CSRFMiddleware(app.handleCorrection))).Methods("POST")
	r.HandleFunc("/api/add-row", app.AuthMiddleware(app.CSRFMiddleware(app.handleAddRow))).Methods("POST")
	r.HandleFunc("/api/delete-row", app.AuthMiddleware(app.CSRFMiddleware(app.handleDeleteRow))).Methods("DELETE")
	r.HandleFunc("/download/directory.db", app.handleDownloadDB).Methods("GET")

	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	fmt.Printf("Server starting on :%s\n", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, r))
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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := app.DB.Exec(query)
	return err
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
			<p><a href="/admin">Try Normal Admin (with OAuth)</a></p>
			<p><a href="/login">Try Login</a></p>
			<p><a href="/logout">Logout</a></p>
		</body>
		</html>
	`))
}
