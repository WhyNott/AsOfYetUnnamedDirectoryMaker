package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Directory represents a directory configuration
// note: maybe this should be merged with database_manager?
type Directory struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	DatabasePath string    `json:"database_path"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateDirectory creates a new directory with the specified owner using proper transaction management
func (app *App) CreateDirectory(id, name, description, ownerEmail string) error {
	// Validate directory ID (must be URL-safe)
	if !isValidDirectoryID(id) {
		return WrapDatabaseError(ErrTypeConstraint, "invalid directory ID: must contain only letters, numbers, hyphens, and underscores", nil)
	}

	// Create database path
	dbPath := filepath.Join("./data", id+".db")

	// Ensure data directory exists
	if err := os.MkdirAll("./data", 0755); err != nil {
		return WrapDatabaseError(ErrTypePermission, "failed to create data directory", err)
	}

	// Use transaction to ensure atomic directory creation
	err := app.WithTransaction(func(tx *sql.Tx) error {
		// Create directory record
		_, err := tx.Exec(`
			INSERT INTO directories (id, name, description, database_path, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, id, name, description, dbPath, time.Now(), time.Now())
		if err != nil {
			return WrapDatabaseError(ErrTypeConstraint, "failed to create directory record", err)
		}

		// Add owner
		_, err = tx.Exec(`
			INSERT INTO directory_owners (directory_id, user_email, role, created_at)
			VALUES (?, ?, 'owner', ?)
		`, id, ownerEmail, time.Now())
		if err != nil {
			return WrapDatabaseError(ErrTypeConstraint, "failed to add directory owner", err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Initialize the directory database after the transaction commits
	if err := app.initDirectoryDatabase(dbPath); err != nil {
		// If initialization fails, clean up the directory records
		app.WithTransaction(func(tx *sql.Tx) error {
			tx.Exec("DELETE FROM directory_owners WHERE directory_id = ?", id)
			tx.Exec("DELETE FROM directories WHERE id = ?", id)
			return nil
		})
		return WrapDatabaseError(ErrTypeConnection, "failed to initialize directory database", err)
	}

	return nil
}

// GetDirectory retrieves a directory by ID
func (app *App) GetDirectory(id string) (*Directory, error) {
	var dir Directory
	err := app.DB.QueryRow(`
		SELECT id, name, description, database_path, created_at, updated_at
		FROM directories WHERE id = ?
	`, id).Scan(&dir.ID, &dir.Name, &dir.Description, &dir.DatabasePath, &dir.CreatedAt, &dir.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("directory not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query directory: %v", err)
	}

	return &dir, nil
}

// IsDirectoryOwner checks if a user owns or has admin access to a directory with caching
func (app *App) IsDirectoryOwner(directoryID, userEmail string) (bool, error) {
	// Check cache first
	if isOwner, cached := app.PermissionCache.GetDirectoryOwnership(directoryID, userEmail); cached {
		return isOwner, nil
	}

	// Query database
	var count int
	err := app.DB.QueryRow(`
		SELECT COUNT(*) FROM directory_owners
		WHERE directory_id = ? AND user_email = ? AND role IN ('owner', 'admin')
	`, directoryID, userEmail).Scan(&count)
	if err != nil {
		return false, WrapDatabaseError(ErrTypeConnection, "failed to check directory ownership", err)
	}

	isOwner := count > 0
	// Cache the result
	app.PermissionCache.SetDirectoryOwnership(directoryID, userEmail, isOwner)

	return isOwner, nil
}

// IsAdmin checks if a user is an admin with caching
func (app *App) IsAdmin(userEmail string) (bool, error) {
	// Check cache first
	if isAdmin, cached := app.PermissionCache.GetAdminStatus(userEmail); cached {
		return isAdmin, nil
	}

	// Query database
	var count int
	err := app.DB.QueryRow(`
		SELECT COUNT(*) FROM admins WHERE user_email = ?
	`, userEmail).Scan(&count)
	if err != nil {
		return false, WrapDatabaseError(ErrTypeConnection, "failed to check admin status", err)
	}

	isAdmin := count > 0
	// Cache the result
	app.PermissionCache.SetAdminStatus(userEmail, isAdmin)

	return isAdmin, nil
}

// AddAdmin adds a user as an admin and invalidates cache
func (app *App) AddAdmin(userEmail string) error {
	_, err := app.DB.Exec(`
		INSERT OR IGNORE INTO admins (user_email, created_at)
		VALUES (?, ?)
	`, userEmail, time.Now())

	if err == nil {
		// Invalidate cached permissions for this user
		app.PermissionCache.InvalidateUser(userEmail)
	}

	return err
}

// initDirectoryDatabase creates the tables for a new directory database
func (app *App) initDirectoryDatabase(dbPath string) error {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open directory database: %v", err)
	}
	defer db.Close()

	query := `
		CREATE TABLE IF NOT EXISTS directory (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			data TEXT NOT NULL
		);
		
		CREATE TABLE IF NOT EXISTS directory_columns (
			id INTEGER PRIMARY KEY,
			columns TEXT NOT NULL
		);
	`
	_, err = db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to initialize directory database tables: %v", err)
	}

	return nil
}

