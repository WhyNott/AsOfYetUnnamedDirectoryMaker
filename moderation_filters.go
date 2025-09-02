package main

import (
	"database/sql"
	"directoryCommunityWebsite/internal/models"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// ModerationFilter handles filter-based row access for moderators
type ModerationFilter struct {
	app *App
}

// NewModerationFilter creates a new moderation filter instance
func NewModerationFilter(app *App) *ModerationFilter {
	return &ModerationFilter{app: app}
}

// CanAccessRow checks if a moderator can access a specific row based on their filter configuration
func (mf *ModerationFilter) CanAccessRow(moderatorEmail, directoryID string, rowID int) (bool, error) {
	// Get moderator's filter configuration
	controls, err := mf.getModeratorFilters(moderatorEmail, directoryID)
	if err != nil {
		return false, err
	}

	// If no filters are configured, deny access by default
	if len(controls) == 0 {
		return false, nil
	}

	// Get the row data
	rowData, err := mf.getRowData(directoryID, rowID)
	if err != nil {
		return false, err
	}

	// Check if the row matches any of the configured filters
	return mf.rowMatchesFilters(controls, rowData, directoryID)
}

// GetAccessibleRows returns all row IDs that a moderator can access
func (mf *ModerationFilter) GetAccessibleRows(moderatorEmail, directoryID string) ([]int, error) {
	// Get moderator's filter configuration
	controls, err := mf.getModeratorFilters(moderatorEmail, directoryID)
	if err != nil {
		return nil, err
	}

	// If no filters are configured, return empty list
	if len(controls) == 0 {
		return []int{}, nil
	}

	// Get all rows and check which ones match the filters
	accessibleRows := []int{}
	rows, err := mf.getAllRows(directoryID)
	if err != nil {
		return nil, err
	}

	for rowID, rowData := range rows {
		matches, err := mf.rowMatchesFilters(controls, rowData, directoryID)
		if err != nil {
			log.Printf("Error checking row %d against filters: %v", rowID, err)
			continue
		}
		if matches {
			accessibleRows = append(accessibleRows, rowID)
		}
	}

	return accessibleRows, nil
}

// getModeratorFilters retrieves the filter configuration for a moderator
func (mf *ModerationFilter) getModeratorFilters(moderatorEmail, directoryID string) (models.Controls, error) {
	var filterJSON string
	err := mf.app.DB.QueryRow(`
		SELECT row_filter 
		FROM moderator_domains 
		WHERE moderator_email = ? AND directory_id = ?
	`, moderatorEmail, directoryID).Scan(&filterJSON)

	if err == sql.ErrNoRows {
		return models.Controls{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get moderator filters: %v", err)
	}

	if filterJSON == "" {
		return models.Controls{}, nil
	}

	var controls models.Controls
	if err := json.Unmarshal([]byte(filterJSON), &controls); err != nil {
		return nil, fmt.Errorf("failed to parse filter configuration: %v", err)
	}

	return controls, nil
}

// getRowData retrieves the data for a specific row
func (mf *ModerationFilter) getRowData(directoryID string, rowID int) (map[string]string, error) {
	// Get directory-specific database connection
	db, err := mf.app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory database: %v", err)
	}

	// Get column names from meta table
	columnNames, err := mf.getColumnNames(db, directoryID)
	if err != nil {
		return nil, err
	}

	// Get the row data
	var dataJSON string
	err = db.QueryRow("SELECT data FROM directory WHERE id = ?", rowID).Scan(&dataJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to get row data: %v", err)
	}

	var rowDataArray []string
	if err := json.Unmarshal([]byte(dataJSON), &rowDataArray); err != nil {
		return nil, fmt.Errorf("failed to parse row data: %v", err)
	}

	// Map column names to values
	rowData := make(map[string]string)
	for i, columnName := range columnNames {
		if i < len(rowDataArray) {
			rowData[columnName] = rowDataArray[i]
		} else {
			rowData[columnName] = ""
		}
	}

	return rowData, nil
}

// getAllRows retrieves all rows in the directory
func (mf *ModerationFilter) getAllRows(directoryID string) (map[int]map[string]string, error) {
	// Get directory-specific database connection
	db, err := mf.app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory database: %v", err)
	}

	// Get column names from meta table
	columnNames, err := mf.getColumnNames(db, directoryID)
	if err != nil {
		return nil, err
	}

	// Get all rows
	rows, err := db.Query("SELECT id, data FROM directory ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("failed to query directory rows: %v", err)
	}
	defer rows.Close()

	allRows := make(map[int]map[string]string)
	for rows.Next() {
		var id int
		var dataJSON string
		if err := rows.Scan(&id, &dataJSON); err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}

		var rowDataArray []string
		if err := json.Unmarshal([]byte(dataJSON), &rowDataArray); err != nil {
			log.Printf("Failed to parse row %d data: %v", id, err)
			continue
		}

		// Map column names to values
		rowData := make(map[string]string)
		for i, columnName := range columnNames {
			if i < len(rowDataArray) {
				rowData[columnName] = rowDataArray[i]
			} else {
				rowData[columnName] = ""
			}
		}
		allRows[id] = rowData
	}

	return allRows, nil
}

