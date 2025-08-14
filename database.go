package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"

	"directoryCommunityWebsite/utils"
)

func (app *App) handleGetDirectory(w http.ResponseWriter, r *http.Request) {
	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		log.Printf("Failed to get directory database for %s: %v", directoryID, err)
		utils.NotFoundError(w, "Directory")
		return
	}
	
	rows, err := db.Query("SELECT id, data FROM directory ORDER BY id")
	if err != nil {
		log.Printf("Failed to query directory: %v", err)
		utils.DatabaseError(w)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}()

	var entries []DirectoryEntry
	for rows.Next() {
		var entry DirectoryEntry
		if err := rows.Scan(&entry.ID, &entry.Data); err != nil {
			log.Printf("Failed to scan directory row: %v", err)
			utils.DatabaseError(w)
			return
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Row iteration error: %v", err)
		utils.DatabaseError(w)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	utils.RespondWithJSON(w, 200, entries)
}

func (app *App) handleGetColumns(w http.ResponseWriter, r *http.Request) {
	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		log.Printf("Failed to get directory database for %s: %v", directoryID, err)
		utils.NotFoundError(w, "Directory")
		return
	}
	
	var columnsJSON string
	err = db.QueryRow("SELECT columns FROM directory_columns WHERE id = 1").Scan(&columnsJSON)
	if err != nil {
		log.Printf("Failed to query columns: %v", err)
		// Return default columns if none found
		defaultColumns := []string{"Column 1", "Column 2", "Column 3"}
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		utils.RespondWithJSON(w, 200, defaultColumns)
		return
	}

	var columns []string
	if err := json.Unmarshal([]byte(columnsJSON), &columns); err != nil {
		log.Printf("Failed to unmarshal columns: %v", err)
		defaultColumns := []string{"Column 1", "Column 2", "Column 3"}
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		utils.RespondWithJSON(w, 200, defaultColumns)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	utils.RespondWithJSON(w, 200, columns)
}

func (app *App) handleDownloadDB(w http.ResponseWriter, r *http.Request) {
	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
	// Get directory info to find the database path
	directory, err := app.GetDirectory(directoryID)
	if err != nil {
		log.Printf("Directory %s not found: %v", directoryID, err)
		utils.NotFoundError(w, "Directory")
		return
	}
	
	file, err := os.Open(directory.DatabasePath)
	if err != nil {
		log.Printf("Failed to open database file for download: %v", err)
		utils.NotFoundError(w, "Database file")
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close database file: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+directoryID+".db")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	http.ServeFile(w, r, directory.DatabasePath)
}

func (app *App) handleHome(w http.ResponseWriter, r *http.Request) {
	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
	// Get directory information
	directory, err := app.GetDirectory(directoryID)
	if err != nil {
		log.Printf("Directory %s not found: %v", directoryID, err)
		utils.NotFoundError(w, "Directory")
		return
	}
	
	// Try to get user info and CSRF token from session if user is authenticated
	var csrfToken string
	var userEmail string
	var isAuthenticated bool
	var isAdmin bool
	var isDirectoryOwner bool
	var isModerator bool
	if session, err := app.SessionStore.Get(r, "auth-session"); err == nil {
		if sessionDataJSON, ok := session.Values["session_data"].(string); ok {
			var sessionData SessionData
			if json.Unmarshal([]byte(sessionDataJSON), &sessionData) == nil {
				csrfToken = sessionData.CSRFToken
				userEmail = sessionData.UserEmail
				isAuthenticated = sessionData.Authenticated
				
				// Check user roles
				if isAuthenticated {
					isAdmin, _ = app.IsAdmin(userEmail)
					isDirectoryOwner, _ = app.IsDirectoryOwner(directoryID, userEmail)
					isModerator, _ = app.IsModerator(userEmail, directoryID)
				}
			}
		}
	}

	tmpl, err := template.ParseFiles("templates/home.html")
	if err != nil {
		log.Printf("Failed to parse home template: %v", err)
		utils.InternalServerError(w, "Template error")
		return
	}

	// Build directory-aware URLs
	downloadURL := "/download/directory.db"
	adminURL := "/owner"
	if directoryID != "default" {
		downloadURL += "?dir=" + directoryID
		adminURL += "?dir=" + directoryID
	}

	data := struct {
		CSRFToken         string
		UserEmail         string
		IsAuthenticated   bool
		IsAdmin           bool
		IsDirectoryOwner  bool
		IsModerator       bool
		Directory         *Directory
		DownloadURL       string
		AdminURL          string
	}{
		CSRFToken:        csrfToken,
		UserEmail:        userEmail,
		IsAuthenticated:  isAuthenticated,
		IsAdmin:          isAdmin,
		IsDirectoryOwner: isDirectoryOwner,
		IsModerator:      isModerator,
		Directory:        directory,
		DownloadURL:      downloadURL,
		AdminURL:         adminURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Failed to execute home template: %v", err)
		utils.InternalServerError(w, "Template execution error")
		return
	}
}

// handleGetUserDirectories returns all directories a user has access to
func (app *App) handleGetUserDirectories(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}

	directories, err := app.GetUserDirectories(userEmail)
	if err != nil {
		log.Printf("Failed to get user directories for %s: %v", userEmail, err)
		utils.InternalServerError(w, "Failed to get directories")
		return
	}

	utils.RespondWithJSON(w, 200, directories)
}
