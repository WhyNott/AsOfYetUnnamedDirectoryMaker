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

	// Add the row to our local database first
	jsonData, err := json.Marshal(addRowReq.Data)
	if err != nil {
		log.Printf("Failed to marshal row data: %v", err)
		http.Error(w, "Failed to process row data", http.StatusInternalServerError)
		return
	}

	_, err = app.DB.Exec("INSERT INTO directory (data) VALUES (?)", string(jsonData))
	if err != nil {
		log.Printf("Failed to insert row into database: %v", err)
		http.Error(w, "Failed to add row to database", http.StatusInternalServerError)
		return
	}

	// Try to add the row to the original Google Sheet
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.addRowToSheet(ctx, addRowReq.Data)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (app *App) addRowToSheet(ctx context.Context, rowData []string) {
	// Get the latest admin session with a sheet URL
	var userEmail, tokenJSON, sheetURL string
	err := app.DB.QueryRow(`
		SELECT user_email, token, sheet_url 
		FROM admin_sessions 
		WHERE sheet_url IS NOT NULL AND sheet_url != ''
		ORDER BY created_at DESC 
		LIMIT 1
	`).Scan(&userEmail, &tokenJSON, &sheetURL)

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
	if err := app.importFromSheet(ctx, spreadsheetID, &token); err != nil {
		fmt.Printf("Failed to re-import sheet after adding row: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data after adding row")
	}
}
