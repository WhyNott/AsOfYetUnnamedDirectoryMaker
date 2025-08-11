package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func (app *App) handleImport(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := r.Context().Value(UserEmailKey).(string)
	if !ok {
		log.Printf("User email not found in context")
		http.Error(w, "Authentication error", http.StatusInternalServerError)
		return
	}

	sheetURL := SanitizeInput(r.FormValue("sheet_url"))
	if sheetURL == "" {
		http.Error(w, "Sheet URL is required", http.StatusBadRequest)
		return
	}

	if !ValidateSheetURL(sheetURL) {
		log.Printf("Invalid sheet URL provided: %s", sheetURL)
		http.Error(w, "Invalid Google Sheets URL format", http.StatusBadRequest)
		return
	}

	spreadsheetID, err := extractSpreadsheetID(sheetURL)
	if err != nil {
		log.Printf("Failed to extract spreadsheet ID from URL %s: %v", sheetURL, err)
		http.Error(w, "Invalid Google Sheets URL", http.StatusBadRequest)
		return
	}

	var tokenJSON string
	err = app.DB.QueryRow("SELECT token FROM admin_sessions WHERE user_email = ?", userEmail).Scan(&tokenJSON)
	if err != nil {
		log.Printf("Failed to get token for user %s: %v", userEmail, err)
		http.Error(w, "Session not found", http.StatusInternalServerError)
		return
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		log.Printf("Failed to unmarshal token: %v", err)
		http.Error(w, "Invalid token", http.StatusInternalServerError)
		return
	}

	refreshedToken, err := app.refreshTokenIfNeeded(&token)
	if err != nil {
		log.Printf("Failed to refresh token: %v", err)
		http.Error(w, "Token refresh failed", http.StatusInternalServerError)
		return
	}
	token = *refreshedToken

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := app.importFromSheet(ctx, spreadsheetID, &token); err != nil {
		log.Printf("Failed to import sheet %s: %v", spreadsheetID, err)
		http.Error(w, fmt.Sprintf("Failed to import sheet: %v", err), http.StatusInternalServerError)
		return
	}

	_, err = app.DB.Exec("UPDATE admin_sessions SET sheet_url = ? WHERE user_email = ?", sheetURL, userEmail)
	if err != nil {
		log.Printf("Failed to save sheet URL for user %s: %v", userEmail, err)
		http.Error(w, "Failed to save sheet URL", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin?imported=true", http.StatusTemporaryRedirect)
}

func (app *App) importFromSheet(ctx context.Context, spreadsheetID string, token *oauth2.Token) error {
	client := app.OAuthConfig.Client(ctx, token)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("unable to retrieve Sheets client: %v", err)
	}

	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, app.Config.SheetRange).Do()
	if err != nil {
		return fmt.Errorf("unable to retrieve data from sheet: %v", err)
	}

	if len(resp.Values) == 0 {
		return fmt.Errorf("no data found in sheet")
	}

	if _, err := app.DB.Exec("DELETE FROM directory"); err != nil {
		return fmt.Errorf("failed to clear directory table: %v", err)
	}

	for i, row := range resp.Values {
		if i == 0 {
			continue
		}

		rowData := make([]string, len(row))
		for j, cell := range row {
			if cell != nil {
				cellStr := fmt.Sprintf("%v", cell)
				rowData[j] = SanitizeInput(cellStr)
			}
		}

		if err := ValidateRowData(rowData); err != nil {
			log.Printf("Skipping invalid row %d: %v", i, err)
			continue
		}

		jsonData, err := json.Marshal(rowData)
		if err != nil {
			return fmt.Errorf("failed to marshal row data: %v", err)
		}

		_, err = app.DB.Exec("INSERT INTO directory (data) VALUES (?)", string(jsonData))
		if err != nil {
			return fmt.Errorf("failed to insert row %d: %v", i, err)
		}
	}

	return nil
}

func (app *App) updateSheetCell(spreadsheetID string, row, col int, value string, token *oauth2.Token) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := app.OAuthConfig.Client(ctx, token)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("unable to retrieve Sheets client: %v", err)
	}

	cellRange := fmt.Sprintf("%s%d", columnIndexToLetter(col), row+2)

	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{{value}},
	}

	_, err = srv.Spreadsheets.Values.Update(spreadsheetID, cellRange, valueRange).
		ValueInputOption("USER_ENTERED").Do()

	return err
}

func extractSpreadsheetID(url string) (string, error) {
	re := regexp.MustCompile(`/spreadsheets/d/([a-zA-Z0-9-_]+)`)
	matches := re.FindStringSubmatch(url)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not extract spreadsheet ID from URL")
	}
	return matches[1], nil
}

func (app *App) appendRowToSheet(spreadsheetID string, rowData []string, token *oauth2.Token) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := app.OAuthConfig.Client(ctx, token)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("unable to retrieve Sheets client: %v", err)
	}

	// Convert string slice to interface slice for Google Sheets API
	values := make([]interface{}, len(rowData))
	for i, v := range rowData {
		values[i] = v
	}

	valueRange := &sheets.ValueRange{
		Values: [][]interface{}{values},
	}

	_, err = srv.Spreadsheets.Values.Append(spreadsheetID, "A:Z", valueRange).
		ValueInputOption("USER_ENTERED").
		InsertDataOption("INSERT_ROWS").
		Do()

	return err
}

