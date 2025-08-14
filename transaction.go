package main

import (
	"database/sql"
	"fmt"
)

// TransactionFunc represents a function that operates within a database transaction
type TransactionFunc func(*sql.Tx) error

// WithTransaction executes a function within a database transaction
// It automatically handles commit/rollback based on whether the function returns an error
func (app *App) WithTransaction(fn TransactionFunc) error {
	tx, err := app.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}

	// Ensure transaction is always closed
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p) // Re-throw panic after rollback
		}
	}()

	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("failed to rollback transaction: %v (original error: %v)", rollbackErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

// WithDirectoryTransaction executes a function within a directory-specific database transaction
func (app *App) WithDirectoryTransaction(directoryID string, fn TransactionFunc) error {
	db, err := app.DirectoryDBManager.GetDirectoryDB(directoryID)
	if err != nil {
		return fmt.Errorf("failed to get directory database: %v", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin directory transaction: %v", err)
	}

	// Ensure transaction is always closed
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p) // Re-throw panic after rollback
		}
	}()

	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("failed to rollback directory transaction: %v (original error: %v)", rollbackErr, err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit directory transaction: %v", err)
	}

	return nil
}

// DatabaseError represents different types of database errors
type DatabaseError struct {
	Type    string
	Message string
	Err     error
}

func (e *DatabaseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

// Common database error types
const (
	ErrTypeConnection = "CONNECTION_ERROR"
	ErrTypeNotFound   = "NOT_FOUND"
	ErrTypeConstraint = "CONSTRAINT_VIOLATION"
	ErrTypeTimeout    = "TIMEOUT"
	ErrTypePermission = "PERMISSION_DENIED"
)

// WrapDatabaseError wraps a database error with additional context
func WrapDatabaseError(errType, message string, err error) *DatabaseError {
	return &DatabaseError{
		Type:    errType,
		Message: message,
		Err:     err,
	}
}