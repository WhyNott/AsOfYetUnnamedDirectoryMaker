package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// GetUserType returns the type of user (admin, owner, moderator, or none)
func (app *App) GetUserType(userEmail, directoryID string) (string, error) {
	// Check admin first
	isAdmin, err := app.IsAdmin(userEmail)
	if err != nil {
		return "", err
	}
	if isAdmin {
		return UserTypeOwner, nil
	}
	
	// Check directory owner
	isOwner, err := app.IsDirectoryOwner(directoryID, userEmail)
	if err != nil {
		return "", err
	}
	if isOwner {
		return UserTypeOwner, nil
	}
	
	// Check moderator
	isModerator, err := app.IsModerator(userEmail, directoryID)
	if err != nil {
		return "", err
	}
	if isModerator {
		return UserTypeModerator, nil
	}
	
	return "", nil // No special permissions
}

// IsModerator checks if a user is a moderator for a specific directory
func (app *App) IsModerator(userEmail, directoryID string) (bool, error) {
	var count int
	err := app.DB.QueryRow(`
		SELECT COUNT(*) FROM moderators 
		WHERE user_email = ? AND directory_id = ? AND is_active = TRUE
	`, userEmail, directoryID).Scan(&count)
	
	if err != nil {
		return false, WrapDatabaseError(ErrTypeConnection, "failed to check moderator status", err)
	}
	
	return count > 0, nil
}

