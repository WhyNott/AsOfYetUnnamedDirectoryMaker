# Directory Community Website

A web service that transforms Google Sheets into searchable SQLite-powered directories with community correction features.

## Features

- **Admin Authentication**: Google OAuth login for administrators
- **Google Sheets Integration**: Import data directly from Google Sheets
- **SQLite Database**: Fast, local database storage with web access
- **Interactive Frontend**: sql.js-powered client-side database querying
- **Community Corrections**: Users can suggest corrections that automatically update the original Google Sheet
- **Real-time Updates**: Changes trigger automatic re-import of sheet data

## Setup

1. **Google Cloud Console Setup**:
   - Go to [Google Cloud Console](https://console.cloud.google.com/)
   - Create a new project or select existing one
   - Enable Google Sheets API and Google OAuth2 API
   - Create OAuth2 credentials (Web application)
   - Add `http://localhost:8080/auth/callback` to authorized redirect URIs

2. **Environment Configuration**:
   ```bash
   cp .env.example .env
   ```
   
   Edit `.env` and fill in your Google OAuth credentials:
   ```
   GOOGLE_CLIENT_ID=your_actual_client_id_here
   GOOGLE_CLIENT_SECRET=your_actual_client_secret_here  
   SESSION_SECRET=a_random_string_at_least_32_characters_long
   ```

3. **Install Dependencies**:
   ```bash
   go mod tidy
   ```

4. **Run the Application**:
   ```bash
   go run *.go
   ```

5. **Access the Application**:
   - Open http://localhost:8080
   - Go to Admin Panel to authenticate and import a Google Sheet
   - Users can view the directory and suggest corrections

## Usage

### For Administrators:
1. Visit `/admin` and authenticate with Google
2. Provide the URL of your Google Sheet
3. The sheet will be imported as an SQLite database
4. Users can now view and interact with your directory

### For Users:
1. Visit the homepage to view the directory
2. Use the search box to filter entries
3. Click on any cell to suggest a correction
4. Corrections are automatically applied to the original Google Sheet

## File Structure

- `main.go` - Main application and server setup
- `auth.go` - Google OAuth authentication handlers
- `sheets.go` - Google Sheets API integration
- `database.go` - SQLite operations and home page
- `corrections.go` - Correction submission system
- `static/app.js` - Frontend JavaScript with sql.js integration

## Requirements

- Go 1.18+
- Google Cloud Project with Sheets API enabled
- Google OAuth2 credentials