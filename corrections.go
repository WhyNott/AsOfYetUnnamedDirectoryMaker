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

func (app *App) handleCorrection(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	var correction CorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&correction); err != nil {
		log.Printf("Failed to decode correction request: %v", err)
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	// Validate the correction request
	if correction.Row < 0 || correction.Column < 0 {
		log.Printf("Invalid row/column in correction: row=%d, col=%d", correction.Row, correction.Column)
		utils2.ValidationError(w, "Invalid row or column")
		return
	}

	if correction.Column > 49 {
		log.Printf("Column exceeds limit: %d", correction.Column)
		utils2.ValidationError(w, "Column exceeds maximum allowed (50)")
		return
	}

	correction.Value = SanitizeInput(correction.Value)
	if len(correction.Value) > 1000 {
		log.Printf("Correction value too long: %d characters", len(correction.Value))
		utils2.ValidationError(w, "Value exceeds maximum length (1000 characters)")
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
	actualRowID, err := app.getRowIDFromIndex(directoryID, correction.Row)
	if err != nil {
		log.Printf("Failed to get row ID from index %d: %v", correction.Row, err)
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
			// Create pending change instead of direct update
			err = app.createPendingChange(directoryID, actualRowID, correction.Column, correction.Value, ChangeTypeEdit, userEmail)
			if err != nil {
				log.Printf("Failed to create pending change: %v", err)
				utils2.InternalServerError(w, "Failed to submit change for approval")
				return
			}
			utils2.RespondWithSuccess(w, nil, "Change submitted for approval")
			return
		}
	} else if userType != UserTypeOwner && userType != UserTypeAdmin {
		utils2.AuthorizationError(w)
		return
	}

	// For owners/admins or moderators without approval requirement, apply directly
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		log.Printf("Failed to get directory database for %s: %v", directoryID, err)
		utils2.NotFoundError(w, "Directory")
		return
	}

	// Get the current data for the specified row
	var currentData string
	err = db.QueryRow("SELECT data FROM directory ORDER BY id LIMIT 1 OFFSET ?", correction.Row).Scan(&currentData)
	if err != nil {
		log.Printf("Failed to get row %d: %v", correction.Row, err)
		utils2.NotFoundError(w, "Row")
		return
	}

	// Parse the current row data
	var rowData []string
	if err := json.Unmarshal([]byte(currentData), &rowData); err != nil {
		log.Printf("Failed to unmarshal row data: %v", err)
		utils2.InternalServerError(w, "Failed to parse row data")
		return
	}

	// Extend the row data if necessary
	for len(rowData) <= correction.Column {
		rowData = append(rowData, "")
	}

	// Update the specific column
	rowData[correction.Column] = correction.Value

	// Validate the updated row data
	if err := ValidateRowData(rowData); err != nil {
		log.Printf("Invalid row data after correction: %v", err)
		utils2.ValidationError(w, err.Error())
		return
	}

	// Try to update the original Google Sheet if we have the information
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.updateOriginalSheet(ctx, correction.Row, correction.Column, correction.Value, directoryID)
	}()

	utils2.RespondWithSuccess(w, nil, "Correction applied successfully")
}

func (app *App) updateOriginalSheet(ctx context.Context, row, col int, value string, directoryID string) {
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
		fmt.Printf("Context cancelled while updating sheet: %v\n", ctx.Err())
		return
	default:
	}

	if err != nil {
		fmt.Printf("No admin session found for sheet update: %v\n", err)
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

	// Update the sheet cell
	if err := app.updateSheetCell(spreadsheetID, row, col, value, &token); err != nil {
		fmt.Printf("Failed to update sheet cell: %v\n", err)
		return
	}

	fmt.Printf("Successfully updated sheet cell at row %d, column %d with value: %s\n", row, col, value)

	// Re-import the sheet to refresh our database
	if err := app.reimportSheet(ctx, spreadsheetID, &token, directoryID); err != nil {
		fmt.Printf("Failed to re-import sheet after update: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data")
	}
}

// getRowIDFromIndex converts a row index to the actual database row ID
func (app *App) getRowIDFromIndex(directoryID string, rowIndex int) (int, error) {
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return 0, fmt.Errorf("failed to get directory database: %v", err)
	}

	var rowID int
	err = db.QueryRow("SELECT id FROM directory ORDER BY id LIMIT 1 OFFSET ?", rowIndex).Scan(&rowID)
	if err != nil {
		return 0, fmt.Errorf("failed to get row ID: %v", err)
	}

	return rowID, nil
}

// createPendingChange creates a new pending change record
func (app *App) createPendingChange(directoryID string, rowID, columnIndex int, newValue, changeType, submittedBy string) error {
	// Get current column schema
	columnSchema, err := app.getCurrentColumnSchema(directoryID)
	if err != nil {
		return fmt.Errorf("failed to get column schema: %v", err)
	}

	columnSchemaJSON, err := json.Marshal(columnSchema)
	if err != nil {
		return fmt.Errorf("failed to marshal column schema: %v", err)
	}

	// Get column name from index
	var columnName string
	if columnIndex < len(columnSchema) {
		columnName = columnSchema[columnIndex]
	} else {
		columnName = fmt.Sprintf("Column_%d", columnIndex)
	}

	// Get old value if this is an edit
	var oldValue string
	if changeType == ChangeTypeEdit {
		oldValue, err = app.getColumnValue(directoryID, rowID, columnIndex)
		if err != nil {
			log.Printf("Failed to get old value for pending change: %v", err)
			// Continue anyway, just leave old value empty
		}
	}

	// Insert pending change
	_, err = app.DB.Exec(`
		INSERT INTO pending_changes 
		(directory_id, row_id, column_name, old_value, new_value, change_type, submitted_by, column_schema, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, directoryID, rowID, columnName, oldValue, newValue, changeType, submittedBy, string(columnSchemaJSON), time.Now())

	if err != nil {
		return fmt.Errorf("failed to insert pending change: %v", err)
	}

	return nil
}

// getCurrentColumnSchema gets the current column names for a directory
func (app *App) getCurrentColumnSchema(directoryID string) ([]string, error) {
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory database: %v", err)
	}

	rows, err := db.Query(`
		SELECT columnName 
		FROM _meta_directory_column_types 
		WHERE columnTable = ? 
		ORDER BY rowid
	`, directoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to query column names: %v", err)
	}
	defer rows.Close()

	var columnNames []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, fmt.Errorf("failed to scan column name: %v", err)
		}
		columnNames = append(columnNames, columnName)
	}

	return columnNames, nil
}

// getColumnValue gets the current value of a specific column in a row
func (app *App) getColumnValue(directoryID string, rowID, columnIndex int) (string, error) {
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return "", fmt.Errorf("failed to get directory database: %v", err)
	}

	var dataJSON string
	err = db.QueryRow("SELECT data FROM directory WHERE id = ?", rowID).Scan(&dataJSON)
	if err != nil {
		return "", fmt.Errorf("failed to get row data: %v", err)
	}

	var rowData []string
	if err := json.Unmarshal([]byte(dataJSON), &rowData); err != nil {
		return "", fmt.Errorf("failed to parse row data: %v", err)
	}

	if columnIndex >= len(rowData) {
		return "", nil // Column doesn't exist yet
	}

	return rowData[columnIndex], nil
}
