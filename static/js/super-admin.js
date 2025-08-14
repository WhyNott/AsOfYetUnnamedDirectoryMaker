var csrfToken = '';

window.onload = function() {
    // Get CSRF token from global config or meta tag
    if (window.adminConfig && window.adminConfig.csrfToken) {
        csrfToken = window.adminConfig.csrfToken;
    } else {
        var meta = document.querySelector('meta[name="csrf-token"]');
        if (meta) {
            csrfToken = meta.getAttribute('content');
        }
    }
    loadDirectories();
};

function createDirectory() {
    var form = document.getElementById('createDirectoryForm');
    var formData = new FormData(form);
    var loadingEl = document.getElementById('createLoading');
    loadingEl.style.display = 'block';
    
    var data = {
        directory_id: formData.get('directory_id'),
        directory_name: formData.get('directory_name'),
        owner_email: formData.get('owner_email'),
        description: formData.get('description') || ''
    };
    
    var xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/admin/create-directory');
    xhr.setRequestHeader('Content-Type', 'application/json');
    xhr.setRequestHeader('X-CSRF-Token', csrfToken);
    xhr.onreadystatechange = function() {
        if (xhr.readyState === 4) {
            loadingEl.style.display = 'none';
            if (xhr.status === 200) {
                alert('Directory created successfully!');
                form.reset();
                loadDirectories();
            } else {
                alert('Error: ' + xhr.responseText);
            }
        }
    };
    xhr.send(JSON.stringify(data));
}

function loadDirectories() {
    var loadingEl = document.getElementById('directoriesLoading');
    var containerEl = document.getElementById('directoriesContainer');
    
    loadingEl.style.display = 'block';
    containerEl.style.display = 'none';
    
    var xhr = new XMLHttpRequest();
    xhr.open('GET', '/api/admin/directories');
    xhr.onreadystatechange = function() {
        if (xhr.readyState === 4) {
            loadingEl.style.display = 'none';
            containerEl.style.display = 'grid';
            if (xhr.status === 200) {
                var directories = JSON.parse(xhr.responseText);
                renderDirectories(directories);
            } else {
                containerEl.innerHTML = '<p style="color: #dc3545;">Failed to load directories</p>';
            }
        }
    };
    xhr.send();
}

function renderDirectories(directories) {
    var container = document.getElementById('directoriesContainer');
    if (directories.length === 0) {
        container.innerHTML = '<p>No directories found</p>';
        return;
    }
    var html = '';
    for (var i = 0; i < directories.length; i++) {
        html += createDirectoryCard(directories[i]);
    }
    container.innerHTML = html;
}

function createDirectoryCard(directory) {
    var createdDate = new Date(directory.created_at).toLocaleDateString();
    var html = '<div class="directory-card">';
    html += '<h3>' + escapeHtml(directory.name) + '</h3>';
    html += '<div class="directory-info">';
    html += '<strong>ID:</strong> ' + escapeHtml(directory.id) + '<br>';
    html += '<strong>Created:</strong> ' + createdDate + '<br>';
    html += '<strong>Database:</strong> ' + escapeHtml(directory.database_path);
    html += '</div>';
    if (directory.description) {
        html += '<p style="font-size: 14px; color: #666; margin: 10px 0;">' + escapeHtml(directory.description) + '</p>';
    }
    html += '<div class="directory-actions">';
    html += '<a href="/?dir=' + encodeURIComponent(directory.id) + '" class="btn btn-primary">View</a>';
    html += '<a href="/admin?dir=' + encodeURIComponent(directory.id) + '" class="btn btn-success">Manage</a>';
    if (directory.id !== 'default') {
        html += '<button onclick="deleteDirectory(\'' + escapeHtml(directory.id) + '\')" class="btn btn-danger">Delete</button>';
    }
    html += '</div></div>';
    return html;
}

function deleteDirectory(directoryId) {
    if (!confirm('Are you sure you want to delete directory "' + directoryId + '"? This action cannot be undone and will delete all data in the directory.')) {
        return;
    }
    
    var data = {
        directory_id: directoryId
    };
    
    var xhr = new XMLHttpRequest();
    xhr.open('DELETE', '/api/admin/delete-directory');
    xhr.setRequestHeader('Content-Type', 'application/json');
    xhr.setRequestHeader('X-CSRF-Token', csrfToken);
    xhr.onreadystatechange = function() {
        if (xhr.readyState === 4) {
            if (xhr.status === 200) {
                alert('Directory deleted successfully!');
                loadDirectories();
            } else {
                alert('Error: ' + xhr.responseText);
            }
        }
    };
    xhr.send(JSON.stringify(data));
}

function escapeHtml(text) {
    var div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}