// getColumnNames retrieves column names from the meta table
func (mf *ModerationFilter) getColumnNames(db *sql.DB, directoryID string) ([]string, error) {
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

// rowMatchesFilters checks if a row matches the configured filters
func (mf *ModerationFilter) rowMatchesFilters(controls models.Controls, rowData map[string]string, directoryID string) (bool, error) {
	for _, control := range controls {
		matches, err := mf.controlMatches(control, rowData, directoryID)
		if err != nil {
			return false, err
		}
		if matches {
			return true, nil // If any filter matches, the row is accessible
		}
	}
	return false, nil
}

// controlMatches checks if a specific control (column-filter pair) matches the row data
func (mf *ModerationFilter) controlMatches(control models.Control, rowData map[string]string, directoryID string) (bool, error) {
	// Get the column value(s) based on the column ID
	columnValues, err := mf.getColumnValues(control.Column, rowData)
	if err != nil {
		return false, err
	}

	// Apply the filter to the column values
	return mf.filterMatches(control.Filter, columnValues, directoryID)
}

// getColumnValues extracts values from the row data based on the column ID specification
func (mf *ModerationFilter) getColumnValues(columnID models.ColumnID, rowData map[string]string) ([]string, error) {
	switch columnID.Type {
	case models.ColumnIDSingle:
		if value, exists := rowData[columnID.Value]; exists {
			return []string{value}, nil
		}
		return []string{""}, nil

	case models.ColumnIDRange:
		var values []string
		// For column ranges, we need to iterate through the range
		// This is a simplified implementation - you might want to enhance this
		for columnName, value := range rowData {
			if columnName >= columnID.Start && columnName <= columnID.End {
				values = append(values, value)
			}
		}
		return values, nil

	default:
		return nil, fmt.Errorf("unsupported column ID type: %s", columnID.Type)
	}
}

// filterMatches checks if the filter condition matches the given values
func (mf *ModerationFilter) filterMatches(filter models.Filter, values []string, directoryID string) (bool, error) {
	switch filter.Type {
	case models.FilterNumericRange:
		return mf.matchesNumericRange(filter, values)

	case models.FilterLocations:
		return mf.matchesLocationFilter(filter, values, directoryID)

	case models.FilterCategories:
		return mf.matchesStringValues(filter.Values, values)

	case models.FilterTags:
		return mf.matchesTagFilter(filter, values, directoryID)

	default:
		return false, fmt.Errorf("unsupported filter type: %s", filter.Type)
	}
}

// matchesNumericRange checks if any of the values match the numeric range filter
func (mf *ModerationFilter) matchesNumericRange(filter models.Filter, values []string) (bool, error) {
	if filter.Range == nil {
		return false, fmt.Errorf("numeric range filter missing range specification")
	}

	for _, value := range values {
		if value == "" {
			continue
		}

		numValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			continue // Skip non-numeric values
		}

		switch filter.Range.Type {
		case models.RangeFilterAbove:
			if numValue > filter.Range.Threshold {
				return true, nil
			}
		case models.RangeFilterBelow:
			if numValue < filter.Range.Threshold {
				return true, nil
			}
		case models.RangeFilterBetween:
			if numValue >= filter.Range.Min && numValue <= filter.Range.Max {
				return true, nil
			}
		}
	}

	return false, nil
}

// matchesLocationFilter checks if any of the values match the location filter
func (mf *ModerationFilter) matchesLocationFilter(filter models.Filter, values []string, directoryID string) (bool, error) {
	// For location filters, we need to check against the tag tables
	// This is similar to tag matching but for location-type columns
	return mf.matchesStringValues(filter.Values, values)
}

// matchesTagFilter checks if any of the values match the tag filter
func (mf *ModerationFilter) matchesTagFilter(filter models.Filter, values []string, directoryID string) (bool, error) {
	// For tag filters, the values might be comma-separated
	// We need to split them and check against the filter values
	allTags := []string{}
	for _, value := range values {
		tags := strings.Split(value, ",")
		for _, tag := range tags {
			allTags = append(allTags, strings.TrimSpace(tag))
		}
	}
	
	return mf.matchesStringValues(filter.Values, allTags)
}

// matchesStringValues checks if any of the actual values match any of the filter values
func (mf *ModerationFilter) matchesStringValues(filterValues, actualValues []string) (bool, error) {
	for _, filterValue := range filterValues {
		for _, actualValue := range actualValues {
			if strings.EqualFold(strings.TrimSpace(filterValue), strings.TrimSpace(actualValue)) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ValidateFilters validates that the filter configuration is valid for the given directory
func (mf *ModerationFilter) ValidateFilters(controls models.Controls, directoryID string) error {
	// Get directory-specific database connection
	db, err := mf.app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return fmt.Errorf("failed to get directory database: %v", err)
	}

	// Get valid column names
	columnNames, err := mf.getColumnNames(db, directoryID)
	if err != nil {
		return err
	}

	columnMap := make(map[string]bool)
	for _, name := range columnNames {
		columnMap[name] = true
	}

	// Validate each control
	for i, control := range controls {
		// Validate column ID
		if !control.Column.IsValid() {
			return fmt.Errorf("control %d has invalid column ID", i)
		}

		// Check if column names exist
		switch control.Column.Type {
		case models.ColumnIDSingle:
			if !columnMap[control.Column.Value] {
				return fmt.Errorf("control %d references non-existent column: %s", i, control.Column.Value)
			}
		case models.ColumnIDRange:
			if !columnMap[control.Column.Start] || !columnMap[control.Column.End] {
				return fmt.Errorf("control %d references non-existent columns in range: %s-%s", 
					i, control.Column.Start, control.Column.End)
			}
		}

		// Validate filter
		if !control.Filter.IsValid() {
			return fmt.Errorf("control %d has invalid filter", i)
		}
	}

	return nil
}