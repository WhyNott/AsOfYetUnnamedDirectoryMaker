# API Documentation

This document describes all the API endpoints available in the Directory Community Website application.

## Authentication

Most endpoints require authentication via session cookies. Authentication is handled through Google OAuth or Twitter OAuth.

### Auth Flow
1. `GET /login` - Display login page
2. `GET /auth/google` - Initiate Google OAuth
3. `GET /auth/twitter` - Initiate Twitter OAuth  
4. `GET /auth/callback` - Handle OAuth callback
5. `GET /logout` - Clear session and logout

## Core API Endpoints

### Directory Data

#### Get Directory Data
```
GET /api/directory
Query Parameters:
  - dir: Directory ID (optional, defaults to "default")

Response: Array of directory entries
[
  {
    "id": 1,
    "data": "[\"col1\", \"col2\", \"col3\"]"
  }
]
```

#### Get Directory Columns
```
GET /api/columns
Query Parameters:
  - dir: Directory ID (optional, defaults to "default")

Response: Column information
{
  "id": 1,
  "columns": ["Name", "Email", "Phone"]
}
```

#### Get User Directories
```
GET /api/user-directories
Headers: Authentication required

Response: Array of accessible directories
[
  {
    "id": "company-1",
    "name": "Company Directory",
    "description": "Employee directory"
  }
]
```

### Data Modification

#### Apply Correction
```
POST /api/corrections
Headers: 
  - Authentication required
  - CSRF token required
  
Body:
{
  "row": 0,
  "column": 1,
  "value": "new value"
}

Response:
{
  "success": true,
  "message": "Correction applied successfully"
}
```

#### Add Row
```
POST /api/add-row
Headers:
  - Authentication required
  - CSRF token required

Body:
{
  "data": ["John Doe", "john@example.com", "555-1234"]
}

Response:
{
  "success": true,
  "message": "Row added successfully"
}
```

#### Delete Row
```
DELETE /api/delete-row
Headers:
  - Authentication required
  - CSRF token required

Body:
{
  "row": 5,
  "reason": "Duplicate entry"
}

Response:
{
  "success": true,
  "message": "Row deleted successfully"
}
```

### Sheet Import

#### Preview Sheet
```
POST /api/preview-sheet
Headers:
  - Authentication required
  - CSRF token required

Body:
{
  "sheet_url": "https://docs.google.com/spreadsheets/d/..."
}

Response:
{
  "columns": ["Name", "Email", "Phone"],
  "row_count": 150,
  "sheet_name": "Employee Directory"
}
```

## Super Admin API

### Directory Management

#### Get All Directories
```
GET /api/super-admin/directories
Headers: Super admin authentication required

Response: Array of all directories
[
  {
    "id": "company-1",
    "name": "Company Directory",
    "description": "Employee directory",
    "database_path": "./data/company-1.db",
    "created_at": "2025-01-01T00:00:00Z"
  }
]
```

#### Create Directory
```
POST /api/super-admin/create-directory
Headers:
  - Super admin authentication required
  - CSRF token required

Body:
{
  "directory_id": "new-company",
  "directory_name": "New Company Directory",
  "description": "Directory for new company",
  "owner_email": "admin@newcompany.com"
}

Response:
{
  "success": true,
  "message": "Directory created successfully"
}
```

#### Delete Directory
```
DELETE /api/super-admin/delete-directory
Headers:
  - Super admin authentication required
  - CSRF token required

Body:
{
  "directory_id": "company-to-delete"
}

Response:
{
  "success": true,
  "message": "Directory deleted successfully"
}
```

## Moderator Management API

### Moderator Operations

#### Get Moderators
```
GET /api/moderators
Query Parameters:
  - dir: Directory ID
Headers: Admin or moderator authentication required

Response: Array of moderators
[
  {
    "id": 1,
    "user_email": "mod@example.com",
    "username": "moderator1",
    "auth_provider": "google",
    "can_edit": true,
    "can_approve": false,
    "requires_approval": true
  }
]
```

#### Appoint Moderator
```
POST /api/moderators/appoint
Headers:
  - Admin or moderator authentication required
  - CSRF token required

Body:
{
  "user_email": "newmod@example.com",
  "username": "newmoderator",
  "auth_provider": "google",
  "directory_id": "company-1",
  "can_edit": true,
  "can_approve": false,
  "requires_approval": true
}

Response:
{
  "success": true,
  "message": "Moderator appointed successfully"
}
```

#### Remove Moderator
```
DELETE /api/moderators/remove
Headers:
  - Admin or moderator authentication required
  - CSRF token required

Body:
{
  "moderator_email": "mod@example.com",
  "directory_id": "company-1"
}

Response:
{
  "success": true,
  "message": "Moderator removed successfully"
}
```

#### Get Moderator Permissions
```
GET /api/moderators/permissions
Query Parameters:
  - moderator_email: Email of moderator (optional, defaults to current user)
  - dir: Directory ID
Headers: Moderator authentication required

Response:
{
  "can_edit": true,
  "can_approve": false,
  "requires_approval": true,
  "assigned_rows": [1, 2, 3, 5]
}
```

### Change Approval

#### Get Pending Changes
```
GET /api/changes/pending
Query Parameters:
  - dir: Directory ID
Headers: Moderator authentication required

Response: Array of pending changes
[
  {
    "id": 1,
    "change_type": "update",
    "row_id": 5,
    "column_id": 2,
    "old_value": "old@example.com",
    "new_value": "new@example.com",
    "requested_by": "user@example.com",
    "created_at": "2025-01-01T12:00:00Z"
  }
]
```

#### Approve/Reject Change
```
POST /api/changes/approve
Headers:
  - Moderator authentication required
  - CSRF token required

Body:
{
  "change_id": 1,
  "action": "approve",  // or "reject"
  "reason": "Approved - valid email update"
}

Response:
{
  "success": true,
  "message": "Change processed successfully"
}
```

## File Downloads

#### Download Directory Database
```
GET /download/directory.db
Query Parameters:
  - dir: Directory ID (optional, defaults to "default")

Response: SQLite database file
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="directory.db"
```

## Response Formats

### Success Response
```json
{
  "success": true,
  "data": {...},
  "message": "Operation completed successfully"
}
```

### Error Response
```json
{
  "error": "Bad Request",
  "message": "Validation failed: Email is required",
  "code": 400
}
```

## Error Codes

- `400` - Bad Request (validation errors, malformed data)
- `401` - Unauthorized (authentication required)
- `403` - Forbidden (insufficient permissions)
- `404` - Not Found (resource not found)
- `429` - Too Many Requests (rate limited)
- `500` - Internal Server Error (server-side errors)

## Authentication & Authorization

### User Types
- **Super Admin**: Platform-wide access to all directories
- **Admin**: Directory owner with full access to their directories  
- **Moderator**: Limited access based on assigned permissions
- **User**: Read-only access (for future implementation)

### Permission Levels
- **can_edit**: Can modify directory data
- **can_approve**: Can approve changes from other moderators
- **requires_approval**: Changes require approval before being applied

### CSRF Protection
All state-changing operations (POST, PUT, DELETE) require CSRF tokens:
- Token provided in session context
- Must be included in request headers: `X-CSRF-Token: <token>`
- Tokens are validated by middleware

### Rate Limiting
- Applied to all endpoints
- Limits based on IP address
- Default: 100 requests per minute per IP
- Returns 429 status when exceeded