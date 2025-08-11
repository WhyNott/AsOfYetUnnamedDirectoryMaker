package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
)

func (app *App) handleGetDirectory(w http.ResponseWriter, r *http.Request) {
	rows, err := app.DB.Query("SELECT id, data FROM directory ORDER BY id")
	if err != nil {
		log.Printf("Failed to query directory: %v", err)
		http.Error(w, "Failed to query directory", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}()

	var entries []DirectoryEntry
	for rows.Next() {
		var entry DirectoryEntry
		if err := rows.Scan(&entry.ID, &entry.Data); err != nil {
			log.Printf("Failed to scan directory row: %v", err)
			http.Error(w, "Failed to scan row", http.StatusInternalServerError)
			return
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Row iteration error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if err := json.NewEncoder(w).Encode(entries); err != nil {
		log.Printf("Failed to encode directory entries: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (app *App) handleDownloadDB(w http.ResponseWriter, r *http.Request) {
	file, err := os.Open(app.Config.DatabasePath)
	if err != nil {
		log.Printf("Failed to open database file for download: %v", err)
		http.Error(w, "Database file not found", http.StatusNotFound)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close database file: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=directory.db")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	http.ServeFile(w, r, app.Config.DatabasePath)
}

func (app *App) handleHome(w http.ResponseWriter, r *http.Request) {
	// Try to get CSRF token from session if user is authenticated
	var csrfToken string
	if session, err := app.SessionStore.Get(r, "auth-session"); err == nil {
		if sessionDataJSON, ok := session.Values["session_data"].(string); ok {
			var sessionData SessionData
			if json.Unmarshal([]byte(sessionDataJSON), &sessionData) == nil {
				csrfToken = sessionData.CSRFToken
			}
		}
	}

	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>Directory Service</title>
    <meta name="csrf-token" content="{{.CSRFToken}}">
    <style>
        body { 
            font-family: Arial, sans-serif; 
            margin: 0; 
            padding: 20px; 
            background-color: #f5f5f5; 
        }
        .container { 
            max-width: 1200px; 
            margin: 0 auto; 
            background: white; 
            padding: 20px; 
            border-radius: 8px; 
            box-shadow: 0 2px 4px rgba(0,0,0,0.1); 
        }
        .header { 
            display: flex; 
            justify-content: space-between; 
            align-items: center; 
            margin-bottom: 20px; 
            padding-bottom: 20px; 
            border-bottom: 1px solid #eee; 
        }
        .controls { 
            margin-bottom: 20px; 
        }
        .search-box { 
            padding: 10px; 
            width: 300px; 
            border: 1px solid #ddd; 
            border-radius: 4px; 
            margin-right: 10px; 
        }
        .add-row-btn { 
            padding: 10px 20px; 
            background: #28a745; 
            color: white; 
            border: none; 
            border-radius: 4px; 
            cursor: pointer; 
            margin-right: 10px; 
        }
        .add-row-btn:hover { 
            background: #218838; 
        }
        .download-btn, .admin-btn { 
            padding: 10px 20px; 
            background: #007cba; 
            color: white; 
            text-decoration: none; 
            border-radius: 4px; 
            margin-left: 10px; 
        }
        .download-btn:hover, .admin-btn:hover { 
            background: #005a87; 
        }
        #directoryTable { 
            width: 100%; 
            border-collapse: collapse; 
            margin-top: 20px; 
        }
        #directoryTable th, #directoryTable td { 
            border: 1px solid #ddd; 
            padding: 8px; 
            text-align: left; 
            position: relative; 
        }
        #directoryTable td:not(.delete-cell) { 
            cursor: pointer; 
        }
        #directoryTable th { 
            background-color: #f2f2f2; 
            font-weight: bold; 
        }
        #directoryTable td:hover { 
            background-color: #f0f8ff; 
        }
        .modal {
            display: none;
            position: fixed;
            z-index: 1;
            left: 0;
            top: 0;
            width: 100%;
            height: 100%;
            background-color: rgba(0,0,0,0.4);
        }
        .modal-content {
            background-color: #fefefe;
            margin: 15% auto;
            padding: 20px;
            border: 1px solid #888;
            width: 400px;
            border-radius: 8px;
        }
        .close {
            color: #aaa;
            float: right;
            font-size: 28px;
            font-weight: bold;
            cursor: pointer;
        }
        .close:hover { color: black; }
        .modal input[type="text"] {
            width: 100%;
            padding: 10px;
            margin: 10px 0;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
        .modal button {
            padding: 10px 20px;
            background: #007cba;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            margin-right: 10px;
        }
        .modal button:hover { background: #005a87; }
        .modal button.cancel { background: #666; }
        .modal button.cancel:hover { background: #555; }
        .delete-btn {
            background: #dc3545;
            border: none;
            border-radius: 3px;
            color: white;
            cursor: pointer;
            font-size: 12px;
            padding: 4px 8px;
            margin-left: 8px;
            opacity: 0;
            transition: opacity 0.2s, background-color 0.2s;
        }
        .delete-btn:hover {
            background: #c82333;
        }
        tr:hover .delete-btn {
            opacity: 1;
        }
        .delete-cell {
            width: 40px;
            text-align: center;
            cursor: default !important;
            background: #f8f9fa !important;
        }
        .delete-icon {
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Community Directory</h1>
            <div>
                <a href="/download/directory.db" class="download-btn">Download Database</a>
                <a href="/admin" class="admin-btn">Admin Panel</a>
            </div>
        </div>
        
        <div class="controls">
            <input type="text" id="searchBox" class="search-box" placeholder="Search directory...">
            <button id="addRowBtn" class="add-row-btn">Add New Row</button>
            <span id="recordCount"></span>
        </div>
        
        <div id="loading">Loading directory...</div>
        <table id="directoryTable" style="display: none;">
            <thead id="tableHeader"></thead>
            <tbody id="tableBody"></tbody>
        </table>
    </div>

    <!-- Correction Modal -->
    <div id="correctionModal" class="modal">
        <div class="modal-content">
            <span class="close">&times;</span>
            <h2>Suggest Correction</h2>
            <p>Current value: <strong id="currentValue"></strong></p>
            <label for="newValue">New value:</label>
            <input type="text" id="newValue" />
            <div style="margin-top: 20px;">
                <button id="submitCorrection">Submit Correction</button>
                <button id="cancelCorrection" class="cancel">Cancel</button>
            </div>
        </div>
    </div>

    <!-- Add Row Modal -->
    <div id="addRowModal" class="modal">
        <div class="modal-content" style="width: 600px;">
            <span class="close" id="closeAddRow">&times;</span>
            <h2>Add New Row</h2>
            <p>Fill in the values for each column:</p>
            <div id="addRowInputs"></div>
            <div style="margin-top: 20px;">
                <button id="submitNewRow">Add Row</button>
                <button id="cancelNewRow" class="cancel">Cancel</button>
            </div>
        </div>
    </div>

    <!-- Delete Row Modal -->
    <div id="deleteRowModal" class="modal">
        <div class="modal-content">
            <span class="close" id="closeDeleteRow">&times;</span>
            <h2>Delete Row</h2>
            <p><strong>Warning:</strong> This will permanently delete the selected row.</p>
            <p>Row data: <span id="deleteRowData" style="font-style: italic; color: #666;"></span></p>
            <label for="deleteReason">Reason for deletion (optional):</label>
            <input type="text" id="deleteReason" placeholder="Enter reason for deleting this row..." maxlength="500" />
            <div style="margin-top: 20px;">
                <button id="confirmDelete" style="background: #dc3545;">Delete Row</button>
                <button id="cancelDelete" class="cancel">Cancel</button>
            </div>
        </div>
    </div>

    <script src="https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.8.0/sql-wasm.js"></script>
    <script src="/static/app.js"></script>
</body>
</html>`

	t, err := template.New("home").Parse(tmpl)
	if err != nil {
		log.Printf("Failed to parse home template: %v", err)
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	data := struct {
		CSRFToken string
	}{CSRFToken: csrfToken}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("Failed to execute home template: %v", err)
		http.Error(w, "Template execution error", http.StatusInternalServerError)
		return
	}
}