// AppointModerator appoints a new moderator
func (app *App) AppointModerator(appointedBy, appointedByType string, request AppointModeratorRequest) error {
	// Validate the appointer has permission
	canAppoint, err := app.CanAppointModerator(appointedBy, appointedByType, request.DirectoryID)
	if err != nil {
		return err
	}
	if !canAppoint {
		return fmt.Errorf("user does not have permission to appoint moderators")
	}
	
	// Start transaction
	tx, err := app.DB.Begin()
	if err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to begin transaction", err)
	}
	defer tx.Rollback()
	
	// Create or update user profile
	err = app.createOrUpdateUserProfile(tx, request.UserEmail, request.Username, request.AuthProvider)
	if err != nil {
		return err
	}
	
	// Insert moderator record
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO moderators 
		(user_email, username, auth_provider, directory_id, appointed_by, appointed_by_type, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, TRUE, ?, ?)
	`, request.UserEmail, request.Username, request.AuthProvider, request.DirectoryID, 
		appointedBy, appointedByType, time.Now(), time.Now())
		
	if err != nil {
		return WrapDatabaseError(ErrTypeConstraint, "failed to create moderator", err)
	}
	
	// Create moderator domain (permissions and row access)
	rowFilterJSON, err := json.Marshal(request.RowFilter)
	if err != nil {
		return fmt.Errorf("failed to marshal row filter: %v", err)
	}
	
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO moderator_domains 
		(moderator_email, directory_id, row_filter, can_edit, can_approve, requires_approval, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, request.UserEmail, request.DirectoryID, string(rowFilterJSON), 
		request.CanEdit, request.CanApprove, request.RequiresApproval, time.Now(), time.Now())
		
	if err != nil {
		return WrapDatabaseError(ErrTypeConstraint, "failed to create moderator domain", err)
	}
	
	// If appointed by another moderator, create hierarchy relationship
	if appointedByType == UserTypeModerator {
		_, err = tx.Exec(`
			INSERT OR IGNORE INTO moderator_hierarchy 
			(parent_moderator_email, child_moderator_email, directory_id, created_at)
			VALUES (?, ?, ?, ?)
		`, appointedBy, request.UserEmail, request.DirectoryID, time.Now())
		
		if err != nil {
			return WrapDatabaseError(ErrTypeConstraint, "failed to create hierarchy relationship", err)
		}
	}
	
	// Commit transaction
	if err = tx.Commit(); err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to commit transaction", err)
	}
	
	log.Printf("Appointed moderator %s for directory %s by %s (%s)", 
		request.UserEmail, request.DirectoryID, appointedBy, appointedByType)
	
	return nil
}

// CanAppointModerator checks if a user can appoint moderators
func (app *App) CanAppointModerator(userEmail, userType, directoryID string) (bool, error) {
	switch userType {
	case UserTypeAdmin:
		return true, nil
	case UserTypeOwner:
		// Check if they're owner of this directory
		return app.IsDirectoryOwner(directoryID, userEmail)
	case UserTypeModerator:
		// Moderators can appoint other moderators if they have approval permissions
		return app.moderatorCanApprove(userEmail, directoryID)
	default:
		return false, nil
	}
}

// moderatorCanApprove checks if a moderator has approval permissions
func (app *App) moderatorCanApprove(userEmail, directoryID string) (bool, error) {
	var canApprove bool
	err := app.DB.QueryRow(`
		SELECT can_approve FROM moderator_domains 
		WHERE moderator_email = ? AND directory_id = ?
	`, userEmail, directoryID).Scan(&canApprove)
	
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, WrapDatabaseError(ErrTypeConnection, "failed to check moderator permissions", err)
	}
	
	return canApprove, nil
}

// GetModeratorsByDirectory returns all moderators for a directory
func (app *App) GetModeratorsByDirectory(directoryID string) ([]Moderator, error) {
	rows, err := app.DB.Query(`
		SELECT id, user_email, username, auth_provider, directory_id, appointed_by, 
		       appointed_by_type, is_active, created_at, updated_at
		FROM moderators 
		WHERE directory_id = ? AND is_active = TRUE
		ORDER BY created_at
	`, directoryID)
	
	if err != nil {
		return nil, WrapDatabaseError(ErrTypeConnection, "failed to query moderators", err)
	}
	defer rows.Close()
	
	var moderators []Moderator
	for rows.Next() {
		var mod Moderator
		err := rows.Scan(&mod.ID, &mod.UserEmail, &mod.Username, &mod.AuthProvider,
			&mod.DirectoryID, &mod.AppointedBy, &mod.AppointedByType, &mod.IsActive,
			&mod.CreatedAt, &mod.UpdatedAt)
		if err != nil {
			return nil, WrapDatabaseError(ErrTypeConnection, "failed to scan moderator", err)
		}
		moderators = append(moderators, mod)
	}
	
	return moderators, nil
}

// GetModeratorPermissions returns a moderator's permissions and domain
func (app *App) GetModeratorPermissions(userEmail, directoryID string) (*ModeratorPermissions, error) {
	var domain ModeratorDomain
	err := app.DB.QueryRow(`
		SELECT row_filter, can_edit, can_approve, requires_approval
		FROM moderator_domains 
		WHERE moderator_email = ? AND directory_id = ?
	`, userEmail, directoryID).Scan(&domain.RowFilter, &domain.CanEdit, &domain.CanApprove, &domain.RequiresApproval)
	
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("moderator permissions not found")
	}
	if err != nil {
		return nil, WrapDatabaseError(ErrTypeConnection, "failed to get moderator permissions", err)
	}
	
	// Parse row filter
	var rowsAllowed []int
	if domain.RowFilter != "" {
		err = json.Unmarshal([]byte(domain.RowFilter), &rowsAllowed)
		if err != nil {
			log.Printf("Failed to parse row filter for moderator %s: %v", userEmail, err)
			rowsAllowed = []int{} // Default to empty if parsing fails
		}
	}
	
	return &ModeratorPermissions{
		CanEdit:          domain.CanEdit,
		CanApprove:       domain.CanApprove,
		RequiresApproval: domain.RequiresApproval,
		RowsAllowed:      rowsAllowed,
	}, nil
}

// RemoveModerator deactivates a moderator
func (app *App) RemoveModerator(userEmail, directoryID string) error {
	_, err := app.DB.Exec(`
		UPDATE moderators 
		SET is_active = FALSE, updated_at = ?
		WHERE user_email = ? AND directory_id = ?
	`, time.Now(), userEmail, directoryID)
	
	if err != nil {
		return WrapDatabaseError(ErrTypeConstraint, "failed to remove moderator", err)
	}
	
	log.Printf("Removed moderator %s from directory %s", userEmail, directoryID)
	return nil
}

// createOrUpdateUserProfile creates or updates a user profile
func (app *App) createOrUpdateUserProfile(tx *sql.Tx, userEmail, username, authProvider string) error {
	// For now, we'll use email as provider_id for Google and username for Twitter
	// In a real implementation, you'd get the actual provider IDs from OAuth
	providerID := userEmail
	if authProvider == AuthProviderTwitter {
		providerID = username
	}
	
	_, err := tx.Exec(`
		INSERT OR REPLACE INTO user_profiles 
		(user_email, username, auth_provider, provider_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, userEmail, username, authProvider, providerID, time.Now(), time.Now())
	
	return err
}

