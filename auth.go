package main

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"time"

	"directoryCommunityWebsite/utils"
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
	
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}

	// Get CSRF token from context, or generate one if not present (for GET requests)
	csrfToken, ok := utils.GetCSRFToken(r)
	if !ok {
		// Generate a CSRF token for this request (needed for forms)
		var err error
		csrfToken, err = GenerateCSRFToken()
		if err != nil {
			log.Printf("Failed to generate CSRF token: %v", err)
			utils.InternalServerError(w, "Security token error")
			return
		}
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
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
        <h1>Admin Panel - {{.Directory.Name}}</h1>
        <div style="margin-bottom: 20px; color: #666;">
            <p>Welcome, {{.UserEmail}}</p>
            <p>Managing directory: <strong>{{.Directory.ID}}</strong>
            {{if .Directory.Description}} ({{.Directory.Description}}){{end}}
            </p>
        </div>
        
        {{if .ImportSuccess}}
        <div style="background: #d4edda; color: #155724; padding: 15px; border: 1px solid #c3e6cb; border-radius: 4px; margin-bottom: 20px;">
            <strong>✅ Import Successful!</strong> The Google Sheet has been successfully imported into the directory.
            <a href="{{.ViewDirectoryURL}}" style="color: #155724; text-decoration: underline; margin-left: 10px;">View Directory</a>
        </div>
        {{end}}
        
        <!-- Moderator Management Section -->
        <div style="margin: 30px 0;">
            <h2>Moderator Management</h2>
            
            <div style="margin-bottom: 20px;">
                <button id="showModeratorForm" style="background: #17a2b8;">Add Moderator</button>
                <button id="viewModerators" style="background: #6c757d;">View Moderators</button>
            </div>
            
            <!-- Add Moderator Form -->
            <div id="moderatorForm" style="display: none; background: #f8f9fa; padding: 20px; border-radius: 5px; margin-bottom: 20px;">
                <h3>Appoint New Moderator</h3>
                <div style="margin-bottom: 15px;">
                    <label>Email:</label><br>
                    <input type="email" id="moderatorEmail" style="width: 300px; padding: 8px;" placeholder="moderator@example.com">
                </div>
                <div style="margin-bottom: 15px;">
                    <label>Username:</label><br>
                    <input type="text" id="moderatorUsername" style="width: 300px; padding: 8px;" placeholder="username">
                </div>
                <div style="margin-bottom: 15px;">
                    <label>Auth Provider:</label><br>
                    <select id="authProvider" style="width: 316px; padding: 8px;">
                        <option value="google">Google</option>
                        <option value="twitter">Twitter/X</option>
                    </select>
                </div>
                <div style="margin-bottom: 15px;">
                    <label><input type="checkbox" id="canEdit" checked> Can Edit</label><br>
                    <label><input type="checkbox" id="canApprove"> Can Approve Changes</label><br>
                    <label><input type="checkbox" id="requiresApproval" checked> Requires Approval</label>
                </div>
                <div style="margin-bottom: 15px;">
                    <label>Row Access:</label><br>
                    <div style="margin-bottom: 10px;">
                        <label><input type="radio" name="rowAccessType" value="all" checked> All Rows</label><br>
                        <label><input type="radio" name="rowAccessType" value="specific"> Specific Rows</label>
                    </div>
                    <div id="specificRowsSection" style="display: none; border: 1px solid #ddd; padding: 10px; border-radius: 4px; max-height: 300px; overflow-y: auto; background: #f9f9f9;">
                        <div style="margin-bottom: 10px; font-weight: bold;">Select rows to allow access:</div>
                        <div id="rowSelectionList">Loading rows...</div>
                    </div>
                </div>
                <div>
                    <button id="appointModerator" style="background: #28a745;">Appoint Moderator</button>
                    <button id="cancelModerator" style="background: #6c757d;">Cancel</button>
                </div>
            </div>
            
            <!-- Moderators List -->
            <div id="moderatorsList" style="display: none;">
                <h3>Current Moderators</h3>
                <div id="moderatorsContent">Loading...</div>
            </div>
        </div>
        
        <form action="{{.ImportURL}}" method="POST" id="importForm">
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
        <p><a href="{{.ViewDirectoryURL}}">View Directory</a> | <a href="/logout">Logout</a></p>
    </div>
    
    <script>
    let csrfToken = '{{.CSRFToken}}';
    let previewURL = '{{.PreviewURL}}';
    let importURL = '{{.ImportURL}}';
    
    document.getElementById('previewBtn').addEventListener('click', async function() {
        const sheetUrl = document.getElementById('sheet_url').value;
        
        if (!sheetUrl) {
            alert('Please enter a Google Sheets URL');
            return;
        }
        
        this.textContent = 'Loading...';
        this.disabled = true;
        
        try {
            const response = await fetch(previewURL, {
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
            const response = await fetch(importURL, {
                method: 'POST',
                body: formData,
                credentials: 'same-origin'
            });
            
            if (response.redirected) {
                // Follow the redirect manually
                window.location.href = response.url;
            } else if (response.ok) {
                // If no redirect but successful, go to admin with success
                window.location.href = '{{.AdminURL}}&imported=true';
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
    
    // Moderator Management JavaScript
    let directoryId = '{{.Directory.ID}}';
    
    document.getElementById('showModeratorForm').addEventListener('click', async function() {
        document.getElementById('moderatorForm').style.display = 'block';
        document.getElementById('moderatorsList').style.display = 'none';
        await loadDirectoryRows();
    });
    
    document.getElementById('cancelModerator').addEventListener('click', function() {
        document.getElementById('moderatorForm').style.display = 'none';
        clearModeratorForm();
    });
    
    // Row access type handlers
    document.querySelectorAll('input[name="rowAccessType"]').forEach(radio => {
        radio.addEventListener('change', function() {
            const specificSection = document.getElementById('specificRowsSection');
            if (this.value === 'specific') {
                specificSection.style.display = 'block';
            } else {
                specificSection.style.display = 'none';
            }
        });
    });
    
    document.getElementById('viewModerators').addEventListener('click', async function() {
        document.getElementById('moderatorForm').style.display = 'none';
        document.getElementById('moderatorsList').style.display = 'block';
        await loadModerators();
    });
    
    document.getElementById('appointModerator').addEventListener('click', async function() {
        const email = document.getElementById('moderatorEmail').value.trim();
        const username = document.getElementById('moderatorUsername').value.trim();
        const authProvider = document.getElementById('authProvider').value;
        const canEdit = document.getElementById('canEdit').checked;
        const canApprove = document.getElementById('canApprove').checked;
        const requiresApproval = document.getElementById('requiresApproval').checked;
        const rowAccessType = document.querySelector('input[name="rowAccessType"]:checked').value;
        
        if (!email || !username) {
            alert('Please fill in email and username');
            return;
        }
        
        let rowFilter = [];
        if (rowAccessType === 'specific') {
            // Get selected rows from checkboxes
            const selectedCheckboxes = document.querySelectorAll('#rowSelectionList input[type="checkbox"]:checked');
            rowFilter = Array.from(selectedCheckboxes).map(cb => parseInt(cb.value));
            
            if (rowFilter.length === 0) {
                alert('Please select at least one row or choose "All Rows"');
                return;
            }
        }
        
        const data = {
            user_email: email,
            username: username,
            auth_provider: authProvider,
            directory_id: directoryId,
            can_edit: canEdit,
            can_approve: canApprove,
            requires_approval: requiresApproval,
            row_filter: rowFilter
        };
        
        try {
            const response = await fetch('/api/moderators/appoint?dir=' + directoryId, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': csrfToken
                },
                credentials: 'same-origin',
                body: JSON.stringify(data)
            });
            
            if (response.ok) {
                alert('Moderator appointed successfully!');
                clearModeratorForm();
                document.getElementById('moderatorForm').style.display = 'none';
            } else {
                const error = await response.text();
                alert('Failed to appoint moderator: ' + error);
            }
        } catch (error) {
            console.error('Error appointing moderator:', error);
            alert('Network error occurred');
        }
    });
    
    async function loadDirectoryRows() {
        try {
            const response = await fetch('/api/directory?dir=' + directoryId, {
                credentials: 'same-origin'
            });
            
            if (response.ok) {
                const directory = await response.json();
                const rowSelectionList = document.getElementById('rowSelectionList');
                
                if (directory && directory.length > 0) {
                    rowSelectionList.innerHTML = '';
                    
                    directory.forEach(row => {
                        // Parse the row data to show a preview
                        let rowDataPreview = '';
                        try {
                            const data = JSON.parse(row.data);
                            // Show first few fields as preview
                            rowDataPreview = Object.values(data).slice(0, 3).join(' | ');
                            if (rowDataPreview.length > 50) {
                                rowDataPreview = rowDataPreview.substring(0, 50) + '...';
                            }
                        } catch (e) {
                            rowDataPreview = 'Row ' + row.id;
                        }
                        
                        const checkboxDiv = document.createElement('div');
                        checkboxDiv.style.marginBottom = '5px';
                        checkboxDiv.innerHTML = 
                            '<label style="display: flex; align-items: center; padding: 5px; cursor: pointer;">' +
                                '<input type="checkbox" value="' + row.id + '" style="margin-right: 8px;">' +
                                '<span style="font-weight: bold; margin-right: 8px;">Row ' + row.id + ':</span>' +
                                '<span style="color: #666; font-size: 0.9em;">' + rowDataPreview + '</span>' +
                            '</label>';
                        rowSelectionList.appendChild(checkboxDiv);
                    });
                } else {
                    rowSelectionList.innerHTML = '<div style="color: #666; font-style: italic;">No rows found in directory</div>';
                }
            } else {
                document.getElementById('rowSelectionList').innerHTML = '<div style="color: #dc3545;">Failed to load directory rows</div>';
            }
        } catch (error) {
            console.error('Error loading directory rows:', error);
            document.getElementById('rowSelectionList').innerHTML = '<div style="color: #dc3545;">Error loading rows</div>';
        }
    }
    
    async function loadModerators() {
        try {
            const response = await fetch('/api/moderators?dir=' + directoryId, {
                credentials: 'same-origin'
            });
            
            if (response.ok) {
                const moderators = await response.json();
                displayModerators(moderators);
            } else {
                document.getElementById('moderatorsContent').innerHTML = '<p style="color: red;">Failed to load moderators</p>';
            }
        } catch (error) {
            console.error('Error loading moderators:', error);
            document.getElementById('moderatorsContent').innerHTML = '<p style="color: red;">Network error</p>';
        }
    }
    
    function displayModerators(moderators) {
        const content = document.getElementById('moderatorsContent');
        
        if (moderators.length === 0) {
            content.innerHTML = '<p>No moderators assigned to this directory yet.</p>';
            return;
        }
        
        let html = '<div style="display: grid; gap: 15px;">';
        moderators.forEach(mod => {
            html += '<div style="background: white; padding: 15px; border: 1px solid #ddd; border-radius: 5px;">';
            html += '<div style="font-weight: bold; margin-bottom: 5px;">' + escapeHtml(mod.username) + '</div>';
            html += '<div style="color: #666; font-size: 14px; margin-bottom: 5px;">' + escapeHtml(mod.user_email) + '</div>';
            html += '<div style="margin-bottom: 5px;"><span style="background: #e3f2fd; padding: 2px 6px; border-radius: 3px; font-size: 12px;">' + mod.auth_provider + '</span></div>';
            html += '<div style="font-size: 12px; color: #888;">Appointed by: ' + escapeHtml(mod.appointed_by) + ' (' + mod.appointed_by_type + ')</div>';
            html += '<div style="font-size: 12px; color: #888;">Created: ' + new Date(mod.created_at).toLocaleDateString() + '</div>';
            html += '<div style="margin-top: 10px;">';
            html += '<button onclick="removeModerator(\'' + mod.user_email + '\')" style="background: #dc3545; color: white; border: none; padding: 5px 10px; border-radius: 3px; cursor: pointer; font-size: 12px;">Remove</button>';
            html += '</div>';
            html += '</div>';
        });
        html += '</div>';
        
        content.innerHTML = html;
    }
    
    async function removeModerator(email) {
        if (!confirm('Are you sure you want to remove this moderator?')) {
            return;
        }
        
        try {
            const response = await fetch('/api/moderators/remove?dir=' + directoryId, {
                method: 'DELETE',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': csrfToken
                },
                credentials: 'same-origin',
                body: JSON.stringify({
                    moderator_email: email,
                    directory_id: directoryId
                })
            });
            
            if (response.ok) {
                alert('Moderator removed successfully!');
                await loadModerators();
            } else {
                const error = await response.text();
                alert('Failed to remove moderator: ' + error);
            }
        } catch (error) {
            console.error('Error removing moderator:', error);
            alert('Network error occurred');
        }
    }
    
    function clearModeratorForm() {
        document.getElementById('moderatorEmail').value = '';
        document.getElementById('moderatorUsername').value = '';
        document.getElementById('authProvider').value = 'google';
        document.getElementById('canEdit').checked = true;
        document.getElementById('canApprove').checked = false;
        document.getElementById('requiresApproval').checked = true;
        
        // Reset row access controls
        document.querySelector('input[name="rowAccessType"][value="all"]').checked = true;
        document.getElementById('specificRowsSection').style.display = 'none';
        
        // Uncheck all row checkboxes
        const checkboxes = document.querySelectorAll('#rowSelectionList input[type="checkbox"]');
        checkboxes.forEach(cb => cb.checked = false);
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
	if err := t.Execute(w, data); err != nil {
		log.Printf("Failed to execute admin template: %v", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return
	}
}
