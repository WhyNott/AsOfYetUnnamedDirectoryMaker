package main

import (
	"directoryCommunityWebsite/internal/utils"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
)

// TemplateCache holds parsed templates with inheritance support
type TemplateCache struct {
	templates map[string]*template.Template
	mutex     sync.RWMutex
}

// NewTemplateCache creates a new template cache
func NewTemplateCache() *TemplateCache {
	return &TemplateCache{
		templates: make(map[string]*template.Template),
	}
}

// GetTemplate returns a cached template or loads it if not cached
func (tc *TemplateCache) GetTemplate(name string) (*template.Template, error) {
	tc.mutex.RLock()
	tmpl, exists := tc.templates[name]
	tc.mutex.RUnlock()

	if exists {
		return tmpl, nil
	}

	// Load template with inheritance
	tc.mutex.Lock()
	defer tc.mutex.Unlock()

	// Double-check after acquiring write lock
	if tmpl, exists := tc.templates[name]; exists {
		return tmpl, nil
	}

	// Parse template with base template and helper functions
	templatePath := filepath.Join("templates", name+".html")
	basePath := filepath.Join("templates", "base.html")

	tmpl, err := template.New("").Funcs(CreateTemplateFuncMap()).ParseFiles(basePath, templatePath)
	if err != nil {
		log.Printf("Failed to parse template %s: %v", name, err)
		return nil, err
	}

	tc.templates[name] = tmpl
	return tmpl, nil
}

// RenderTemplate renders a template with the given data
func (tc *TemplateCache) RenderTemplate(w http.ResponseWriter, name string, data interface{}) error {
	tmpl, err := tc.GetTemplate(name)
	if err != nil {
		return err
	}

	return tmpl.ExecuteTemplate(w, "base.html", data)
}

// ClearCache clears the template cache (useful for development)
func (tc *TemplateCache) ClearCache() {
	tc.mutex.Lock()
	defer tc.mutex.Unlock()
	tc.templates = make(map[string]*template.Template)
}

// Global template cache
var templateCache *TemplateCache

func init() {
	templateCache = NewTemplateCache()
}

// Template helper functions for common UI patterns

// UserBadge creates a user role badge HTML
func UserBadge(userType string) template.HTML {
	badges := map[string]string{
		"admin":     `<span class="admin-badge">ADMIN</span>`,
		"owner":     `<span class="owner-badge">OWNER</span>`,
		"moderator": `<span class="moderator-badge">MODERATOR</span>`,
	}

	if badge, exists := badges[userType]; exists {
		return template.HTML(badge)
	}
	return ""
}

// AlertBox creates an alert message HTML
func AlertBox(alertType, message, linkText, linkURL string) template.HTML {
	linkHTML := ""
	if linkText != "" && linkURL != "" {
		linkHTML = fmt.Sprintf(`<a href="%s" class="alert-link">%s</a>`, linkURL, linkText)
	}

	return template.HTML(fmt.Sprintf(
		`<div class="alert alert-%s">%s %s</div>`,
		alertType, message, linkHTML,
	))
}

// FormGroup creates a form input group HTML
func FormGroup(label, inputType, inputID, placeholder, value string, required bool) template.HTML {
	requiredAttr := ""
	if required {
		requiredAttr = "required"
	}

	return template.HTML(fmt.Sprintf(`
		<div class="form-group">
			<label for="%s">%s:</label>
			<input type="%s" id="%s" name="%s" placeholder="%s" value="%s" %s>
		</div>
	`, inputID, label, inputType, inputID, inputID, placeholder, value, requiredAttr))
}

// SelectGroup creates a form select group HTML
func SelectGroup(label, selectID string, options map[string]string, selected string, required bool) template.HTML {
	requiredAttr := ""
	if required {
		requiredAttr = "required"
	}

	optionsHTML := ""
	for value, text := range options {
		selectedAttr := ""
		if value == selected {
			selectedAttr = "selected"
		}
		optionsHTML += fmt.Sprintf(`<option value="%s" %s>%s</option>`, value, selectedAttr, text)
	}

	return template.HTML(fmt.Sprintf(`
		<div class="form-group">
			<label for="%s">%s:</label>
			<select id="%s" name="%s" %s>
				%s
			</select>
		</div>
	`, selectID, label, selectID, selectID, requiredAttr, optionsHTML))
}

// NavButton creates a navigation button HTML
func NavButton(text, url, buttonType string, condition bool) template.HTML {
	if !condition {
		return ""
	}

	class := "btn"
	if buttonType != "" {
		class += " btn-" + buttonType
	}

	return template.HTML(fmt.Sprintf(
		`<a href="%s" class="%s">%s</a>`,
		url, class, text,
	))
}

// DirectoryInfoHTML creates directory information display HTML
func DirectoryInfoHTML(name, id, description string) template.HTML {
	descHTML := ""
	if description != "" {
		descHTML = fmt.Sprintf(" • %s", description)
	}

	return template.HTML(fmt.Sprintf(`
		<div>
			<h1>%s</h1>
			<div class="directory-info">
				Directory: <strong>%s</strong>%s
			</div>
		</div>
	`, name, id, descHTML))
}