func columnIndexToLetter(index int) string {
	var result strings.Builder
	for index >= 0 {
		result.WriteByte(byte('A' + index%26))
		index = index/26 - 1
	}

	runes := []rune(result.String())
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func (app *App) refreshTokenIfNeeded(token *oauth2.Token) (*oauth2.Token, error) {
	if token.Valid() {
		return token, nil
	}

	if token.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokenSource := app.OAuthConfig.TokenSource(ctx, token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %v", err)
	}

	if newToken.AccessToken != token.AccessToken {
		if err := app.saveRefreshedToken(newToken); err != nil {
			log.Printf("Failed to save refreshed token: %v", err)
		}
	}

	return newToken, nil
}

func (app *App) saveRefreshedToken(token *oauth2.Token) error {
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal refreshed token: %v", err)
	}

	_, err = app.DB.Exec(`
		UPDATE admin_sessions 
		SET token = ? 
		WHERE user_email = (
			SELECT user_email FROM admin_sessions 
			WHERE token = ? 
			ORDER BY created_at DESC 
			LIMIT 1
		)
	`, string(tokenJSON), string(tokenJSON))

	return err
}

func (app *App) deleteSheetRow(spreadsheetID string, rowNumber int, token *oauth2.Token) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := app.OAuthConfig.Client(ctx, token)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("unable to retrieve Sheets client: %v", err)
	}

	// Create a delete dimension request
	deleteRequest := &sheets.Request{
		DeleteDimension: &sheets.DeleteDimensionRequest{
			Range: &sheets.DimensionRange{
				SheetId:    0, // Assuming first sheet
				Dimension:  "ROWS",
				StartIndex: int64(rowNumber - 1), // Convert to 0-based index
				EndIndex:   int64(rowNumber),     // End is exclusive
			},
		},
	}

	batchUpdateRequest := &sheets.BatchUpdateSpreadsheetRequest{
		Requests: []*sheets.Request{deleteRequest},
	}

	_, err = srv.Spreadsheets.BatchUpdate(spreadsheetID, batchUpdateRequest).Do()

	return err
}

func (app *App) handlePreviewSheet(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := r.Context().Value(UserEmailKey).(string)
	if !ok {
		log.Printf("User email not found in context")
		http.Error(w, "Authentication error", http.StatusInternalServerError)
		return
	}

	var previewReq PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&previewReq); err != nil {
		log.Printf("Failed to decode preview request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	sheetURL := SanitizeInput(previewReq.SheetURL)
	if sheetURL == "" {
		http.Error(w, "Sheet URL is required", http.StatusBadRequest)
		return
	}

	if !ValidateSheetURL(sheetURL) {
		log.Printf("Invalid sheet URL provided for preview: %s", sheetURL)
		http.Error(w, "Invalid Google Sheets URL format", http.StatusBadRequest)
		return
	}

	spreadsheetID, err := extractSpreadsheetID(sheetURL)
	if err != nil {
		log.Printf("Failed to extract spreadsheet ID from URL %s: %v", sheetURL, err)
		http.Error(w, "Invalid Google Sheets URL", http.StatusBadRequest)
		return
	}

	var tokenJSON string
	err = app.DB.QueryRow("SELECT token FROM admin_sessions WHERE user_email = ?", userEmail).Scan(&tokenJSON)
	if err != nil {
		log.Printf("Failed to get token for user %s: %v", userEmail, err)
		http.Error(w, "Session not found", http.StatusInternalServerError)
		return
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		log.Printf("Failed to unmarshal token: %v", err)
		http.Error(w, "Invalid token", http.StatusInternalServerError)
		return
	}

	refreshedToken, err := app.refreshTokenIfNeeded(&token)
	if err != nil {
		log.Printf("Failed to refresh token for preview: %v", err)
		http.Error(w, "Token refresh failed", http.StatusInternalServerError)
		return
	}
	token = *refreshedToken

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	preview, err := app.previewSheet(ctx, spreadsheetID, &token)
	if err != nil {
		log.Printf("Failed to preview sheet %s: %v", spreadsheetID, err)
		http.Error(w, fmt.Sprintf("Failed to preview sheet: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(preview)
}

func (app *App) previewSheet(ctx context.Context, spreadsheetID string, token *oauth2.Token) (*PreviewResponse, error) {
	client := app.OAuthConfig.Client(ctx, token)

	srv, err := sheets.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Sheets client: %v", err)
	}

	// Get spreadsheet metadata
	spreadsheet, err := srv.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve spreadsheet metadata: %v", err)
	}

	sheetName := "Sheet1"
	if len(spreadsheet.Sheets) > 0 && spreadsheet.Sheets[0].Properties != nil {
		sheetName = spreadsheet.Sheets[0].Properties.Title
	}

	// Get first few rows to determine column structure
	resp, err := srv.Spreadsheets.Values.Get(spreadsheetID, app.Config.SheetRange).Do()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve data from sheet: %v", err)
	}

	if len(resp.Values) == 0 {
		return nil, fmt.Errorf("no data found in sheet")
	}

	// Extract column names from the first row
	var columns []string
	if len(resp.Values) > 0 {
		for i, cell := range resp.Values[0] {
			if cell != nil {
				columnName := fmt.Sprintf("%v", cell)
				if columnName == "" {
					columnName = fmt.Sprintf("Column %d", i+1)
				}
				columns = append(columns, SanitizeInput(columnName))
			} else {
				columns = append(columns, fmt.Sprintf("Column %d", i+1))
			}
		}
	}

	// Count data rows (excluding header)
	rowCount := len(resp.Values) - 1
	if rowCount < 0 {
		rowCount = 0
	}

	return &PreviewResponse{
		Columns:   columns,
		RowCount:  rowCount,
		SheetName: sheetName,
	}, nil
}
