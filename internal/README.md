# Internal Package Structure

This directory contains the internal packages for the Directory Community Website application.

## Package Organization

### `/handlers`
HTTP request handlers organized by functionality:
- `auth.go` - Authentication and OAuth handlers
- `admin.go` - Admin panel handlers  
- `moderator.go` - Moderator management handlers
- `directory.go` - Directory and data management handlers
- `api.go` - API endpoint handlers

### `/services`
Business logic and service layer:
- `auth_service.go` - Authentication business logic
- `directory_service.go` - Directory management business logic
- `moderator_service.go` - Moderator system business logic
- `sheet_service.go` - Google Sheets integration
- `encryption_service.go` - Encryption and security services

### `/models`
Data structures and domain models:
- `user.go` - User and authentication models
- `directory.go` - Directory and data models
- `moderator.go` - Moderator and permission models
- `session.go` - Session management models

### `/middleware`
HTTP middleware components:
- `auth.go` - Authentication middleware
- `logging.go` - Request logging middleware
- `cache.go` - Static file caching middleware
- `rate_limit.go` - Rate limiting middleware
- `recovery.go` - Panic recovery middleware

### `/utils`
Utility functions and helpers:
- `validation.go` - Input validation utilities
- `crypto.go` - Cryptographic utilities
- `template.go` - Template helper functions
- `response.go` - HTTP response utilities

## Migration Plan

### Phase 3: Package Restructuring
1. Move handlers from root to `internal/handlers/`
2. Extract business logic to `internal/services/`
3. Define models in `internal/models/`
4. Organize middleware in `internal/middleware/`
5. Consolidate utilities in `internal/utils/`

### Benefits
- **Separation of Concerns**: Clear boundaries between layers
- **Testability**: Easier to unit test individual components
- **Maintainability**: Logical organization of related code
- **Scalability**: Foundation for larger application growth
- **Go Best Practices**: Follows standard Go project layout

### Import Structure
```
internal/
├── handlers/     (HTTP layer)
├── services/     (Business logic layer)  
├── models/       (Data layer)
├── middleware/   (HTTP middleware)
└── utils/        (Shared utilities)
```