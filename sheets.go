package main

import (
	"context"
	utils2 "directoryCommunityWebsite/internal/utils"
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

func (app *App) importDirectoryFromSheet(
	ctx context.Context, spreadsheetID string,
	token *oauth2.Token, directoryID string,
	columnNames []string, columnTypes []string,
) error {
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

	// Validate column names match the sheet header
	if len(resp.Values) > 0 {
		sheetColumns := make([]string, len(resp.Values[0]))
		for i, cell := range resp.Values[0] {
			if cell != nil {
				sheetColumns[i] = strings.TrimSpace(fmt.Sprintf("%v", cell))
			}
		}

		if len(columnNames) != len(sheetColumns) {
			return fmt.Errorf("column count mismatch: expected %d columns, sheet has %d",
				len(columnNames), len(sheetColumns))
		}

		for i, expectedCol := range columnNames {
			if i < len(sheetColumns) && strings.TrimSpace(expectedCol) != sheetColumns[i] {
				return fmt.Errorf("column name mismatch at position %d: expected '%s', sheet has '%s'",
					i, expectedCol, sheetColumns[i])
			}
		}
	}

	// Get directory-specific database connection
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return fmt.Errorf("failed to get directory database: %v", err)
	}

	if _, err := db.Exec("DROP TABLE IF EXISTS _meta_directory_column_types"); err != nil {
		return fmt.Errorf("failed to clear column types table: %v", err)
	}

	_, err = db.Exec(`
		CREATE Table _meta_directory_column_types (
  	columnName TEXT NOT NULL,
  	columnTable TEXT NOT NULL,
  	columnType TEXT CHECK (
  		columnType IN (
  			'basic',
  			'numeric',
  			'location',
  			'tag',
  			'category'
  		)
  	) NOT NULL,
  	PRIMARY KEY (columnName, columnTable)
  );
	`)

	if err != nil {
		return fmt.Errorf("failed to reset _meta_directory_column_types: %v", err)
	}

	for i := range columnNames {
		if _, err := db.Exec(
			`INSERT INTO _meta_directory_column_types (
                                          columnName,
                                          columnTable,
                                          columnType
                    ) VALUES (?, ?, ?)`,
			columnNames[i], directoryID, columnTypes[i]); err != nil {
			return fmt.Errorf("failed to insert column type %v for column %v: %v",
				columnTypes[i], columnNames[i], err)
		}

		if columnTypes[i] == "tag" || columnTypes[i] == "location" {
			tableName := fmt.Sprintf("_meta_%v_tag_%v", directoryID, columnNames[i])

			if _, err := db.Exec(
				`CREATE TABLE ? (
                       columnTable TEXT NOT NULL,
                       rowID integer NOT NULL,
                       tag TEXT NOT NULL)
                       FOREIGN KEY (rowID) REFERENCES ?(rowID);`,
				tableName, directoryID); err != nil {
				return fmt.Errorf("failed to create tag table %v for %v: %v", tableName, directoryID, err)
			}

		}

	}

	// Drop the old directory table if it exists
	if _, err := db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS '%s'", directoryID)); err != nil {
		return fmt.Errorf("failed to drop old directory table: %v", err)
	}

	// Create column definitions for the new table
	columnDefs := []string{"rowID INTEGER PRIMARY KEY AUTOINCREMENT"}
	for i := range columnNames {
		// Use TEXT type for all columns since tags and locations will be handled in separate tables
		// Quote column names to handle spaces and special characters
		columnDefs = append(columnDefs, fmt.Sprintf("[%s] TEXT", columnNames[i]))
	}

	// Create the new directory table
	query := fmt.Sprintf("CREATE TABLE '%s' (%s)", directoryID, strings.Join(columnDefs, ", "))
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("failed to create directory table based on query %v: %v ", query, err)
	}

	// Insert data from the sheet
	if len(resp.Values) > 1 {
		// Prepare column list for INSERT statement - quote column names
		quotedColumns := make([]string, len(columnNames))
		for i, name := range columnNames {
			quotedColumns[i] = fmt.Sprintf("[%s]", name)
		}
		columnList := strings.Join(quotedColumns, ", ")
		placeholders := make([]string, len(columnNames))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		insertQuery := fmt.Sprintf("INSERT INTO '%s' (%s) VALUES (%s)",
			directoryID, columnList, strings.Join(placeholders, ", "))

		for i := 1; i < len(resp.Values); i++ { // Skip header row
			row := resp.Values[i]
			rowData := make([]interface{}, len(columnNames))

			for j := range columnNames {
				var cellValue string
				if j < len(row) && row[j] != nil {
					cellValue = fmt.Sprintf("%v", row[j])
				}
				rowData[j] = cellValue
			}

			// Insert the main row
			result, err := db.Exec(insertQuery, rowData...)
			if err != nil {
				return fmt.Errorf("failed to insert row %d: %v", insertQuery, err)
			}

			// Get the inserted row ID
			rowID, err := result.LastInsertId()
			if err != nil {
				return fmt.Errorf("failed to get last insert ID for row %d: %v", i, err)
			}

			// Handle tag and location columns
			for j, columnType := range columnTypes {
				if columnType == "tag" || columnType == "location" {
					if j < len(row) && row[j] != nil {
						cellValue := fmt.Sprintf("%v", row[j])
						if cellValue != "" {
							// Split comma-separated values
							tags := strings.Split(cellValue, ",")
							tableName := fmt.Sprintf("_meta_%s_tag_%s", directoryID, columnNames[j])

							for _, tag := range tags {
								tag = strings.TrimSpace(tag)
								if tag != "" {
									_, err := db.Exec(
										fmt.Sprintf("INSERT INTO '%s' (columnTable, rowID, tag) VALUES (?, ?, ?)", tableName),
										directoryID, rowID, tag)
									if err != nil {
										return fmt.Errorf("failed to insert tag %s for column %s: %v",
											tag, columnNames[j], err)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return nil

}
func (app *App) reimportSheet(
	ctx context.Context, spreadsheetID string,
	token *oauth2.Token, directoryID string,
) error {
	// Get column names and types from database for re-import
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return fmt.Errorf("Failed to get directory database for re-import: %v\n", err)
	}

	// Query column types from meta table
	rows, err := db.Query(`
		SELECT columnName, columnType 
		FROM _meta_directory_column_types 
		WHERE columnTable = ? 
		ORDER BY rowid`, directoryID)
	if err != nil {
		return fmt.Errorf("Failed to query column types for re-import: %v\n", err)
	}
	defer rows.Close()

	var columnNames []string
	var columnTypes []string

	for rows.Next() {
		var columnName, columnType string
		if err := rows.Scan(&columnName, &columnType); err != nil {
			return fmt.Errorf("Failed to scan column data for re-import: %v\n", err)
		}
		columnNames = append(columnNames, columnName)
		columnTypes = append(columnTypes, columnType)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("Error iterating column rows for re-import: %v\n", err)
	}

	if len(columnNames) == 0 {
		return fmt.Errorf("No column configuration found for directory %s, skipping re-import\n", directoryID)

	}

	return app.importDirectoryFromSheet(ctx, spreadsheetID, token, directoryID, columnNames, columnTypes)

}

func (app *App) handleImport(w http.ResponseWriter, r *http.Request) {
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	// Get directory ID from query parameter or default to "default"
	directoryID := utils2.GetDirectoryID(r)

	// Check if user has access to this directory
	isOwner, err := app.IsDirectoryOwner(directoryID, userEmail)
	if err != nil {
		log.Printf("Failed to check directory ownership: %v", err)
		utils2.DatabaseError(w)
		return
	}

	isAdmin, err := app.IsAdmin(userEmail)
	if err != nil {
		log.Printf("Failed to check super admin status: %v", err)
		utils2.DatabaseError(w)
		return
	}

	if !isOwner && !isAdmin {
		log.Printf("User %s does not have access to directory %s", userEmail, directoryID)
		utils2.AuthorizationError(w)
		return
	}

	sheetURL := SanitizeInput(r.FormValue("sheet_url"))
	if sheetURL == "" {
		utils2.ValidationError(w, "Sheet URL is required")
		return
	}

	if !ValidateSheetURL(sheetURL) {
		log.Printf("Invalid sheet URL provided: %s", sheetURL)
		utils2.ValidationError(w, "Invalid Google Sheets URL format")
		return
	}

	spreadsheetID, err := extractSpreadsheetID(sheetURL)
	if err != nil {
		log.Printf("Failed to extract spreadsheet ID from URL %s: %v", sheetURL, err)
		utils2.ValidationError(w, "Invalid Google Sheets URL")
		return
	}

	// Parse column names and types from form data
	var columnNames []string
	var columnTypes []string

	// Parse column names (assuming they come as column_name_0, column_name_1, etc.)
	for i := 0; ; i++ {
		columnName := r.FormValue(fmt.Sprintf("column_name_%d", i))
		if columnName == "" {
			break
		}
		columnNames = append(columnNames, SanitizeInput(columnName))
	}

	// Parse column types (assuming they come as column_type_0, column_type_1, etc.)
	for i := 0; ; i++ {
		columnType := r.FormValue(fmt.Sprintf("column_type_%d", i))
		if columnType == "" {
			break
		}
		// Validate column type
		validTypes := map[string]bool{
			"basic": true, "numeric": true, "location": true,
			"tag": true, "category": true,
		}
		if !validTypes[columnType] {
			log.Printf("Invalid column type provided: %s", columnType)
			utils2.ValidationError(w, fmt.Sprintf("Invalid column type: %s", columnType))
			return
		}
		columnTypes = append(columnTypes, columnType)
	}

	// Validate that we have the same number of column names and types
	if len(columnNames) != len(columnTypes) {
		log.Printf("Column names count (%d) doesn't match column types count (%d)",
			len(columnNames), len(columnTypes))
		utils2.ValidationError(w, "Column names and types count mismatch")
		return
	}

	if len(columnNames) == 0 {
		utils2.ValidationError(w, "No columns specified")
		return
	}

	token, err := app.getDecryptedToken(userEmail)
	if err != nil {
		log.Printf("Failed to get token for user %s: %v", userEmail, err)
		utils2.InternalServerError(w, "Session not found")
		return
	}

	refreshedToken, err := app.refreshTokenIfNeeded(token)
	if err != nil {
		log.Printf("Failed to refresh token: %v", err)
		utils2.InternalServerError(w, "Token refresh failed")
		return
	}
	token = refreshedToken

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := app.importDirectoryFromSheet(ctx, spreadsheetID, refreshedToken, directoryID, columnNames, columnTypes); err != nil {
		log.Printf("Failed to import sheet %s: %v", spreadsheetID, err)
		utils2.InternalServerError(w, fmt.Sprintf("Failed to import sheet: %v", err))
		return
	}

	_, err = app.DB.Exec("UPDATE admin_sessions SET sheet_url = ?, directory_id = ? WHERE user_email = ?", sheetURL, directoryID, userEmail)
	if err != nil {
		log.Printf("Failed to save sheet URL for user %s: %v", userEmail, err)
		utils2.InternalServerError(w, "Failed to save sheet URL")
		return
	}

	log.Printf("Import successful, redirecting to admin page with success message")
	redirectURL := "/owner?imported=true"
	if directoryID != "default" {
		redirectURL += "&dir=" + directoryID
	}
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
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

// getDecryptedToken retrieves and decrypts an OAuth token for a user
func (app *App) getDecryptedToken(userEmail string) (*oauth2.Token, error) {
	var encryptedTokenJSON string
	err := app.DB.QueryRow("SELECT token FROM admin_sessions WHERE user_email = ?", userEmail).Scan(&encryptedTokenJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to get token for user %s: %v", userEmail, err)
	}

	// Decrypt the token
	tokenJSON, err := app.EncryptionService.Decrypt(encryptedTokenJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt token for user %s: %v", userEmail, err)
	}

	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal token: %v", err)
	}

	return &token, nil
}

func (app *App) saveRefreshedToken(token *oauth2.Token) error {
	tokenJSON, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal refreshed token: %v", err)
	}

	// Encrypt the token before storing
	encryptedToken, err := app.EncryptionService.Encrypt(string(tokenJSON))
	if err != nil {
		return fmt.Errorf("failed to encrypt refreshed token: %v", err)
	}

	// Note: This update strategy is flawed - we should update by user_email instead
	// For now, we'll leave it but this needs to be fixed in a future update
	_, err = app.DB.Exec(`
		UPDATE admin_sessions 
		SET token = ? 
		WHERE user_email = (
			SELECT user_email FROM admin_sessions 
			WHERE token = ? 
			ORDER BY created_at DESC 
			LIMIT 1
		)
	`, encryptedToken, encryptedToken)

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
	userEmail, ok := utils2.RequireAuthentication(w, r)
	if !ok {
		return
	}

	var previewReq PreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&previewReq); err != nil {
		log.Printf("Failed to decode preview request: %v", err)
		utils2.BadRequestError(w, "Invalid request body")
		return
	}

	sheetURL := SanitizeInput(previewReq.SheetURL)
	if sheetURL == "" {
		utils2.ValidationError(w, "Sheet URL is required")
		return
	}

	if !ValidateSheetURL(sheetURL) {
		log.Printf("Invalid sheet URL provided for preview: %s", sheetURL)
		utils2.ValidationError(w, "Invalid Google Sheets URL format")
		return
	}

	spreadsheetID, err := extractSpreadsheetID(sheetURL)
	if err != nil {
		log.Printf("Failed to extract spreadsheet ID from URL %s: %v", sheetURL, err)
		utils2.ValidationError(w, "Invalid Google Sheets URL")
		return
	}

	token, err := app.getDecryptedToken(userEmail)
	if err != nil {
		log.Printf("Failed to get token for user %s: %v", userEmail, err)
		utils2.InternalServerError(w, "Session not found")
		return
	}

	refreshedToken, err := app.refreshTokenIfNeeded(token)
	if err != nil {
		log.Printf("Failed to refresh token for preview: %v", err)
		utils2.InternalServerError(w, "Token refresh failed")
		return
	}
	token = refreshedToken

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	preview, err := app.previewSheet(ctx, spreadsheetID, refreshedToken)
	if err != nil {
		log.Printf("Failed to preview sheet %s: %v", spreadsheetID, err)
		utils2.InternalServerError(w, fmt.Sprintf("Failed to preview sheet: %v", err))
		return
	}

	utils2.RespondWithJSON(w, 200, preview)
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

	// Default all columns to "basic" type
	columnTypes := make([]string, len(columns))
	for i := range columnTypes {
		columnTypes[i] = "basic"
	}

	return &PreviewResponse{
		Columns:     columns,
		ColumnTypes: columnTypes,
		RowCount:    rowCount,
		SheetName:   sheetName,
	}, nil
}