// GetModeratorHierarchy returns the hierarchy of moderators under a given moderator
func (app *App) GetModeratorHierarchy(moderatorEmail, directoryID string) ([]ModeratorHierarchy, error) {
	rows, err := app.DB.Query(`
		SELECT id, parent_moderator_email, child_moderator_email, directory_id, created_at
		FROM moderator_hierarchy 
		WHERE parent_moderator_email = ? AND directory_id = ?
		ORDER BY created_at
	`, moderatorEmail, directoryID)
	
	if err != nil {
		return nil, WrapDatabaseError(ErrTypeConnection, "failed to query moderator hierarchy", err)
	}
	defer rows.Close()
	
	var hierarchy []ModeratorHierarchy
	for rows.Next() {
		var h ModeratorHierarchy
		err := rows.Scan(&h.ID, &h.ParentModeratorEmail, &h.ChildModeratorEmail,
			&h.DirectoryID, &h.CreatedAt)
		if err != nil {
			return nil, WrapDatabaseError(ErrTypeConnection, "failed to scan hierarchy", err)
		}
		hierarchy = append(hierarchy, h)
	}
	
	return hierarchy, nil
}

// CanRemoveModerator checks if a user can remove a specific moderator
func (app *App) CanRemoveModerator(userEmail, userType, moderatorEmail, directoryID string) (bool, error) {
	switch userType {
	case UserTypeAdmin:
		return true, nil
	case UserTypeOwner:
		// Owners can remove moderators from their directory
		return app.IsDirectoryOwner(directoryID, userEmail)
	case UserTypeModerator:
		// Moderators can only remove moderators they appointed
		return app.didModeratorAppoint(userEmail, moderatorEmail, directoryID)
	default:
		return false, nil
	}
}

// didModeratorAppoint checks if a moderator appointed another moderator
func (app *App) didModeratorAppoint(parentEmail, childEmail, directoryID string) (bool, error) {
	var count int
	err := app.DB.QueryRow(`
		SELECT COUNT(*) FROM moderators 
		WHERE user_email = ? AND directory_id = ? AND appointed_by = ? AND appointed_by_type = ?
	`, childEmail, directoryID, parentEmail, UserTypeModerator).Scan(&count)
	
	if err != nil {
		return false, WrapDatabaseError(ErrTypeConnection, "failed to check moderator appointment", err)
	}
	
	return count > 0, nil
}

