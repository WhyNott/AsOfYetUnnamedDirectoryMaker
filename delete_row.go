package main

import (
	"context"
	utils2 "directoryCommunityWebsite/internal/utils"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

func (app *App) handleDeleteRow(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	var deleteRowReq DeleteRowRequest
	if err := json.NewDecoder(r.Body).Decode(&deleteRowReq); err != nil {
		log.Printf("Failed to decode delete row request: %v", err)
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	// Validate the delete request
	if deleteRowReq.Row < 0 {
		log.Printf("Invalid row in delete request: row=%d", deleteRowReq.Row)
		utils2.ValidationError(w, "Invalid row number")
		return
	}

	// Sanitize the reason
	deleteRowReq.Reason = SanitizeInput(deleteRowReq.Reason)
	if len(deleteRowReq.Reason) > 500 {
		log.Printf("Delete reason too long: %d characters", len(deleteRowReq.Reason))
		utils2.ValidationError(w, "Reason exceeds maximum length (500 characters)")
		return
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := utils2.GetDirectoryID(r)

	// Check user permissions and apply moderation workflow
	userType, err := app.GetUserType(userEmail, directoryID)
	if err != nil {
		log.Printf("Failed to get user type: %v", err)
		utils2.InternalServerError(w, "Permission check failed")
		return
	}

	// Get actual row ID from the row index
	actualRowID, err := app.getRowIDFromIndex(directoryID, deleteRowReq.Row)
	if err != nil {
		log.Printf("Failed to get row ID from index %d: %v", deleteRowReq.Row, err)
		utils2.NotFoundError(w, "Row")
		return
	}

	if userType == UserTypeModerator {
		// Check if moderator can access this row
		filter := NewModerationFilter(app)
		canAccess, err := filter.CanAccessRow(userEmail, directoryID, actualRowID)
		if err != nil {
			log.Printf("Failed to check row access: %v", err)
			utils2.InternalServerError(w, "Permission check failed")
			return
		}
		if !canAccess {
			utils2.AuthorizationError(w)
			return
		}

		// Check if moderator's changes require approval
		permissions, err := app.GetModeratorPermissions(userEmail, directoryID)
		if err != nil {
			log.Printf("Failed to get moderator permissions: %v", err)
			utils2.InternalServerError(w, "Permission check failed")
			return
		}

		if !permissions.CanEdit {
			utils2.AuthorizationError(w)
			return
		}

		if permissions.RequiresApproval {
			// Create pending change for row deletion
			err = app.createPendingDeleteRow(directoryID, actualRowID, deleteRowReq.Reason, userEmail)
			if err != nil {
				log.Printf("Failed to create pending delete row: %v", err)
				utils2.InternalServerError(w, "Failed to submit change for approval")
				return
			}
			utils2.RespondWithSuccess(w, nil, "Row deletion submitted for approval")
			return
		}
	} else if userType != UserTypeOwner && userType != UserTypeAdmin {
		utils2.AuthorizationError(w)
		return
	}

	// For owners/admins or moderators without approval requirement, delete directly
	// Try to delete the row from the original Google Sheet
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.deleteRowFromSheet(ctx, deleteRowReq.Row, deleteRowReq.Reason, directoryID)
	}()

	utils2.RespondWithSuccess(w, nil, "Row deleted successfully")
}

func (app *App) deleteRowFromSheet(ctx context.Context, rowIndex int, reason string, directoryID string) {
	// Get the latest admin session with a sheet URL
	var userEmail, encryptedTokenJSON, sheetURL string
	err := app.DB.QueryRow(`
		SELECT user_email, token, sheet_url 
		FROM admin_sessions 
		WHERE sheet_url IS NOT NULL AND sheet_url != ''
		ORDER BY created_at DESC 
		LIMIT 1
	`).Scan(&userEmail, &encryptedTokenJSON, &sheetURL)

	select {
	case <-ctx.Done():
		fmt.Printf("Context cancelled while deleting row from sheet: %v\n", ctx.Err())
		return
	default:
	}

	if err != nil {
		fmt.Printf("No admin session found for deleting row from sheet: %v\n", err)
		return
	}

	spreadsheetID, err := extractSpreadsheetID(sheetURL)
	if err != nil {
		fmt.Printf("Failed to extract spreadsheet ID for deletion: %v\n", err)
		return
	}

	// Decrypt the token
	tokenJSON, err := app.EncryptionService.Decrypt(encryptedTokenJSON)
	if err != nil {
		fmt.Printf("Failed to decrypt token for deletion: %v\n", err)
		return
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		fmt.Printf("Failed to unmarshal token for deletion: %v\n", err)
		return
	}

	refreshedToken, err := app.refreshTokenIfNeeded(&token)
	if err != nil {
		fmt.Printf("Failed to refresh token for deletion: %v\n", err)
		return
	}
	token = *refreshedToken

	// Delete the row from the sheet (we'll add 2 to account for header row and 0-indexing)
	if err := app.deleteSheetRow(spreadsheetID, rowIndex+2, &token); err != nil {
		fmt.Printf("Failed to delete row %d from sheet: %v\n", rowIndex, err)
		return
	}

	fmt.Printf("Successfully deleted row %d from sheet. Reason: %s\n", rowIndex, reason)

	// Re-import the sheet to refresh our database
	if err := app.reimportSheet(ctx, spreadsheetID, &token, directoryID); err != nil {
		fmt.Printf("Failed to re-import sheet after deletion: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data after deletion")
	}
}

// createPendingDeleteRow creates a pending change for row deletion
func (app *App) createPendingDeleteRow(directoryID string, rowID int, reason, submittedBy string) error {
	// Get current column schema
	columnSchema, err := app.getCurrentColumnSchema(directoryID)
	if err != nil {
		return fmt.Errorf("failed to get column schema: %v", err)
	}

	columnSchemaJSON, err := json.Marshal(columnSchema)
	if err != nil {
		return fmt.Errorf("failed to marshal column schema: %v", err)
	}

	// Get the current row data to store as old_value
	oldRowData, err := app.getFullRowData(directoryID, rowID)
	if err != nil {
		return fmt.Errorf("failed to get row data for deletion: %v", err)
	}

	oldRowDataJSON, err := json.Marshal(oldRowData)
	if err != nil {
		return fmt.Errorf("failed to marshal old row data: %v", err)
	}

	// Insert pending change
	_, err = app.DB.Exec(`
		INSERT INTO pending_changes 
		(directory_id, row_id, column_name, old_value, new_value, change_type, submitted_by, reason, column_schema, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, directoryID, rowID, "full_row", string(oldRowDataJSON), "", ChangeTypeDelete, submittedBy, reason, string(columnSchemaJSON), time.Now())

	if err != nil {
		return fmt.Errorf("failed to insert pending delete row: %v", err)
	}

	return nil
}

// getFullRowData gets the complete row data as a string array
func (app *App) getFullRowData(directoryID string, rowID int) ([]string, error) {
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory database: %v", err)
	}

	var dataJSON string
	err = db.QueryRow("SELECT data FROM directory WHERE id = ?", rowID).Scan(&dataJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to get row data: %v", err)
	}

	var rowData []string
	if err := json.Unmarshal([]byte(dataJSON), &rowData); err != nil {
		return nil, fmt.Errorf("failed to parse row data: %v", err)
	}

	return rowData, nil
}