// UserInfo creates user information display HTML
func UserInfo(email string, userTypes []string) template.HTML {
	badgeHTML := ""
	for _, userType := range userTypes {
		badgeHTML += string(UserBadge(userType))
	}

	return template.HTML(fmt.Sprintf(`
		<span class="user-info">
			Logged in as: <strong>%s</strong>%s
		</span>
	`, email, badgeHTML))
}

// Icon creates an icon HTML (for future use with icon fonts)
func Icon(iconName string) template.HTML {
	return template.HTML(fmt.Sprintf(`<i class="icon icon-%s"></i>`, iconName))
}

// ConditionalClass adds a CSS class conditionally
func ConditionalClass(baseClass, conditionalClass string, condition bool) string {
	if condition {
		return baseClass + " " + conditionalClass
	}
	return baseClass
}

// Truncate truncates a string to specified length
func Truncate(text string, length int) string {
	if len(text) <= length {
		return text
	}
	return text[:length] + "..."
}

// Join joins a slice of strings with separator
func Join(items []string, separator string) string {
	return strings.Join(items, separator)
}

// CreateTemplateFuncMap creates a function map for templates
func CreateTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"userBadge":        UserBadge,
		"alertBox":         AlertBox,
		"formGroup":        FormGroup,
		"selectGroup":      SelectGroup,
		"navButton":        NavButton,
		"directoryInfo":    DirectoryInfoHTML,
		"userInfo":         UserInfo,
		"icon":             Icon,
		"conditionalClass": ConditionalClass,
		"truncate":         Truncate,
		"join":             Join,
	}
}

// TemplateData represents the common data structure for all templates
type TemplateData struct {
	// User information
	UserEmail        string
	IsAuthenticated  bool
	IsAdmin          bool
	IsDirectoryOwner bool
	IsModerator      bool
	UserType         string

	// Directory information
	Directory   *DirectoryInfo
	DirectoryID string

	// Security
	CSRFToken string

	// URLs
	AdminURL         string
	ViewDirectoryURL string
	DownloadURL      string
	ImportURL        string
	PreviewURL       string

	// Status/Messages
	ImportSuccess bool

	// Page-specific data
	PageData interface{}
}

// DirectoryInfo represents directory metadata
type DirectoryInfo struct {
	ID          string
	Name        string
	Description string
}

// BuildTemplateData builds common template data from request context
func (app *App) BuildTemplateData(r *http.Request, pageData interface{}) (*TemplateData, error) {
	ctx := r.Context()

	// Get directory ID
	directoryID := r.URL.Query().Get("dir")
	if directoryID == "" {
		directoryID = "default"
	}

	// Get directory information
	directory, err := app.GetDirectory(directoryID)
	if err != nil {
		log.Printf("Failed to get directory %s: %v", directoryID, err)
		// Create default directory info
		directory = &Directory{
			ID:          directoryID,
			Name:        "Directory",
			Description: "",
		}
	}

	// Build template data
	data := &TemplateData{
		DirectoryID: directoryID,
		Directory: &DirectoryInfo{
			ID:          directory.ID,
			Name:        directory.Name,
			Description: directory.Description,
		},
		PageData: pageData,
	}

	// Get user information from context
	if userEmail, ok := ctx.Value(utils.UserEmailKey).(string); ok {
		data.UserEmail = userEmail
		data.IsAuthenticated = true

		// Get permissions from context (set by TemplateContextMiddleware)
		data.IsAdmin, _ = ctx.Value(utils.IsAdminKey).(bool)
		data.IsModerator, _ = ctx.Value(utils.IsModeratorKey).(bool)
		data.IsDirectoryOwner, _ = ctx.Value(utils.IsDirectoryOwnerKey).(bool)
		data.UserType, _ = ctx.Value(utils.UserTypeKey).(string)
	}

	// Get CSRF token from context
	data.CSRFToken, _ = ctx.Value(utils.CSRFTokenKey).(string)

	// Build URLs
	data.AdminURL = "/owner"
	data.ViewDirectoryURL = "/"
	data.DownloadURL = "/download"
	data.ImportURL = "/import"
	data.PreviewURL = "/preview"

	if directoryID != "default" {
		data.AdminURL += "?dir=" + directoryID
		data.ViewDirectoryURL += "?dir=" + directoryID
		data.DownloadURL += "?dir=" + directoryID
		data.ImportURL += "?dir=" + directoryID
		data.PreviewURL += "?dir=" + directoryID
	}

	// Check for import success flag
	data.ImportSuccess = r.URL.Query().Get("imported") == "true"

	return data, nil
}

// RenderTemplateWithContext renders a template with automatic context data
func (app *App) RenderTemplateWithContext(w http.ResponseWriter, r *http.Request, templateName string, pageData interface{}) error {
	// Build template data automatically
	data, err := app.BuildTemplateData(r, pageData)
	if err != nil {
		return err
	}

	// Use the template cache to render
	return templateCache.RenderTemplate(w, templateName, data)
}
