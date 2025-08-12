package main

import (
	"context"
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
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Sanitize input data
	for i, value := range addRowReq.Data {
		addRowReq.Data[i] = SanitizeInput(value)
	}

	// Validate the row data
	if err := ValidateRowData(addRowReq.Data); err != nil {
		log.Printf("Invalid row data in add request: %v", err)
		http.Error(w, "Invalid row data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := GetCurrentDirectoryID(r)
	
	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		log.Printf("Failed to get directory database for %s: %v", directoryID, err)
		http.Error(w, "Directory not found", http.StatusNotFound)
		return
	}

	// Add the row to the directory-specific database
	jsonData, err := json.Marshal(addRowReq.Data)
	if err != nil {
		log.Printf("Failed to marshal row data: %v", err)
		http.Error(w, "Failed to process row data", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("INSERT INTO directory (data) VALUES (?)", string(jsonData))
	if err != nil {
		log.Printf("Failed to insert row into database: %v", err)
		http.Error(w, "Failed to add row to database", http.StatusInternalServerError)
		return
	}

	// Try to add the row to the original Google Sheet
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.addRowToSheet(ctx, addRowReq.Data, directoryID)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
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
	if err := app.importFromSheet(ctx, spreadsheetID, &token, directoryID); err != nil {
		fmt.Printf("Failed to re-import sheet after adding row: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data after adding row")
	}
}