// MigrateToMultiDirectory creates a default directory and migrates existing data
func (app *App) MigrateToMultiDirectory() error {
	// Check if default directory already exists
	_, err := app.GetDirectory("default")
	if err == nil {
		// Already migrated
		return nil
	}

	log.Printf("Migrating to multi-directory system...")

	// Create default directory
	if err := app.CreateDirectory("default", "Default Directory", "Legacy directory for existing data", "system@localhost"); err != nil {
		return fmt.Errorf("failed to create default directory: %v", err)
	}

	// Copy existing directory.db to default.db
	defaultDBPath := "./data/default.db"
	existingDBPath := "./directory.db"

	if _, err := os.Stat(existingDBPath); err == nil {
		// Copy existing data
		if err := app.copyDatabase(existingDBPath, defaultDBPath); err != nil {
			log.Printf("Warning: failed to copy existing database: %v", err)
		} else {
			log.Printf("Successfully migrated existing data to default directory")
		}
	}

	// Make all existing admin users owners of the default directory
	rows, err := app.DB.Query("SELECT DISTINCT user_email FROM admin_sessions")
	if err != nil {
		log.Printf("Warning: failed to query existing admin users: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var userEmail string
			if err := rows.Scan(&userEmail); err == nil {
				// Add as owner of default directory
				app.DB.Exec(`
					INSERT OR IGNORE INTO directory_owners (directory_id, user_email, role, created_at)
					VALUES ('default', ?, 'owner', ?)
				`, userEmail, time.Now())
				log.Printf("Added %s as owner of default directory", userEmail)
			}
		}
	}

	return nil
}

// copyDatabase copies a SQLite database file
func (app *App) copyDatabase(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// isValidDirectoryID checks if a directory ID is URL-safe
func isValidDirectoryID(id string) bool {
	if len(id) < 1 || len(id) > 50 {
		return false
	}
	// Allow letters, numbers, hyphens, and underscores
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
	return matched && !strings.HasPrefix(id, "-") && !strings.HasSuffix(id, "-")
}

// DeleteDirectory deletes a directory and all its associated data
func (app *App) DeleteDirectory(directoryID string) error {
	if directoryID == "default" {
		return fmt.Errorf("cannot delete default directory")
	}

	// Start a transaction
	tx, err := app.DB.Begin()
	if err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to begin transaction", err)
	}
	defer tx.Rollback()

	// Get directory info to get database path before deletion
	var directory Directory
	err = tx.QueryRow(`
		SELECT id, name, description, database_path, created_at, updated_at
		FROM directories WHERE id = ?
	`, directoryID).Scan(&directory.ID, &directory.Name, &directory.Description, &directory.DatabasePath, &directory.CreatedAt, &directory.UpdatedAt)

	if err == sql.ErrNoRows {
		return fmt.Errorf("directory not found")
	}
	if err != nil {
		return WrapDatabaseError(ErrTypeNotFound, "failed to query directory", err)
	}

	// Close database connection if it exists
	app.DirectoryDBManager.CloseDirectory(directoryID)

	// Delete directory owners
	_, err = tx.Exec(`DELETE FROM directory_owners WHERE directory_id = ?`, directoryID)
	if err != nil {
		return WrapDatabaseError(ErrTypeConstraint, "failed to delete directory owners", err)
	}

	// Delete directory record
	_, err = tx.Exec(`DELETE FROM directories WHERE id = ?`, directoryID)
	if err != nil {
		return WrapDatabaseError(ErrTypeConstraint, "failed to delete directory", err)
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return WrapDatabaseError(ErrTypeConnection, "failed to commit transaction", err)
	}

	// Delete the database file
	if err := os.Remove(directory.DatabasePath); err != nil && !os.IsNotExist(err) {
		log.Printf("Warning: failed to delete directory database file %s: %v", directory.DatabasePath, err)
		// Don't return error here as the directory record is already deleted
	}

	// Clear permission cache for all users who might have had access
	app.PermissionCache.Clear()

	log.Printf("Successfully deleted directory %s", directoryID)
	return nil
}

// GetUserDirectories returns all directories a user has access to (owned directories plus all directories for admins)
func (app *App) GetUserDirectories(userEmail string) ([]Directory, error) {
	// Check if user is admin
	isAdmin, err := app.IsAdmin(userEmail)
	if err != nil {
		return nil, WrapDatabaseError(ErrTypeConnection, "failed to check admin status", err)
	}

	var directories []Directory
	var rows *sql.Rows

	if isAdmin {
		// Admins see all directories
		rows, err = app.DB.Query(`
			SELECT d.id, d.name, d.description, d.database_path, d.created_at, d.updated_at
			FROM directories d
			ORDER BY d.name
		`)
	} else {
		// Regular users see only directories they own
		rows, err = app.DB.Query(`
			SELECT d.id, d.name, d.description, d.database_path, d.created_at, d.updated_at
			FROM directories d
			INNER JOIN directory_owners o ON d.id = o.directory_id
			WHERE o.user_email = ?
			ORDER BY d.name
		`, userEmail)
	}

	if err != nil {
		return nil, WrapDatabaseError(ErrTypeConnection, "failed to query user directories", err)
	}
	defer rows.Close()

	for rows.Next() {
		var dir Directory
		err := rows.Scan(&dir.ID, &dir.Name, &dir.Description, &dir.DatabasePath, &dir.CreatedAt, &dir.UpdatedAt)
		if err != nil {
			return nil, WrapDatabaseError(ErrTypeConnection, "failed to scan directory", err)
		}
		directories = append(directories, dir)
	}

	return directories, nil
}
