package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validator provides comprehensive input validation
type Validator struct {
	errors []string
}

// NewValidator creates a new validator instance
func NewValidator() *Validator {
	return &Validator{
		errors: make([]string, 0),
	}
}

// AddError adds a validation error
func (v *Validator) AddError(message string) {
	v.errors = append(v.errors, message)
}

// HasErrors returns true if there are validation errors
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Errors returns all validation errors
func (v *Validator) Errors() []string {
	return v.errors
}

// ErrorString returns all errors as a single string
func (v *Validator) ErrorString() string {
	return strings.Join(v.errors, "; ")
}

// String validation methods

// ValidateRequired checks if a string is not empty
func (v *Validator) ValidateRequired(value, field string) *Validator {
	if strings.TrimSpace(value) == "" {
		v.AddError(fmt.Sprintf("%s is required", field))
	}
	return v
}

// ValidateLength checks string length constraints
func (v *Validator) ValidateLength(value, field string, min, max int) *Validator {
	length := utf8.RuneCountInString(value)
	if length < min {
		v.AddError(fmt.Sprintf("%s must be at least %d characters long", field, min))
	}
	if max > 0 && length > max {
		v.AddError(fmt.Sprintf("%s must be no more than %d characters long", field, max))
	}
	return v
}

// ValidateEmail validates email format
func (v *Validator) ValidateEmail(email, field string) *Validator {
	if email == "" {
		return v
	}
	
	// Enhanced email validation
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	
	if !emailRegex.MatchString(email) {
		v.AddError(fmt.Sprintf("%s must be a valid email address", field))
		return v
	}
	
	// Additional checks
	if len(email) > 320 { // RFC 5321 limit
		v.AddError(fmt.Sprintf("%s is too long (maximum 320 characters)", field))
	}
	
	return v
}

// ValidateURL validates URL format and schemes
func (v *Validator) ValidateURL(rawURL, field string, allowedSchemes ...string) *Validator {
	if rawURL == "" {
		return v
	}
	
	u, err := url.Parse(rawURL)
	if err != nil {
		v.AddError(fmt.Sprintf("%s must be a valid URL", field))
		return v
	}
	
	if u.Scheme == "" {
		v.AddError(fmt.Sprintf("%s must include a scheme (http/https)", field))
		return v
	}
	
	if len(allowedSchemes) > 0 {
		schemeAllowed := false
		for _, scheme := range allowedSchemes {
			if u.Scheme == scheme {
				schemeAllowed = true
				break
			}
		}
		if !schemeAllowed {
			v.AddError(fmt.Sprintf("%s must use one of the following schemes: %s", field, strings.Join(allowedSchemes, ", ")))
		}
	}
	
	if u.Host == "" {
		v.AddError(fmt.Sprintf("%s must include a valid host", field))
	}
	
	return v
}

// ValidateGoogleSheetsURL validates Google Sheets URL specifically
func (v *Validator) ValidateGoogleSheetsURL(sheetURL, field string) *Validator {
	if sheetURL == "" {
		return v
	}
	
	// First validate as URL
	v.ValidateURL(sheetURL, field, "https")
	if v.HasErrors() {
		return v
	}
	
	// Check if it's a Google Sheets URL
	sheetRegex := regexp.MustCompile(`^https://docs\.google\.com/spreadsheets/d/[a-zA-Z0-9-_]+`)
	if !sheetRegex.MatchString(sheetURL) {
		v.AddError(fmt.Sprintf("%s must be a valid Google Sheets URL", field))
	}
	
	return v
}

// ValidateDirectoryID validates directory ID format
func (v *Validator) ValidateDirectoryID(id, field string) *Validator {
	if id == "" {
		return v
	}
	
	if len(id) < 1 || len(id) > 50 {
		v.AddError(fmt.Sprintf("%s must be between 1 and 50 characters long", field))
	}
	
	// Allow letters, numbers, hyphens, and underscores
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
	if !matched {
		v.AddError(fmt.Sprintf("%s can only contain letters, numbers, hyphens, and underscores", field))
	}
	
	// Cannot start or end with hyphen
	if strings.HasPrefix(id, "-") || strings.HasSuffix(id, "-") {
		v.AddError(fmt.Sprintf("%s cannot start or end with a hyphen", field))
	}
	
	return v
}

