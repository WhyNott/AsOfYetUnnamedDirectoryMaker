package main

import (
	"encoding/json"
	"log"
	"net/http"

	"directoryCommunityWebsite/utils"
)

// handleAppointModerator handles appointing a new moderator
func (app *App) handleAppointModerator(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}
	
	userType, ok := utils.GetUserType(r)
	if !ok {
		utils.AuthorizationError(w)
		return
	}
	
	var req AppointModeratorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode appoint moderator request: %v", err)
		utils.BadRequestError(w, "Invalid request body")
		return
	}
	
	// Validate input
	if req.UserEmail == "" || req.Username == "" || req.DirectoryID == "" {
		utils.ValidationError(w, "User email, username, and directory ID are required")
		return
	}
	
	if req.AuthProvider == "" {
		req.AuthProvider = AuthProviderGoogle // Default to Google
	}
	
	// Appoint the moderator
	err := app.AppointModerator(userEmail, userType, req)
	if err != nil {
		log.Printf("Failed to appoint moderator: %v", err)
		if err.Error() == "user does not have permission to appoint moderators" {
			utils.AuthorizationError(w)
		} else {
			utils.InternalServerError(w, "Failed to appoint moderator")
		}
		return
	}
	
	utils.RespondWithSuccess(w, nil, "Moderator appointed successfully")
}

// handleGetModerators returns all moderators for a directory
func (app *App) handleGetModerators(w http.ResponseWriter, r *http.Request) {
	directoryID := utils.GetDirectoryID(r)
	
	moderators, err := app.GetModeratorsByDirectory(directoryID)
	if err != nil {
		log.Printf("Failed to get moderators for directory %s: %v", directoryID, err)
		utils.InternalServerError(w, "Failed to get moderators")
		return
	}
	
	utils.RespondWithJSON(w, 200, moderators)
}

// handleRemoveModerator handles removing a moderator
func (app *App) handleRemoveModerator(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}
	
	userType, ok := utils.GetUserType(r)
	if !ok {
		utils.AuthorizationError(w)
		return
	}
	
	var req struct {
		ModeratorEmail string `json:"moderator_email"`
		DirectoryID    string `json:"directory_id"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.BadRequestError(w, "Invalid request body")
		return
	}
	
	// Check if user can remove this moderator
	canRemove, err := app.CanRemoveModerator(userEmail, userType, req.ModeratorEmail, req.DirectoryID)
	if err != nil {
		log.Printf("Failed to check remove moderator permission: %v", err)
		utils.InternalServerError(w, "Internal server error")
		return
	}
	
	if !canRemove {
		utils.AuthorizationError(w)
		return
	}
	
	// Remove the moderator
	err = app.RemoveModerator(req.ModeratorEmail, req.DirectoryID)
	if err != nil {
		log.Printf("Failed to remove moderator: %v", err)
		utils.InternalServerError(w, "Failed to remove moderator")
		return
	}
	
	utils.RespondWithSuccess(w, nil, "Moderator removed successfully")
}

// handleGetModeratorPermissions returns a moderator's permissions
func (app *App) handleGetModeratorPermissions(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}
	
	directoryID := utils.GetDirectoryID(r)
	
	// Check if this is the moderator requesting their own permissions
	targetEmail := r.URL.Query().Get("moderator_email")
	if targetEmail == "" {
		targetEmail = userEmail // Default to requesting user's permissions
	}
	
	// Only allow users to see their own permissions unless they're owner/admin
	userType, _ := app.GetUserType(userEmail, directoryID)
	if targetEmail != userEmail && userType != UserTypeOwner && userType != UserTypeOwner {
		utils.AuthorizationError(w)
		return
	}
	
	permissions, err := app.GetModeratorPermissions(targetEmail, directoryID)
	if err != nil {
		if err.Error() == "moderator permissions not found" {
			utils.NotFoundError(w, "Moderator permissions")
		} else {
			log.Printf("Failed to get moderator permissions: %v", err)
			utils.InternalServerError(w, "Failed to get moderator permissions")
		}
		return
	}
	
	utils.RespondWithJSON(w, 200, permissions)
}

// handleGetPendingChanges returns pending changes for approval
func (app *App) handleGetPendingChanges(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}
	
	directoryID := utils.GetDirectoryID(r)
	userType, _ := app.GetUserType(userEmail, directoryID)
	
	// Only show changes that the user can approve
	var changes []PendingChange
	var err error
	
	if userType == UserTypeAdmin || userType == UserTypeOwner {
		// Platform admins and directory owners can see all pending changes
		changes, err = app.GetPendingChanges(directoryID, "")
	} else if userType == UserTypeModerator {
		// Moderators can only see changes they can approve
		canApprove, approveErr := app.moderatorCanApprove(userEmail, directoryID)
		if approveErr != nil || !canApprove {
			// Return empty list if they can't approve changes
			changes = []PendingChange{}
		} else {
			changes, err = app.GetPendingChanges(directoryID, userEmail)
		}
	} else {
		changes = []PendingChange{} // Regular users can't see pending changes
	}
	
	if err != nil {
		log.Printf("Failed to get pending changes: %v", err)
		utils.InternalServerError(w, "Failed to get pending changes")
		return
	}
	
	utils.RespondWithJSON(w, 200, changes)
}

// handleApproveChange handles approving or rejecting a change
func (app *App) handleApproveChange(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}
	
	directoryID := utils.GetDirectoryID(r)
	
	var req ChangeApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.BadRequestError(w, "Invalid request body")
		return
	}
	
	// Validate action
	if req.Action != "approve" && req.Action != "reject" {
		utils.ValidationError(w, "Invalid action - must be 'approve' or 'reject'")
		return
	}
	
	// Check if user can approve changes
	canApprove, err := app.CanApproveChange(userEmail, directoryID, req.ChangeID)
	if err != nil {
		log.Printf("Failed to check approval permission: %v", err)
		utils.InternalServerError(w, "Internal server error")
		return
	}
	
	if !canApprove {
		utils.AuthorizationError(w)
		return
	}
	
	// Process the approval/rejection
	err = app.ProcessChangeApproval(req.ChangeID, userEmail, req.Action, req.Reason)
	if err != nil {
		log.Printf("Failed to process change approval: %v", err)
		utils.InternalServerError(w, "Failed to process change approval")
		return
	}
	
	utils.RespondWithSuccess(w, nil, "Change processed successfully")
}

// handleGetModeratorHierarchy returns the hierarchy of moderators
func (app *App) handleGetModeratorHierarchy(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils.RequireAuthentication(w, r)
	if !ok {
		return
	}
	
	directoryID := utils.GetDirectoryID(r)
	
	// Get the moderator email to check hierarchy for
	targetEmail := r.URL.Query().Get("moderator_email")
	if targetEmail == "" {
		targetEmail = userEmail // Default to requesting user
	}
	
	hierarchy, err := app.GetModeratorHierarchy(targetEmail, directoryID)
	if err != nil {
		log.Printf("Failed to get moderator hierarchy: %v", err)
		utils.InternalServerError(w, "Failed to get moderator hierarchy")
		return
	}
	
	utils.RespondWithJSON(w, 200, hierarchy)
}