// GetPendingChanges returns pending changes for a directory
func (app *App) GetPendingChanges(directoryID, moderatorEmail string) ([]PendingChange, error) {
	var query string
	var args []interface{}
	
	if moderatorEmail != "" {
		// For moderators, only show changes in their domain
		query = `
			SELECT pc.id, pc.directory_id, pc.row_id, pc.column_name, pc.old_value, pc.new_value,
			       pc.change_type, pc.submitted_by, pc.status, pc.reviewed_by, pc.reviewed_at,
			       pc.reason, pc.created_at
			FROM pending_changes pc
			INNER JOIN moderator_domains md ON md.moderator_email = ?
			WHERE pc.directory_id = ? AND pc.status = ? AND md.directory_id = pc.directory_id
			ORDER BY pc.created_at DESC
		`
		args = []interface{}{moderatorEmail, directoryID, ChangeStatusPending}
	} else {
		// For admins/super admins, show all pending changes
		query = `
			SELECT id, directory_id, row_id, column_name, old_value, new_value,
			       change_type, submitted_by, status, reviewed_by, reviewed_at,
			       reason, created_at
			FROM pending_changes
			WHERE directory_id = ? AND status = ?
			ORDER BY created_at DESC
		`
		args = []interface{}{directoryID, ChangeStatusPending}
	}
	
	rows, err := app.DB.Query(query, args...)
	if err != nil {
		return nil, WrapDatabaseError(ErrTypeConnection, "failed to query pending changes", err)
	}
	defer rows.Close()
	
	var changes []PendingChange
	for rows.Next() {
		var change PendingChange
		err := rows.Scan(&change.ID, &change.DirectoryID, &change.RowID, &change.ColumnName,
			&change.OldValue, &change.NewValue, &change.ChangeType, &change.SubmittedBy,
			&change.Status, &change.ReviewedBy, &change.ReviewedAt, &change.Reason, &change.CreatedAt)
		if err != nil {
			return nil, WrapDatabaseError(ErrTypeConnection, "failed to scan pending change", err)
		}
		changes = append(changes, change)
	}
	
	return changes, nil
}

// CanApproveChange checks if a user can approve a specific change
func (app *App) CanApproveChange(userEmail, directoryID string, changeID int) (bool, error) {
	userType, err := app.GetUserType(userEmail, directoryID)
	if err != nil {
		return false, err
	}
	
	switch userType {
	case UserTypeAdmin, UserTypeOwner:
		return true, nil
	case UserTypeModerator:
		// Check if moderator has approval permissions and the change is in their domain
		return app.moderatorCanApproveSpecificChange(userEmail, directoryID, changeID)
	default:
		return false, nil
	}
}

// moderatorCanApproveSpecificChange checks if a moderator can approve a specific change
func (app *App) moderatorCanApproveSpecificChange(moderatorEmail, directoryID string, changeID int) (bool, error) {
	// First check if moderator has approval permissions
	canApprove, err := app.moderatorCanApprove(moderatorEmail, directoryID)
	if err != nil || !canApprove {
		return false, err
	}
	
	// Get the specific change details to check row access
	var change PendingChange
	err = app.DB.QueryRow(`
		SELECT id, directory_id, row_id, column_name, old_value, new_value, change_type, 
		       submitted_by, status, reviewed_by, reviewed_at, reason, created_at
		FROM pending_changes 
		WHERE id = ? AND directory_id = ?
	`, changeID, directoryID).Scan(
		&change.ID, &change.DirectoryID, &change.RowID, &change.ColumnName,
		&change.OldValue, &change.NewValue, &change.ChangeType,
		&change.SubmittedBy, &change.Status, &change.ReviewedBy,
		&change.ReviewedAt, &change.Reason, &change.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil // Change not found
		}
		return false, WrapDatabaseError(ErrTypeConnection, "failed to get change details", err)
	}
	
	// Get moderator permissions to check row access
	permissions, err := app.GetModeratorPermissions(moderatorEmail, directoryID)
	if err != nil {
		return false, err
	}
	
	// Check if the change affects a row the moderator can access
	// If RowsAllowed is empty, it means they have access to all rows
	if len(permissions.RowsAllowed) == 0 {
		return true, nil
	}
	
	// Check if the affected row is in the moderator's allowed rows
	for _, allowedRow := range permissions.RowsAllowed {
		if allowedRow == change.RowID {
			return true, nil
		}
	}
	
	// Row is not in moderator's domain
	return false, nil
}

// moderatorCanEditSpecificRow checks if a moderator can edit a specific row
func (app *App) moderatorCanEditSpecificRow(moderatorEmail, directoryID string, rowID int) (bool, error) {
	// Get moderator permissions to check edit access
	permissions, err := app.GetModeratorPermissions(moderatorEmail, directoryID)
	if err != nil {
		return false, err
	}
	
	// First check if moderator has edit permissions
	if !permissions.CanEdit {
		return false, nil
	}
	
	// Check if the row is in the moderator's domain
	// If RowsAllowed is empty, it means they have access to all rows
	if len(permissions.RowsAllowed) == 0 {
		return true, nil
	}
	
	// Check if the row is in the moderator's allowed rows
	for _, allowedRow := range permissions.RowsAllowed {
		if allowedRow == rowID {
			return true, nil
		}
	}
	
	// Row is not in moderator's domain
	return false, nil
}

