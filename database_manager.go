package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// DirectoryDatabaseManager manages connections to directory-specific databases
// TODO: it seems to me that this is doing the same thing as directory_manager.go
type DirectoryDatabaseManager struct {
	connections map[string]*sql.DB
	mutex       sync.RWMutex
	app         *App
}

// NewDirectoryDatabaseManager creates a new database manager
func NewDirectoryDatabaseManager(app *App) *DirectoryDatabaseManager {
	return &DirectoryDatabaseManager{
		connections: make(map[string]*sql.DB),
		app:         app,
	}
}

// GetDirectoryDB gets or creates a database connection for a specific directory
func (dm *DirectoryDatabaseManager) GetDirectoryDB(directoryID string) (*sql.DB, error) {
	dm.mutex.RLock()
	if db, exists := dm.connections[directoryID]; exists {
		dm.mutex.RUnlock()
		return db, nil
	}
	dm.mutex.RUnlock()

	// Get directory info from the main database
	directory, err := dm.app.GetDirectory(directoryID)
	if err != nil {
		return nil, fmt.Errorf("directory not found: %v", err)
	}

	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	// Double-check after acquiring write lock
	if db, exists := dm.connections[directoryID]; exists {
		return db, nil
	}

	// Open connection to directory-specific database
	db, err := sql.Open("sqlite3", directory.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open directory database: %v", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping directory database: %v", err)
	}

	dm.connections[directoryID] = db
	return db, nil
}

// CloseAll closes all directory database connections
func (dm *DirectoryDatabaseManager) CloseAll() {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	for directoryID, db := range dm.connections {
		if err := db.Close(); err != nil {
			fmt.Printf("Error closing database for directory %s: %v\n", directoryID, err)
		}
	}
	dm.connections = make(map[string]*sql.DB)
}

// CloseDirectory closes the database connection for a specific directory
func (dm *DirectoryDatabaseManager) CloseDirectory(directoryID string) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()

	if db, exists := dm.connections[directoryID]; exists {
		if err := db.Close(); err != nil {
			fmt.Printf("Error closing database for directory %s: %v\n", directoryID, err)
		}
		delete(dm.connections, directoryID)
	}
}

// GetCurrentDirectoryID extracts directory ID from request parameters or returns default
func GetCurrentDirectoryID(r *http.Request) string {
	directoryID := r.URL.Query().Get("dir")
	if directoryID == "" {
		return "default" // Fall back to default directory
	}
	return directoryID
}
