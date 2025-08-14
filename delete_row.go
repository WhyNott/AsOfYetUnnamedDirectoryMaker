package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"directoryCommunityWebsite/utils"
	"golang.org/x/oauth2"
)

func (app *App) handleDeleteRow(w http.ResponseWriter, r *http.Request) {
	var deleteRowReq DeleteRowRequest
	if err := json.NewDecoder(r.Body).Decode(&deleteRowReq); err != nil {
		log.Printf("Failed to decode delete row request: %v", err)
		utils.BadRequestError(w, "Invalid request body")
		return
	}

	// Validate the delete request
	if deleteRowReq.Row < 0 {
		log.Printf("Invalid row in delete request: row=%d", deleteRowReq.Row)
		utils.ValidationError(w, "Invalid row number")
		return
	}

	// Sanitize the reason
	deleteRowReq.Reason = SanitizeInput(deleteRowReq.Reason)
	if len(deleteRowReq.Reason) > 500 {
		log.Printf("Delete reason too long: %d characters", len(deleteRowReq.Reason))
		utils.ValidationError(w, "Reason exceeds maximum length (500 characters)")
		return
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := utils.GetDirectoryID(r)
	
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		log.Printf("Failed to get directory database for %s: %v", directoryID, err)
		utils.NotFoundError(w, "Directory")
		return
	}

	// Check if the row exists
	var rowCount int
	err = db.QueryRow("SELECT COUNT(*) FROM directory").Scan(&rowCount)
	if err != nil {
		log.Printf("Failed to count directory rows: %v", err)
		utils.DatabaseError(w)
		return
	}

	if deleteRowReq.Row >= rowCount {
		log.Printf("Row %d does not exist (total rows: %d)", deleteRowReq.Row, rowCount)
		utils.NotFoundError(w, "Row")
		return
	}

	// Get the row data before deletion for logging
	var deletedData string
	err = db.QueryRow("SELECT data FROM directory ORDER BY id LIMIT 1 OFFSET ?", deleteRowReq.Row).Scan(&deletedData)
	if err != nil {
		log.Printf("Failed to get row %d for deletion: %v", deleteRowReq.Row, err)
		utils.NotFoundError(w, "Row")
		return
	}

	// Delete the row from directory-specific database
	_, err = db.Exec(`
		DELETE FROM directory 
		WHERE id = (
			SELECT id FROM directory ORDER BY id LIMIT 1 OFFSET ?
		)
	`, deleteRowReq.Row)

	if err != nil {
		log.Printf("Failed to delete row from database: %v", err)
		utils.InternalServerError(w, "Failed to delete row from database")
		return
	}

	// Log the deletion for audit purposes
	log.Printf("Row %d deleted. Data: %s, Reason: %s", deleteRowReq.Row, deletedData, deleteRowReq.Reason)

	// Try to delete the row from the original Google Sheet
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.deleteRowFromSheet(ctx, deleteRowReq.Row, deletedData, deleteRowReq.Reason, directoryID)
	}()

	utils.RespondWithSuccess(w, nil, "Row deleted successfully")
}

func (app *App) deleteRowFromSheet(ctx context.Context, rowIndex int, deletedData, reason string, directoryID string) {
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

	fmt.Printf("Successfully deleted row %d from sheet. Data: %s, Reason: %s\n", rowIndex, deletedData, reason)

	// Re-import the sheet to refresh our database
	if err := app.importFromSheet(ctx, spreadsheetID, &token, directoryID); err != nil {
		fmt.Printf("Failed to re-import sheet after deletion: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data after deletion")
	}
}
