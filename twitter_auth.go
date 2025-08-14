package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"directoryCommunityWebsite/utils"
	"golang.org/x/oauth2"
)

// TwitterConfig creates the Twitter OAuth2 configuration
func (app *App) TwitterConfig() *oauth2.Config {
	if app.Config.TwitterClientID == "" || app.Config.TwitterClientSecret == "" {
		return nil // Twitter OAuth not configured
	}
	
	return &oauth2.Config{
		ClientID:     app.Config.TwitterClientID,
		ClientSecret: app.Config.TwitterClientSecret,
		RedirectURL:  app.Config.TwitterRedirectURL,
		Scopes:       []string{"tweet.read", "users.read"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://twitter.com/i/oauth2/authorize",
			TokenURL: "https://api.twitter.com/2/oauth2/token",
		},
	}
}

// TwitterUserInfo represents user information from Twitter API
type TwitterUserInfo struct {
	Data struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Username string `json:"username"`
		Email    string `json:"email"`
	} `json:"data"`
}

// handleTwitterLogin initiates Twitter OAuth flow
func (app *App) handleTwitterLogin(w http.ResponseWriter, r *http.Request) {
	twitterConfig := app.TwitterConfig()
	if twitterConfig == nil {
		utils.RespondWithError(w, http.StatusServiceUnavailable, "Twitter authentication not configured")
		return
	}
	
	// Generate state token for CSRF protection
	state, err := generateStateToken()
	if err != nil {
		log.Printf("Failed to generate state token: %v", err)
		utils.InternalServerError(w, "Internal server error")
		return
	}
	
	// Store state in session
	session, err := app.SessionStore.Get(r, "oauth-session")
	if err != nil {
		log.Printf("Failed to get OAuth session: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	
	session.Values["state"] = state
	session.Values["provider"] = "twitter"
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to save OAuth session: %v", err)
		http.Error(w, "Session save error", http.StatusInternalServerError)
		return
	}
	
	// Redirect to Twitter OAuth
	authURL := twitterConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// handleTwitterCallback handles the Twitter OAuth callback
func (app *App) handleTwitterCallback(w http.ResponseWriter, r *http.Request) {
	twitterConfig := app.TwitterConfig()
	if twitterConfig == nil {
		http.Error(w, "Twitter authentication not configured", http.StatusServiceUnavailable)
		return
	}
	
	// Get the OAuth session
	session, err := app.SessionStore.Get(r, "oauth-session")
	if err != nil {
		log.Printf("Failed to get OAuth session: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	
	// Verify state parameter
	expectedState, ok := session.Values["state"].(string)
	if !ok || expectedState == "" {
		log.Printf("No state found in session")
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}
	
	receivedState := r.URL.Query().Get("state")
	if receivedState != expectedState {
		log.Printf("State mismatch. Expected: %s, Got: %s", expectedState, receivedState)
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}
	
	// Check for error in callback
	if errorCode := r.URL.Query().Get("error"); errorCode != "" {
		log.Printf("Twitter OAuth error: %s", errorCode)
		http.Error(w, "OAuth authorization failed", http.StatusBadRequest)
		return
	}
	
	// Exchange authorization code for token
	code := r.URL.Query().Get("code")
	if code == "" {
		log.Printf("No authorization code received")
		http.Error(w, "No authorization code received", http.StatusBadRequest)
		return
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	token, err := twitterConfig.Exchange(ctx, code)
	if err != nil {
		log.Printf("Failed to exchange code for token: %v", err)
		http.Error(w, "Failed to exchange authorization code", http.StatusInternalServerError)
		return
	}
	
	// Get user information from Twitter
	userInfo, err := app.getTwitterUserInfo(ctx, token)
	if err != nil {
		log.Printf("Failed to get Twitter user info: %v", err)
		http.Error(w, "Failed to get user information", http.StatusInternalServerError)
		return
	}
	
	// For moderators, we need to use a different email format since Twitter might not provide email
	// We'll use twitter:{username}@moderator.local as a unique identifier
	userEmail := fmt.Sprintf("twitter:%s@moderator.local", userInfo.Data.Username)
	if userInfo.Data.Email != "" {
		// If Twitter provides email, use it
		userEmail = userInfo.Data.Email
	}
	
	// Create user profile
	err = app.createOrUpdateUserProfileDirect(userEmail, userInfo.Data.Username, AuthProviderTwitter, userInfo.Data.ID)
	if err != nil {
		log.Printf("Failed to create user profile: %v", err)
		http.Error(w, "Failed to create user profile", http.StatusInternalServerError)
		return
	}
	
	// Create session for the user
	err = app.createUserSession(w, r, userEmail, AuthProviderTwitter)
	if err != nil {
		log.Printf("Failed to create user session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}
	
	// Clean up OAuth session
	delete(session.Values, "state")
	delete(session.Values, "provider")
	session.Save(r, w)
	
	// Redirect to home page or moderator dashboard
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// getTwitterUserInfo fetches user information from Twitter API
func (app *App) getTwitterUserInfo(ctx context.Context, token *oauth2.Token) (*TwitterUserInfo, error) {
	client := app.TwitterConfig().Client(ctx, token)
	
	// Twitter API v2 endpoint for user information
	resp, err := client.Get("https://api.twitter.com/2/users/me?user.fields=email")
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Twitter API returned status %d", resp.StatusCode)
	}
	
	var userInfo TwitterUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %v", err)
	}
	
	return &userInfo, nil
}

// generateStateToken generates a random state token for OAuth
func generateStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// createOrUpdateUserProfileDirect creates or updates a user profile directly
func (app *App) createOrUpdateUserProfileDirect(userEmail, username, authProvider, providerID string) error {
	_, err := app.DB.Exec(`
		INSERT OR REPLACE INTO user_profiles 
		(user_email, username, auth_provider, provider_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userEmail, username, authProvider, providerID, time.Now(), time.Now())
	
	return err
}

// createUserSession creates an authenticated session for a user
func (app *App) createUserSession(w http.ResponseWriter, r *http.Request, userEmail, authProvider string) error {
	session, err := app.SessionStore.Get(r, "auth-session")
	if err != nil {
		return fmt.Errorf("failed to get auth session: %v", err)
	}
	
	// Generate CSRF token
	csrfToken, err := GenerateCSRFToken()
	if err != nil {
		return fmt.Errorf("failed to generate CSRF token: %v", err)
	}
	
	// Create session data
	sessionData := SessionData{
		UserEmail:     userEmail,
		Authenticated: true,
		CSRFToken:     csrfToken,
		CreatedAt:     time.Now(),
	}
	
	// Marshal session data
	sessionDataJSON, err := json.Marshal(sessionData)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %v", err)
	}
	
	session.Values["session_data"] = string(sessionDataJSON)
	
	if err := session.Save(r, w); err != nil {
		return fmt.Errorf("failed to save session: %v", err)
	}
	
	log.Printf("Created session for user: %s (provider: %s)", userEmail, authProvider)
	return nil
}

// handleLoginPage shows a page with both Google and Twitter login options
func (app *App) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// Check if user is already authenticated
	session, err := app.SessionStore.Get(r, "auth-session")
	if err == nil {
		if sessionDataJSON, ok := session.Values["session_data"].(string); ok {
			var sessionData SessionData
			if json.Unmarshal([]byte(sessionDataJSON), &sessionData) == nil && sessionData.Authenticated {
				// Already logged in, redirect to home
				http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
				return
			}
		}
	}
	
	hasTwitter := app.Config.TwitterClientID != "" && app.Config.TwitterClientSecret != ""
	
	tmpl, err := template.ParseFiles("templates/login.html")
	if err != nil {
		log.Printf("Failed to parse login template: %v", err)
		utils.InternalServerError(w, "Template error")
		return
	}
	
	data := struct {
		HasTwitter bool
	}{HasTwitter: hasTwitter}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Failed to execute login template: %v", err)
		utils.InternalServerError(w, "Template execution error")
		return
	}
}