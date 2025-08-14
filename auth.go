package main

import (
	"context"
	utils2 "directoryCommunityWebsite/internal/utils"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"
	oauth2api "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
)

//TODO: This file needs: 1) Extract that giant template somewhere else 2) Make it clear which parts are for google and which are for X

func (app *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	log.Printf("Login handler called from: %s", r.Header.Get("Referer"))
	log.Printf("Request URL: %s", r.URL.String())

	state, err := GenerateSecureToken(16)
	if err != nil {
		log.Printf("Failed to generate state: %v", err)
		http.Error(w, "Failed to generate state", http.StatusInternalServerError)
		return
	}

	session, err := app.SessionStore.Get(r, "auth-session")
	if err != nil {
		log.Printf("Failed to get session, creating new one: %v", err)
		// Create a new session if the old one is corrupted
		session, err = app.SessionStore.New(r, "auth-session")
		if err != nil {
			log.Printf("Failed to create new session: %v", err)
			http.Error(w, "Session error", http.StatusInternalServerError)
			return
		}
	}

	// Clear any existing session data to start fresh
	for k := range session.Values {
		delete(session.Values, k)
	}

	session.Values["state"] = state

	// Ensure session options are set correctly
	session.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   app.Config.SessionMaxAge,
		HttpOnly: true,
		Secure:   app.Config.Environment == "production",
		SameSite: http.SameSiteLaxMode, // Lax mode to allow OAuth redirects
	}

	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	log.Printf("Saved state in session: %s", state)
	url := app.OAuthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "select_account"))
	log.Printf("Redirecting to OAuth URL: %s", url)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (app *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Get user email from session for database cleanup
	userEmail := ""
	if session, err := app.SessionStore.Get(r, "auth-session"); err == nil && session != nil {
		if sessionDataJSON, ok := session.Values["session_data"].(string); ok {
			var sessionData SessionData
			if json.Unmarshal([]byte(sessionDataJSON), &sessionData) == nil {
				userEmail = sessionData.UserEmail
			}
		}
	}

	// Clear session from database if we have user email
	if userEmail != "" {
		_, err := app.DB.Exec("DELETE FROM admin_sessions WHERE user_email = ?", userEmail)
		if err != nil {
			log.Printf("Failed to clear admin session from database: %v", err)
		}
	}

	session, err := app.SessionStore.Get(r, "auth-session")
	if err != nil {
		// Even if we can't get the session, clear the cookie
		log.Printf("Failed to get session during logout: %v", err)
	}

	if session != nil {
		// Clear all session values
		for k := range session.Values {
			delete(session.Values, k)
		}

		// Set MaxAge to -1 to delete the cookie
		session.Options.MaxAge = -1
		session.Save(r, w)
	}

	// Also try to clear the cookie manually in case session handling fails
	http.SetCookie(w, &http.Cookie{
		Name:     "auth-session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   app.Config.Environment == "production",
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("User logged out successfully")
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (app *App) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	log.Printf("OAuth callback received")

	session, err := app.SessionStore.Get(r, "auth-session")
	if err != nil {
		log.Printf("Failed to get session in callback: %v", err)
		http.Error(w, "Session error - please try logging in again", http.StatusBadRequest)
		return
	}

	log.Printf("Session retrieved successfully. Session values: %+v", session.Values)

	state, ok := session.Values["state"].(string)
	if !ok || state == "" {
		log.Printf("No state found in session. Session values: %+v", session.Values)
		http.Error(w, "Invalid state parameter - please try logging in again", http.StatusBadRequest)
		return
	}

	providedState := r.URL.Query().Get("state")
	log.Printf("State comparison - Session: '%s', Provided: '%s'", state, providedState)
	if state != providedState {
		log.Printf("State mismatch. Session: %s, Provided: %s", state, providedState)
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		log.Printf("No authorization code found in callback")
		http.Error(w, "Code not found", http.StatusBadRequest)
		return
	}

	log.Printf("Exchanging authorization code for token")
	token, err := app.OAuthConfig.Exchange(context.Background(), code)
	if err != nil {
		log.Printf("Failed to exchange token: %v", err)
		http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
		return
	}

	log.Printf("Token exchange successful")

	log.Printf("Creating OAuth2 client and service")
	client := app.OAuthConfig.Client(context.Background(), token)
	oauth2Service, err := oauth2api.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		log.Printf("Failed to create OAuth2 service: %v", err)
		http.Error(w, "Failed to create OAuth2 service", http.StatusInternalServerError)
		return
	}

	log.Printf("Getting user info from Google")
	userInfo, err := oauth2Service.Userinfo.Get().Do()
	if err != nil {
		log.Printf("Failed to get user info: %v", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	log.Printf("User info received: %s", userInfo.Email)

	log.Printf("Saving admin session to database")
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		log.Printf("Failed to marshal token: %v", err)
		http.Error(w, "Token processing error", http.StatusInternalServerError)
		return
	}

	// Encrypt the token before storing
	encryptedToken, err := app.EncryptionService.Encrypt(string(tokenJSON))
	if err != nil {
		log.Printf("Failed to encrypt token: %v", err)
		http.Error(w, "Token encryption error", http.StatusInternalServerError)
		return
	}

	_, err = app.DB.Exec(`
		INSERT OR REPLACE INTO admin_sessions (user_email, token) 
		VALUES (?, ?)
	`, userInfo.Email, encryptedToken)

	if err != nil {
		log.Printf("Failed to save admin session to database: %v", err)
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	log.Printf("Creating session data structure")
	csrfToken, err := GenerateCSRFToken()
	if err != nil {
		log.Printf("Failed to generate CSRF token: %v", err)
		http.Error(w, "Security token error", http.StatusInternalServerError)
		return
	}

	sessionData := SessionData{
		UserEmail:     userInfo.Email,
		Authenticated: true,
		CSRFToken:     csrfToken,
		CreatedAt:     time.Now(),
	}

	sessionDataJSON, err := json.Marshal(sessionData)
	if err != nil {
		log.Printf("Failed to marshal session data: %v", err)
		http.Error(w, "Session processing error", http.StatusInternalServerError)
		return
	}

	log.Printf("Saving session data to cookie")
	session.Values["session_data"] = string(sessionDataJSON)
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save session to cookie: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	log.Printf("Authentication successful, redirecting to /owner")
	http.Redirect(w, r, "/owner", http.StatusTemporaryRedirect)
}

func (app *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	log.Printf("Admin handler called with URL: %s, imported param: %s", r.URL.String(), r.URL.Query().Get("imported"))

	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	// Get CSRF token from context, or generate one if not present (for GET requests)
	csrfToken, ok := utils2.GetCSRFToken(r)
	if !ok {
		// Generate a CSRF token for this request (needed for forms)
		var err error
		csrfToken, err = GenerateCSRFToken()
		if err != nil {
			log.Printf("Failed to generate CSRF token: %v", err)
			utils2.InternalServerError(w, "Security token error")
			return
		}
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := utils2.GetDirectoryID(r)

	// Get directory information
	directory, err := app.GetDirectory(directoryID)
	if err != nil {
		log.Printf("Directory %s not found: %v", directoryID, err)
		http.Error(w, "Directory not found", http.StatusNotFound)
		return
	}

	// Check if user has access to this directory
	isOwner, err := app.IsDirectoryOwner(directoryID, userEmail)
	if err != nil {
		log.Printf("Failed to check directory ownership: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	isAdmin, err := app.IsAdmin(userEmail)
	if err != nil {
		log.Printf("Failed to check super admin status: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	if !isOwner && !isAdmin {
		log.Printf("User %s does not have access to directory %s", userEmail, directoryID)
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if import was successful
	importSuccess := r.URL.Query().Get("imported") == "true"
	log.Printf("Import success flag: %v", importSuccess)

	// Use the proper owner template file instead of inline template
	tmpl, err := template.ParseFiles("templates/owner.html")
	if err != nil {
		log.Printf("Failed to parse owner template: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	// Build directory-aware URLs
	viewDirectoryURL := "/"
	importURL := "/import"
	previewURL := "/api/preview-sheet"
	adminURL := "/owner"
	if directoryID != "default" {
		viewDirectoryURL += "?dir=" + directoryID
		importURL += "?dir=" + directoryID
		previewURL += "?dir=" + directoryID
		adminURL += "?dir=" + directoryID
	}

	data := struct {
		UserEmail        string
		CSRFToken        string
		ImportSuccess    bool
		Directory        *Directory
		ViewDirectoryURL string
		ImportURL        string
		PreviewURL       string
		AdminURL         string
	}{
		UserEmail:        userEmail,
		CSRFToken:        csrfToken,
		ImportSuccess:    importSuccess,
		Directory:        directory,
		ViewDirectoryURL: viewDirectoryURL,
		ImportURL:        importURL,
		PreviewURL:       previewURL,
		AdminURL:         adminURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Failed to execute owner template: %v", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return
	}
}
