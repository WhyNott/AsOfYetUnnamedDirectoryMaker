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

func (app *App) handleCorrection(w http.ResponseWriter, r *http.Request) {
	var correction CorrectionRequest
	if err := json.NewDecoder(r.Body).Decode(&correction); err != nil {
		log.Printf("Failed to decode correction request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate the correction request
	if correction.Row < 0 || correction.Column < 0 {
		log.Printf("Invalid row/column in correction: row=%d, col=%d", correction.Row, correction.Column)
		http.Error(w, "Invalid row or column", http.StatusBadRequest)
		return
	}

	if correction.Column > 49 {
		log.Printf("Column exceeds limit: %d", correction.Column)
		http.Error(w, "Column exceeds maximum allowed (50)", http.StatusBadRequest)
		return
	}

	correction.Value = SanitizeInput(correction.Value)
	if len(correction.Value) > 1000 {
		log.Printf("Correction value too long: %d characters", len(correction.Value))
		http.Error(w, "Value exceeds maximum length (1000 characters)", http.StatusBadRequest)
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

	// Get the current data for the specified row
	var currentData string
	err = db.QueryRow("SELECT data FROM directory ORDER BY id LIMIT 1 OFFSET ?", correction.Row).Scan(&currentData)
	if err != nil {
		log.Printf("Failed to get row %d: %v", correction.Row, err)
		http.Error(w, "Row not found", http.StatusNotFound)
		return
	}

	// Parse the current row data
	var rowData []string
	if err := json.Unmarshal([]byte(currentData), &rowData); err != nil {
		log.Printf("Failed to unmarshal row data: %v", err)
		http.Error(w, "Failed to parse row data", http.StatusInternalServerError)
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
		http.Error(w, "Invalid row data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Save the updated row back to the database
	updatedData, err := json.Marshal(rowData)
	if err != nil {
		log.Printf("Failed to marshal updated row data: %v", err)
		http.Error(w, "Failed to process row data", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(`
		UPDATE directory 
		SET data = ? 
		WHERE id = (
			SELECT id FROM directory ORDER BY id LIMIT 1 OFFSET ?
		)
	`, string(updatedData), correction.Row)

	if err != nil {
		log.Printf("Failed to update database for correction: %v", err)
		http.Error(w, "Failed to update database", http.StatusInternalServerError)
		return
	}

	// Try to update the original Google Sheet if we have the information
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	go func() {
		defer cancel()
		app.updateOriginalSheet(ctx, correction.Row, correction.Column, correction.Value, directoryID)
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
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
	if err := app.importFromSheet(ctx, spreadsheetID, &token, directoryID); err != nil {
		fmt.Printf("Failed to re-import sheet after update: %v\n", err)
	} else {
		fmt.Println("Successfully re-imported sheet data")
	}
}
