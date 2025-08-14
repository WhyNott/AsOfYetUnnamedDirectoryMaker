package models

import "time"

//TODO: Is this actually used anywhere???

type Directory struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	DatabasePath string    `json:"database_path"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// DirectoryEntry represents a row of data in a directory
type DirectoryEntry struct {
	ID   int    `json:"id"`
	Data string `json:"data"`
}

// DirectoryOwner represents ownership relationship
type DirectoryOwner struct {
	ID          int       `json:"id"`
	DirectoryID string    `json:"directory_id"`
	UserEmail   string    `json:"user_email"`
	CreatedAt   time.Time `json:"created_at"`
}

// ColumnInfo represents column metadata
type ColumnInfo struct {
	ID      int      `json:"id"`
	Columns []string `json:"columns"`
}
