package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"

	"directoryCommunityWebsite/utils"
)

// handleSuperAdmin displays the super admin panel
func (app *App) handleSuperAdmin(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}

	csrfToken, ok := utils.RequireCSRFToken(w, r)
	if !ok {
		return
	}

	tmpl, err := template.ParseFiles("templates/super-admin.html")
	if err != nil {
		log.Printf("Failed to parse super admin template: %v", err)
		utils.InternalServerError(w, "Template error")
		return
	}

	data := struct {
		UserEmail string
		CSRFToken string
	}{UserEmail: userEmail, CSRFToken: csrfToken}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("Failed to execute super admin template: %v", err)
		utils.InternalServerError(w, "Template execution error")
		return
	}
}

// handleGetAllDirectories returns all directories for super admin
func (app *App) handleGetAllDirectories(w http.ResponseWriter, r *http.Request) {
	rows, err := app.DB.Query(`
		SELECT d.id, d.name, d.description, d.database_path, d.created_at, d.updated_at
		FROM directories d
		ORDER BY d.created_at DESC
	`)
	if err != nil {
		log.Printf("Failed to query all directories: %v", err)
		utils.DatabaseError(w)
		return
	}
	defer rows.Close()

	var directories []Directory
	for rows.Next() {
		var dir Directory
		err := rows.Scan(&dir.ID, &dir.Name, &dir.Description, &dir.DatabasePath, &dir.CreatedAt, &dir.UpdatedAt)
		if err != nil {
			log.Printf("Failed to scan directory: %v", err)
			utils.DatabaseError(w)
			return
		}
		directories = append(directories, dir)
	}

	utils.RespondWithJSON(w, 200, directories)
}

// CreateDirectoryRequest represents the API request for creating a directory
type CreateDirectoryRequest struct {
	DirectoryID   string `json:"directory_id"`
	DirectoryName string `json:"directory_name"`
	OwnerEmail    string `json:"owner_email"`
	Description   string `json:"description"`
}

// handleCreateDirectory creates a new directory via API
func (app *App) handleCreateDirectory(w http.ResponseWriter, r *http.Request) {
	var req CreateDirectoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.BadRequestError(w, "Invalid request body")
		return
	}

	// Validate input
	if req.DirectoryID == "" || req.DirectoryName == "" || req.OwnerEmail == "" {
		utils.ValidationError(w, "Directory ID, name, and owner email are required")
		return
	}

	if !ValidateEmail(req.OwnerEmail) {
		utils.ValidationError(w, "Invalid email address")
		return
	}

	// Create directory
	if err := app.CreateDirectory(req.DirectoryID, req.DirectoryName, req.Description, req.OwnerEmail); err != nil {
		log.Printf("Failed to create directory: %v", err)
		if err.Error() == "invalid directory ID: must contain only letters, numbers, hyphens, and underscores" {
			utils.BadRequestError(w, err.Error())
		} else {
			utils.InternalServerError(w, "Failed to create directory")
		}
		return
	}

	utils.RespondWithSuccess(w, nil, "Directory created successfully")
}

// handleDeleteDirectory deletes a directory and all its associated data
func (app *App) handleDeleteDirectory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DirectoryID string `json:"directory_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.BadRequestError(w, "Invalid request body")
		return
	}

	if req.DirectoryID == "" {
		utils.ValidationError(w, "Directory ID is required")
		return
	}

	if req.DirectoryID == "default" {
		utils.BadRequestError(w, "Cannot delete default directory")
		return
	}

	// Delete directory
	if err := app.DeleteDirectory(req.DirectoryID); err != nil {
		log.Printf("Failed to delete directory: %v", err)
		utils.InternalServerError(w, "Failed to delete directory")
		return
	}

	utils.RespondWithSuccess(w, nil, "Directory deleted successfully")
}