// ValidateNoSQL validates that input doesn't contain SQL injection patterns
func (v *Validator) ValidateNoSQL(value, field string) *Validator {
	if value == "" {
		return v
	}
	
	// Common SQL injection patterns
	sqlPatterns := []string{
		`(?i)\b(SELECT|INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|EXEC|UNION|SCRIPT)\b`,
		`(?i)\b(OR|AND)\s+\d+\s*=\s*\d+`,
		`(?i)\b(OR|AND)\s+\d+\s*[<>=]+\s*\d+`,
		`['"]\s*(OR|AND|;|--|\||&)`,
		`\b(xp_|sp_)\w+`,
	}
	
	for _, pattern := range sqlPatterns {
		matched, _ := regexp.MatchString(pattern, value)
		if matched {
			v.AddError(fmt.Sprintf("%s contains potentially dangerous content", field))
			break
		}
	}
	
	return v
}

// ValidateNoXSS validates that input doesn't contain XSS patterns
func (v *Validator) ValidateNoXSS(value, field string) *Validator {
	if value == "" {
		return v
	}
	
	// Common XSS patterns
	xssPatterns := []string{
		`(?i)<script[^>]*>.*?</script>`,
		`(?i)<iframe[^>]*>.*?</iframe>`,
		`(?i)<object[^>]*>.*?</object>`,
		`(?i)<embed[^>]*>.*?</embed>`,
		`(?i)<link[^>]*>`,
		`(?i)javascript:`,
		`(?i)vbscript:`,
		`(?i)onload\s*=`,
		`(?i)onerror\s*=`,
		`(?i)onclick\s*=`,
	}
	
	for _, pattern := range xssPatterns {
		matched, _ := regexp.MatchString(pattern, value)
		if matched {
			v.AddError(fmt.Sprintf("%s contains potentially dangerous content", field))
			break
		}
	}
	
	return v
}

// ValidateSafeText validates text for general safety
func (v *Validator) ValidateSafeText(value, field string) *Validator {
	if value == "" {
		return v
	}
	
	// Check for excessive control characters
	controlCharCount := 0
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			controlCharCount++
		}
	}
	
	if controlCharCount > 0 {
		v.AddError(fmt.Sprintf("%s contains invalid characters", field))
	}
	
	return v
}

// Numeric validation methods

// ValidateRange validates that a number is within a specified range
func (v *Validator) ValidateRange(value int, field string, min, max int) *Validator {
	if value < min {
		v.AddError(fmt.Sprintf("%s must be at least %d", field, min))
	}
	if value > max {
		v.AddError(fmt.Sprintf("%s must be no more than %d", field, max))
	}
	return v
}

// Collection validation methods

// ValidateStringSlice validates a slice of strings
func (v *Validator) ValidateStringSlice(values []string, field string, maxItems int, itemValidator func(string) error) *Validator {
	if len(values) > maxItems {
		v.AddError(fmt.Sprintf("%s cannot have more than %d items", field, maxItems))
	}
	
	for i, value := range values {
		if itemValidator != nil {
			if err := itemValidator(value); err != nil {
				v.AddError(fmt.Sprintf("%s[%d]: %s", field, i, err.Error()))
			}
		}
	}
	
	return v
}

// Enhanced validation functions that replace existing ones

// Enhanced ValidateRowData with better validation
func ValidateRowDataEnhanced(data []string) error {
	validator := NewValidator()
	
	validator.ValidateStringSlice(data, "Row data", 100, func(cell string) error {
		cellValidator := NewValidator()
		cellValidator.ValidateLength(cell, "Cell", 0, 2000)
		cellValidator.ValidateNoXSS(cell, "Cell")
		cellValidator.ValidateSafeText(cell, "Cell")
		
		if cellValidator.HasErrors() {
			return fmt.Errorf(cellValidator.ErrorString())
		}
		return nil
	})
	
	if validator.HasErrors() {
		return fmt.Errorf(validator.ErrorString())
	}
	
	return nil
}

// Enhanced email validation that replaces ValidateEmail function
func ValidateEmailEnhanced(email string) bool {
	validator := NewValidator()
	validator.ValidateEmail(email, "Email")
	return !validator.HasErrors()
}

// Enhanced sheet URL validation that replaces ValidateSheetURL function  
func ValidateSheetURLEnhanced(url string) bool {
	validator := NewValidator()
	validator.ValidateGoogleSheetsURL(url, "Sheet URL")
	return !validator.HasErrors()
}

// Enhanced input sanitization
func SanitizeInputEnhanced(input string) string {
	// Trim whitespace
	input = strings.TrimSpace(input)
	
	// Remove null bytes and other control characters except newlines and tabs
	var result strings.Builder
	for _, r := range input {
		if !unicode.IsControl(r) || r == '\n' || r == '\r' || r == '\t' {
			result.WriteRune(r)
		}
	}
	
	sanitized := result.String()
	
	// Limit length as a safety measure
	if len(sanitized) > 10000 {
		sanitized = sanitized[:10000]
	}
	
	return sanitized
}