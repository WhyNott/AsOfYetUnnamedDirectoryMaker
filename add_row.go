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
