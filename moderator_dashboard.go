package main

import (
	"html/template"
	"log"
	"net/http"

	"directoryCommunityWebsite/utils"
)

// handleModeratorDashboard shows the moderator self-management interface
func (app *App) handleModeratorDashboard(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}

	csrfToken, ok := utils.RequireCSRFToken(w, r)
	if !ok {
		return
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
	// Get directory information
	directory, err := app.GetDirectory(directoryID)
	if err != nil {
		log.Printf("Directory %s not found: %v", directoryID, err)
		utils.NotFoundError(w, "Directory")
		return
	}
	
	// Get user type to confirm they're a moderator
	userType, err := app.GetUserType(userEmail, directoryID)
	if err != nil {
		log.Printf("Failed to get user type: %v", err)
		utils.DatabaseError(w)
		return
	}
	
	if userType != UserTypeModerator {
		utils.AuthorizationError(w)
		return
	}

	// Get moderator permissions
	permissions, err := app.GetModeratorPermissions(userEmail, directoryID)
	if err != nil {
		log.Printf("Failed to get moderator permissions: %v", err)
		utils.InternalServerError(w, "Failed to get permissions")
		return
	}

	tmpl, err := template.ParseFiles("templates/moderator.html")
	if err != nil {
		log.Printf("Failed to parse moderator template: %v", err)
		utils.InternalServerError(w, "Template error")
		return
	}

	// Build directory-aware URLs
	viewDirectoryURL := "/"
	if directoryID != "default" {
		viewDirectoryURL += "?dir=" + directoryID
	}

	data := struct {
		UserEmail        string
		CSRFToken        string
		Directory        *Directory
		Permissions      *ModeratorPermissions
		ViewDirectoryURL string
	}{
		UserEmail:        userEmail,
		CSRFToken:        csrfToken,
		Directory:        directory,
		Permissions:      permissions,
		ViewDirectoryURL: viewDirectoryURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Failed to execute moderator template: %v", err)
		utils.InternalServerError(w, "Template execution error")
		return
	}
}