// ProcessChangeApproval processes a change approval or rejection
func (app *App) ProcessChangeApproval(changeID int, reviewerEmail, action, reason string) error {
	tx, err := app.DB.Begin()
	if err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to begin transaction", err)
	}
	defer tx.Rollback()
	
	// Get the pending change
	var change PendingChange
	err = tx.QueryRow(`
		SELECT id, directory_id, row_id, column_name, old_value, new_value, change_type, submitted_by
		FROM pending_changes
		WHERE id = ? AND status = ?
	`, changeID, ChangeStatusPending).Scan(&change.ID, &change.DirectoryID, &change.RowID,
		&change.ColumnName, &change.OldValue, &change.NewValue, &change.ChangeType, &change.SubmittedBy)
	
	if err == sql.ErrNoRows {
		return fmt.Errorf("pending change not found")
	}
	if err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to get pending change", err)
	}
	
	// Update the change status
	status := ChangeStatusRejected
	if action == "approve" {
		status = ChangeStatusApproved
	}
	
	_, err = tx.Exec(`
		UPDATE pending_changes
		SET status = ?, reviewed_by = ?, reviewed_at = ?, reason = ?
		WHERE id = ?
	`, status, reviewerEmail, time.Now(), reason, changeID)
	
	if err != nil {
		return WrapDatabaseError(ErrTypeConstraint, "failed to update change status", err)
	}
	
	// If approved, apply the change to the directory database
	if action == "approve" {
		err = app.applyApprovedChange(tx, change)
		if err != nil {
			return err
		}
	}
	
	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to commit transaction", err)
	}
	
	log.Printf("Change %d %s by %s (reviewer: %s)", changeID, action, reviewerEmail, reviewerEmail)
	return nil
}

// applyApprovedChange applies an approved change to the directory database
func (app *App) applyApprovedChange(tx *sql.Tx, change PendingChange) error {
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(change.DirectoryID)
	if err != nil {
		return fmt.Errorf("failed to get directory database: %v", err)
	}
	
	switch change.ChangeType {
	case ChangeTypeEdit:
		// For edits, we need to update the specific row and rebuild the data JSON
		// This is a simplified implementation - in practice you'd want more sophisticated handling
		var currentData string
		err = db.QueryRow("SELECT data FROM directory WHERE id = ?", change.RowID).Scan(&currentData)
		if err != nil {
			return fmt.Errorf("failed to get current row data: %v", err)
		}
		
		// Parse current data and update the specific column
		var rowData []string
		if err = json.Unmarshal([]byte(currentData), &rowData); err != nil {
			return fmt.Errorf("failed to parse current row data: %v", err)
		}
		
		// TODO: Map column name to column index
		// For now, assume column_name is a number
		columnIndex := 0 // This should be derived from column_name
		
		if columnIndex < len(rowData) {
			rowData[columnIndex] = change.NewValue
		}
		
		newData, err := json.Marshal(rowData)
		if err != nil {
			return fmt.Errorf("failed to marshal updated row data: %v", err)
		}
		
		_, err = db.Exec("UPDATE directory SET data = ? WHERE id = ?", string(newData), change.RowID)
		if err != nil {
			return fmt.Errorf("failed to update directory row: %v", err)
		}
		
	case ChangeTypeAdd:
		// For adds, insert new row
		_, err = db.Exec("INSERT INTO directory (data) VALUES (?)", change.NewValue)
		if err != nil {
			return fmt.Errorf("failed to add directory row: %v", err)
		}
		
	case ChangeTypeDelete:
		// For deletes, remove the row
		_, err = db.Exec("DELETE FROM directory WHERE id = ?", change.RowID)
		if err != nil {
			return fmt.Errorf("failed to delete directory row: %v", err)
		}
	}
	
	return nil
}