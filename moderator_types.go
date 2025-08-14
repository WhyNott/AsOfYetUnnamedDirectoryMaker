package main

import (
	"time"
)

// Moderator represents a moderator user
type Moderator struct {
	ID               int       `json:"id"`
	UserEmail        string    `json:"user_email"`
	Username         string    `json:"username"`
	AuthProvider     string    `json:"auth_provider"`
	DirectoryID      string    `json:"directory_id"`
	AppointedBy      string    `json:"appointed_by"`
	AppointedByType  string    `json:"appointed_by_type"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// ModeratorHierarchy represents parent-child relationships between moderators
type ModeratorHierarchy struct {
	ID                   int       `json:"id"`
	ParentModeratorEmail string    `json:"parent_moderator_email"`
	ChildModeratorEmail  string    `json:"child_moderator_email"`
	DirectoryID          string    `json:"directory_id"`
	CreatedAt            time.Time `json:"created_at"`
}

// ModeratorDomain represents a moderator's area of responsibility
type ModeratorDomain struct {
	ID               int       `json:"id"`
	ModeratorEmail   string    `json:"moderator_email"`
	DirectoryID      string    `json:"directory_id"`
	RowFilter        string    `json:"row_filter"` // JSON string
	CanEdit          bool      `json:"can_edit"`
	CanApprove       bool      `json:"can_approve"`
	RequiresApproval bool      `json:"requires_approval"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// PendingChange represents a change waiting for approval
type PendingChange struct {
	ID           int       `json:"id"`
	DirectoryID  string    `json:"directory_id"`
	RowID        int       `json:"row_id"`
	ColumnName   string    `json:"column_name"`
	OldValue     string    `json:"old_value"`
	NewValue     string    `json:"new_value"`
	ChangeType   string    `json:"change_type"`
	SubmittedBy  string    `json:"submitted_by"`
	Status       string    `json:"status"`
	ReviewedBy   string    `json:"reviewed_by"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}

// UserProfile represents a user's profile information
type UserProfile struct {
	ID           int       `json:"id"`
	UserEmail    string    `json:"user_email"`
	Username     string    `json:"username"`
	AuthProvider string    `json:"auth_provider"`
	ProviderID   string    `json:"provider_id"`
	AvatarURL    string    `json:"avatar_url"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ModeratorPermissions represents what a moderator can do
type ModeratorPermissions struct {
	CanEdit          bool     `json:"can_edit"`
	CanApprove       bool     `json:"can_approve"`
	RequiresApproval bool     `json:"requires_approval"`
	RowsAllowed      []int    `json:"rows_allowed"` // parsed from RowFilter
}

// AppointModeratorRequest represents the API request for appointing a moderator
type AppointModeratorRequest struct {
	UserEmail        string   `json:"user_email"`
	Username         string   `json:"username"`
	AuthProvider     string   `json:"auth_provider"`
	DirectoryID      string   `json:"directory_id"`
	RowFilter        []int    `json:"row_filter"` // specific row IDs
	CanEdit          bool     `json:"can_edit"`
	CanApprove       bool     `json:"can_approve"`
	RequiresApproval bool     `json:"requires_approval"`
}

// ChangeApprovalRequest represents the API request for approving/rejecting changes
type ChangeApprovalRequest struct {
	ChangeID int    `json:"change_id"`
	Action   string `json:"action"` // "approve" or "reject"
	Reason   string `json:"reason"`
}

// UserType constants
const (
	UserTypeAdmin = "admin"
	UserTypeOwner      = "owner"
	UserTypeModerator  = "moderator"
)

// Auth provider constants
const (
	AuthProviderGoogle  = "google"
	AuthProviderTwitter = "twitter"
)

// Change status constants
const (
	ChangeStatusPending  = "pending"
	ChangeStatusApproved = "approved"
	ChangeStatusRejected = "rejected"
)

// Change type constants
const (
	ChangeTypeEdit   = "edit"
	ChangeTypeAdd    = "add"
	ChangeTypeDelete = "delete"
)