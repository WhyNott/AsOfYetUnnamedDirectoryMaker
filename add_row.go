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

func (app *App) handleAddRow(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	var addRowReq AddRowRequest
	if err := json.NewDecoder(r.Body).Decode(&addRowReq); err != nil {
		log.Printf("Failed to decode add row request: %v", err)
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	// Sanitize input data
	for i, value := range addRowReq.Data {
		addRowReq.Data[i] = SanitizeInput(value)
	}

	// Validate the row data
	if err := ValidateRowData(addRowReq.Data); err != nil {
		log.Printf("Invalid row data in add request: %v", err)
		utils2.ValidationError(w, err.Error())
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

	if userType == UserTypeModerator {
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
			// Create pending change for row addition
			err = app.createPendingAddRow(directoryID, addRowReq.Data, userEmail)
			if err != nil {
				log.Printf("Failed to create pending add row: %v", err)
				utils2.InternalServerError(w, "Failed to submit change for approval")
				return
			}
			utils2.RespondWithSuccess(w, nil, "Row addition submitted for approval")
			return
		}
	} else if userType != UserTypeOwner && userType != UserTypeAdmin {
		utils2.AuthorizationError(w)
		return
	}

	// For owners/admins or moderators without approval requirement, add directly
	// Try to add the row to the original Google Sheet
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.addRowToSheet(ctx, addRowReq.Data, directoryID)
	}()

	utils2.RespondWithSuccess(w, nil, "Row added successfully")
}

func (app *App) addRowToSheet(ctx context.Context, rowData []string, directoryID string) {
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
		fmt.Printf("Context cancelled while adding row to sheet: %v\n", ctx.Err())
		return
	default:
	}

	//TODO: These errors should be communicated to the user!!!!!
	if err != nil {
		fmt.Printf("No admin session found for adding row to sheet: %v\n", err)
		return
	}

	spreadsheetID, err := extractSpreadsheetID(sheetURL)
	if err != nil {
		fmt.Printf("Failed to extract spreadsheet ID: %v\n", err)
		return
	}

	// Decrypt the token
	tokenJSON, err := app.EncryptionService.Decrypt(encryptedTokenJSON)
	if err != nil {
		fmt.Printf("Failed to decrypt token: %v\n", err)
		return
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		fmt.Printf("Failed to unmarshal token: %v\n", err)
		return
	}

	// Append the row to the sheet
	if err := app.appendRowToSheet(spreadsheetID, rowData, &token); err != nil {
		fmt.Printf("Failed to append row to sheet: %v\n", err)
		return
	}

	fmt.Printf("Successfully added new row to sheet with data: %v\n", rowData)

	// Re-import the sheet to refresh our database
	if err := app.reimportSheet(ctx, spreadsheetID, &token, directoryID); err != nil {
		fmt.Printf("Failed to re-import sheet after adding row: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data after adding row")
	}
}

// createPendingAddRow creates a pending change for row addition
func (app *App) createPendingAddRow(directoryID string, rowData []string, submittedBy string) error {
	// Get current column schema
	columnSchema, err := app.getCurrentColumnSchema(directoryID)
	if err != nil {
		return fmt.Errorf("failed to get column schema: %v", err)
	}

	columnSchemaJSON, err := json.Marshal(columnSchema)
	if err != nil {
		return fmt.Errorf("failed to marshal column schema: %v", err)
	}

	// Convert row data to JSON for storage
	rowDataJSON, err := json.Marshal(rowData)
	if err != nil {
		return fmt.Errorf("failed to marshal row data: %v", err)
	}

	// Insert pending change (use row_id = -1 for new rows)
	_, err = app.DB.Exec(`
		INSERT INTO pending_changes 
		(directory_id, row_id, column_name, old_value, new_value, change_type, submitted_by, column_schema, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, directoryID, -1, "new_row", "", string(rowDataJSON), ChangeTypeAdd, submittedBy, string(columnSchemaJSON), time.Now())

	if err != nil {
		return fmt.Errorf("failed to insert pending add row: %v", err)
	}

	return nil
}
