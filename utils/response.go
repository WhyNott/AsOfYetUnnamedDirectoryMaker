package utils

import (
	"encoding/json"
	"log"
	"net/http"
)

// ErrorResponse represents a standardized error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Code    int    `json:"code"`
}

// SuccessResponse represents a standardized success response
type SuccessResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
}

// RespondWithError sends a standardized error response
func RespondWithError(w http.ResponseWriter, code int, message string) {
	log.Printf("API Error (%d): %s", code, message)
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	
	response := ErrorResponse{
		Error:   getErrorType(code),
		Message: message,
		Code:    code,
	}
	
	json.NewEncoder(w).Encode(response)
}

// RespondWithJSON sends a standardized JSON response
func RespondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// RespondWithSuccess sends a standardized success response
func RespondWithSuccess(w http.ResponseWriter, data interface{}, message string) {
	response := SuccessResponse{
		Success: true,
		Data:    data,
		Message: message,
	}
	
	RespondWithJSON(w, http.StatusOK, response)
}

// Common error response functions
func AuthenticationError(w http.ResponseWriter) {
	RespondWithError(w, http.StatusUnauthorized, "Authentication required")
}

func AuthorizationError(w http.ResponseWriter) {
	RespondWithError(w, http.StatusForbidden, "Insufficient permissions")
}

func BadRequestError(w http.ResponseWriter, message string) {
	RespondWithError(w, http.StatusBadRequest, message)
}

func NotFoundError(w http.ResponseWriter, resource string) {
	RespondWithError(w, http.StatusNotFound, resource+" not found")
}

func InternalServerError(w http.ResponseWriter, message string) {
	RespondWithError(w, http.StatusInternalServerError, message)
}

func ValidationError(w http.ResponseWriter, message string) {
	RespondWithError(w, http.StatusBadRequest, "Validation failed: "+message)
}

func DatabaseError(w http.ResponseWriter) {
	RespondWithError(w, http.StatusInternalServerError, "Database operation failed")
}

// RequireAuthentication is a helper that checks authentication and responds with error if not authenticated
func RequireAuthentication(w http.ResponseWriter, r *http.Request) (string, bool) {
	userEmail, ok := GetUserEmail(r)
	if !ok {
		AuthenticationError(w)
		return "", false
	}
	return userEmail, true
}

// RequireCSRFToken is a helper that checks CSRF token and responds with error if missing
func RequireCSRFToken(w http.ResponseWriter, r *http.Request) (string, bool) {
	token, ok := GetCSRFToken(r)
	if !ok {
		RespondWithError(w, http.StatusBadRequest, "CSRF token required")
		return "", false
	}
	return token, true
}

// getErrorType returns a human-readable error type based on status code
func getErrorType(code int) string {
	switch code {
	case http.StatusBadRequest:
		return "Bad Request"
	case http.StatusUnauthorized:
		return "Unauthorized" 
	case http.StatusForbidden:
		return "Forbidden"
	case http.StatusNotFound:
		return "Not Found"
	case http.StatusMethodNotAllowed:
		return "Method Not Allowed"
	case http.StatusConflict:
		return "Conflict"
	case http.StatusTooManyRequests:
		return "Rate Limited"
	case http.StatusInternalServerError:
		return "Internal Server Error"
	case http.StatusBadGateway:
		return "Bad Gateway"
	case http.StatusServiceUnavailable:
		return "Service Unavailable"
	default:
		return "Error"
	}
}