package main

import (
	utils2 "directoryCommunityWebsite/internal/utils"
	"encoding/json"
	"log"
	"net/http"
)

// handleAppointModerator handles appointing a new moderator
func (app *App) handleAppointModerator(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	userType, ok := utils2.GetUserType(r)
	if !ok {
		utils2.AuthorizationError(w)
		return
	}

	var req AppointModeratorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode appoint moderator request: %v", err)
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	// Validate input
	if req.UserEmail == "" || req.Username == "" || req.DirectoryID == "" {
		utils2.ValidationError(w, "User email, username, and directory ID are required")
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
			utils2.AuthorizationError(w)
		} else {
			utils2.InternalServerError(w, "Failed to appoint moderator")
		}
		return
	}

	utils2.RespondWithSuccess(w, nil, "Moderator appointed successfully")
}

// handleGetModerators returns all moderators for a directory
func (app *App) handleGetModerators(w http.ResponseWriter, r *http.Request) {
	directoryID := utils2.GetDirectoryID(r)

	moderators, err := app.GetModeratorsByDirectory(directoryID)
	if err != nil {
		log.Printf("Failed to get moderators for directory %s: %v", directoryID, err)
		utils2.InternalServerError(w, "Failed to get moderators")
		return
	}

	utils2.RespondWithJSON(w, 200, moderators)
}

// handleRemoveModerator handles removing a moderator
func (app *App) handleRemoveModerator(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	userType, ok := utils2.GetUserType(r)
	if !ok {
		utils2.AuthorizationError(w)
		return
	}

	var req struct {
		ModeratorEmail string `json:"moderator_email"`
		DirectoryID    string `json:"directory_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	// Check if user can remove this moderator
	canRemove, err := app.CanRemoveModerator(userEmail, userType, req.ModeratorEmail, req.DirectoryID)
	if err != nil {
		log.Printf("Failed to check remove moderator permission: %v", err)
		utils2.InternalServerError(w, "Internal server error")
		return
	}

	if !canRemove {
		utils2.AuthorizationError(w)
		return
	}

	// Remove the moderator
	err = app.RemoveModerator(req.ModeratorEmail, req.DirectoryID)
	if err != nil {
		log.Printf("Failed to remove moderator: %v", err)
		utils2.InternalServerError(w, "Failed to remove moderator")
		return
	}

	utils2.RespondWithSuccess(w, nil, "Moderator removed successfully")
}

// handleGetModeratorPermissions returns a moderator's permissions
func (app *App) handleGetModeratorPermissions(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	directoryID := utils2.GetDirectoryID(r)

	// Check if this is the moderator requesting their own permissions
	targetEmail := r.URL.Query().Get("moderator_email")
	if targetEmail == "" {
		targetEmail = userEmail // Default to requesting user's permissions
	}

	// Only allow users to see their own permissions unless they're owner/admin
	userType, _ := app.GetUserType(userEmail, directoryID)
	if targetEmail != userEmail && userType != UserTypeOwner && userType != UserTypeOwner {
		utils2.AuthorizationError(w)
		return
	}

	permissions, err := app.GetModeratorPermissions(targetEmail, directoryID)
	if err != nil {
		if err.Error() == "moderator permissions not found" {
			utils2.NotFoundError(w, "Moderator permissions")
		} else {
			log.Printf("Failed to get moderator permissions: %v", err)
			utils2.InternalServerError(w, "Failed to get moderator permissions")
		}
		return
	}

	utils2.RespondWithJSON(w, 200, permissions)
}

// handleGetPendingChanges returns pending changes for approval
func (app *App) handleGetPendingChanges(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	directoryID := utils2.GetDirectoryID(r)
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
		utils2.InternalServerError(w, "Failed to get pending changes")
		return
	}

	utils2.RespondWithJSON(w, 200, changes)
}

// handleApproveChange handles approving or rejecting a change
func (app *App) handleApproveChange(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	directoryID := utils2.GetDirectoryID(r)

	var req ChangeApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	// Validate action
	if req.Action != "approve" && req.Action != "reject" {
		utils2.ValidationError(w, "Invalid action - must be 'approve' or 'reject'")
		return
	}

	// Check if user can approve changes
	canApprove, err := app.CanApproveChange(userEmail, directoryID, req.ChangeID)
	if err != nil {
		log.Printf("Failed to check approval permission: %v", err)
		utils2.InternalServerError(w, "Internal server error")
		return
	}

	if !canApprove {
		utils2.AuthorizationError(w)
		return
	}

	// Process the approval/rejection
	err = app.ProcessChangeApproval(req.ChangeID, userEmail, req.Action, req.Reason)
	if err != nil {
		log.Printf("Failed to process change approval: %v", err)
		utils2.InternalServerError(w, "Failed to process change approval")
		return
	}

	utils2.RespondWithSuccess(w, nil, "Change processed successfully")
}

// handleGetModeratorHierarchy returns the hierarchy of moderators
func (app *App) handleGetModeratorHierarchy(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	directoryID := utils2.GetDirectoryID(r)

	// Get the moderator email to check hierarchy for
	targetEmail := r.URL.Query().Get("moderator_email")
	if targetEmail == "" {
		targetEmail = userEmail // Default to requesting user
	}

	hierarchy, err := app.GetModeratorHierarchy(targetEmail, directoryID)
	if err != nil {
		log.Printf("Failed to get moderator hierarchy: %v", err)
		utils2.InternalServerError(w, "Failed to get moderator hierarchy")
		return
	}

	utils2.RespondWithJSON(w, 200, hierarchy)
}
