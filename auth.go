package main

import (
	"context"
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

	_, err = app.DB.Exec(`
		INSERT OR REPLACE INTO admin_sessions (user_email, token) 
		VALUES (?, ?)
	`, userInfo.Email, string(tokenJSON))

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

	log.Printf("Authentication successful, redirecting to /admin")
	http.Redirect(w, r, "/admin", http.StatusTemporaryRedirect)
}

func (app *App) handleAdmin(w http.ResponseWriter, r *http.Request) {
	log.Printf("Admin handler called with URL: %s, imported param: %s", r.URL.String(), r.URL.Query().Get("imported"))
	
	userEmail, ok := r.Context().Value(UserEmailKey).(string)
	if !ok {
		log.Printf("User email not found in context")
		http.Error(w, "Authentication error", http.StatusInternalServerError)
		return
	}

	csrfToken, ok := r.Context().Value(CSRFTokenKey).(string)
	if !ok {
		log.Printf("CSRF token not found in context")
		http.Error(w, "Security token error", http.StatusInternalServerError)
		return
	}

	// Check if import was successful
	importSuccess := r.URL.Query().Get("imported") == "true"
	log.Printf("Import success flag: %v", importSuccess)

	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>Admin Panel - Directory Service</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 40px; }
        .container { max-width: 800px; }
        input[type="url"] { width: 500px; padding: 10px; }
        button { padding: 10px 20px; background: #007cba; color: white; border: none; cursor: pointer; }
        button:hover { background: #005a87; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Admin Panel</h1>
        <p>Welcome, {{.UserEmail}}</p>
        
        {{if .ImportSuccess}}
        <div style="background: #d4edda; color: #155724; padding: 15px; border: 1px solid #c3e6cb; border-radius: 4px; margin-bottom: 20px;">
            <strong>✅ Import Successful!</strong> The Google Sheet has been successfully imported into the directory.
            <a href="/" style="color: #155724; text-decoration: underline; margin-left: 10px;">View Directory</a>
        </div>
        {{end}}
        
        <form action="/import" method="POST" id="importForm">
            <h2>Import Google Sheet</h2>
            <p>Enter the URL of your Google Sheet:</p>
            <input type="url" name="sheet_url" id="sheet_url" placeholder="https://docs.google.com/spreadsheets/d/..." required>
            <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
            <br><br>
            <button type="button" id="previewBtn">Preview Sheet</button>
            <button type="submit" id="importBtn" style="display:none;">Import Sheet</button>
        </form>
        
        <div id="previewSection" style="display:none; margin-top: 20px; padding: 15px; border: 1px solid #ddd; border-radius: 5px; background: #f9f9f9;">
            <h3>Sheet Preview</h3>
            <div id="previewContent"></div>
            <div style="margin-top: 15px;">
                <button type="button" id="confirmImport" style="background: #28a745;">Confirm Import</button>
                <button type="button" id="cancelPreview" style="background: #666;">Cancel</button>
            </div>
        </div>
        
        <hr>
        <p><a href="/">View Directory</a> | <a href="/logout">Logout</a></p>
    </div>
    
    <script>
    let csrfToken = '{{.CSRFToken}}';
    
    document.getElementById('previewBtn').addEventListener('click', async function() {
        const sheetUrl = document.getElementById('sheet_url').value;
        
        if (!sheetUrl) {
            alert('Please enter a Google Sheets URL');
            return;
        }
        
        this.textContent = 'Loading...';
        this.disabled = true;
        
        try {
            const response = await fetch('/api/preview-sheet', {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': csrfToken
                },
                credentials: 'same-origin',
                body: JSON.stringify({
                    sheet_url: sheetUrl
                })
            });
            
            if (response.ok) {
                const preview = await response.json();
                showPreview(preview);
            } else {
                const errorText = await response.text();
                alert('Preview failed: ' + errorText);
            }
        } catch (error) {
            console.error('Preview error:', error);
            alert('Network error. Please try again.');
        } finally {
            this.textContent = 'Preview Sheet';
            this.disabled = false;
        }
    });
    
    document.getElementById('confirmImport').addEventListener('click', async function() {
        const form = document.getElementById('importForm');
        const formData = new FormData(form);
        
        try {
            const response = await fetch('/import', {
                method: 'POST',
                body: formData,
                credentials: 'same-origin'
            });
            
            if (response.redirected) {
                // Follow the redirect manually
                window.location.href = response.url;
            } else if (response.ok) {
                // If no redirect but successful, go to admin with success
                window.location.href = '/admin?imported=true';
            } else {
                alert('Import failed: ' + await response.text());
            }
        } catch (error) {
            console.error('Import error:', error);
            alert('Import failed due to network error');
        }
    });
    
    document.getElementById('cancelPreview').addEventListener('click', function() {
        document.getElementById('previewSection').style.display = 'none';
        document.getElementById('previewBtn').style.display = 'inline-block';
        document.getElementById('importBtn').style.display = 'none';
    });
    
    function showPreview(preview) {
        const content = document.getElementById('previewContent');
        
        let html = '<div style="margin-bottom: 10px;">';
        html += '<strong>Sheet Name:</strong> ' + escapeHtml(preview.sheet_name) + '<br>';
        html += '<strong>Data Rows:</strong> ' + preview.row_count + '<br>';
        html += '<strong>Columns (' + preview.columns.length + '):</strong>';
        html += '</div>';
        
        html += '<div style="display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px;">';
        preview.columns.forEach((column, index) => {
            html += '<span style="background: #e3f2fd; padding: 4px 8px; border-radius: 3px; border: 1px solid #bbdefb; font-size: 12px;">';
            html += (index + 1) + '. ' + escapeHtml(column);
            html += '</span>';
        });
        html += '</div>';
        
        content.innerHTML = html;
        
        document.getElementById('previewSection').style.display = 'block';
        document.getElementById('previewBtn').style.display = 'none';
        document.getElementById('importBtn').style.display = 'inline-block';
    }
    
    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }
    </script>
</body>
</html>`

	t, err := template.New("admin").Parse(tmpl)
	if err != nil {
		log.Printf("Failed to parse admin template: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	data := struct {
		UserEmail     string
		CSRFToken     string
		ImportSuccess bool
	}{
		UserEmail:     userEmail,
		CSRFToken:     csrfToken,
		ImportSuccess: importSuccess,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("Failed to execute admin template: %v", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return
	}
}
