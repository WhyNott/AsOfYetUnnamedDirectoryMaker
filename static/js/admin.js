// Admin Panel JavaScript
// Uses global window.adminConfig set by template

const { csrfToken, previewURL, importURL, adminURL, directoryId } = window.adminConfig;

// Sheet Import Functions
document.getElementById('previewBtn').addEventListener('click', async function() {
    const sheetUrl = document.getElementById('sheet_url').value;
    
    if (!sheetUrl) {
        alert('Please enter a Google Sheets URL');
        return;
    }
    
    this.textContent = 'Loading...';
    this.disabled = true;
    
    try {
        const response = await fetch(previewURL, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfToken
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                sheet_url: sheetUrl
            })
        });
        
        if (response.ok) {
            const preview = await response.json();
            showPreview(preview);
        } else {
            const errorText = await response.text();
            alert('Preview failed: ' + errorText);
        }
    } catch (error) {
        console.error('Preview error:', error);
        alert('Network error. Please try again.');
    } finally {
        this.textContent = 'Preview Sheet';
        this.disabled = false;
    }
});

document.getElementById('confirmImport').addEventListener('click', async function() {
    const form = document.getElementById('importForm');
    const formData = new FormData(form);
    
    try {
        const response = await fetch(importURL, {
            method: 'POST',
            body: formData,
            credentials: 'same-origin'
        });
        
        if (response.redirected) {
            // Follow the redirect manually
            window.location.href = response.url;
        } else if (response.ok) {
            // If no redirect but successful, go to admin with success
            window.location.href = adminURL + '&imported=true';
        } else {
            alert('Import failed: ' + await response.text());
        }
    } catch (error) {
        console.error('Import error:', error);
        alert('Import failed due to network error');
    }
});

document.getElementById('cancelPreview').addEventListener('click', function() {
    document.getElementById('previewSection').style.display = 'none';
    document.getElementById('previewBtn').style.display = 'inline-block';
    document.getElementById('importBtn').style.display = 'none';
});

function showPreview(preview) {
    const content = document.getElementById('previewContent');
    
    let html = '<div style="margin-bottom: 10px;">';
    html += '<strong>Sheet Name:</strong> ' + escapeHtml(preview.sheet_name) + '<br>';
    html += '<strong>Data Rows:</strong> ' + preview.row_count + '<br>';
    html += '<strong>Columns (' + preview.columns.length + '):</strong>';
    html += '</div>';
    
    html += '<div style="display: flex; flex-wrap: wrap; gap: 8px; margin-top: 10px;">';
    preview.columns.forEach((column, index) => {
        html += '<span style="background: #e3f2fd; padding: 4px 8px; border-radius: 3px; border: 1px solid #bbdefb; font-size: 12px;">';
        html += (index + 1) + '. ' + escapeHtml(column);
        html += '</span>';
    });
    html += '</div>';
    
    content.innerHTML = html;
    
    document.getElementById('previewSection').style.display = 'block';
    document.getElementById('previewBtn').style.display = 'none';
    document.getElementById('importBtn').style.display = 'inline-block';
}

// Moderator Management Functions
document.getElementById('showModeratorForm').addEventListener('click', async function() {
    document.getElementById('moderatorForm').style.display = 'block';
    document.getElementById('moderatorsList').style.display = 'none';
    await loadDirectoryRows();
});

document.getElementById('cancelModerator').addEventListener('click', function() {
    document.getElementById('moderatorForm').style.display = 'none';
    clearModeratorForm();
});

// Row access type handlers
document.querySelectorAll('input[name="rowAccessType"]').forEach(radio => {
    radio.addEventListener('change', function() {
        const specificSection = document.getElementById('specificRowsSection');
        if (this.value === 'specific') {
            specificSection.style.display = 'block';
        } else {
            specificSection.style.display = 'none';
        }
    });
});

document.getElementById('viewModerators').addEventListener('click', async function() {
    document.getElementById('moderatorForm').style.display = 'none';
    document.getElementById('moderatorsList').style.display = 'block';
    await loadModerators();
});

document.getElementById('appointModerator').addEventListener('click', async function() {
    const email = document.getElementById('moderatorEmail').value.trim();
    const username = document.getElementById('moderatorUsername').value.trim();
    const authProvider = document.getElementById('authProvider').value;
    const canEdit = document.getElementById('canEdit').checked;
    const canApprove = document.getElementById('canApprove').checked;
    const requiresApproval = document.getElementById('requiresApproval').checked;
    const rowAccessType = document.querySelector('input[name="rowAccessType"]:checked').value;
    
    if (!email || !username) {
        alert('Please fill in email and username');
        return;
    }
    
    let rowFilter = [];
    if (rowAccessType === 'specific') {
        // Get selected rows from checkboxes
        const selectedCheckboxes = document.querySelectorAll('#rowSelectionList input[type="checkbox"]:checked');
        rowFilter = Array.from(selectedCheckboxes).map(cb => parseInt(cb.value));
        
        if (rowFilter.length === 0) {
            alert('Please select at least one row or choose "All Rows"');
            return;
        }
    }
    
    const data = {
        user_email: email,
        username: username,
        auth_provider: authProvider,
        directory_id: directoryId,
        can_edit: canEdit,
        can_approve: canApprove,
        requires_approval: requiresApproval,
        row_filter: rowFilter
    };
    
    try {
        const response = await fetch('/api/moderators/appoint?dir=' + directoryId, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfToken
            },
            credentials: 'same-origin',
            body: JSON.stringify(data)
        });
        
        if (response.ok) {
            alert('Moderator appointed successfully!');
            clearModeratorForm();
            document.getElementById('moderatorForm').style.display = 'none';
        } else {
            const error = await response.text();
            alert('Failed to appoint moderator: ' + error);
        }
    } catch (error) {
        console.error('Error appointing moderator:', error);
        alert('Network error occurred');
    }
});

async function loadDirectoryRows() {
    try {
        const response = await fetch('/api/directory?dir=' + directoryId, {
            credentials: 'same-origin'
        });
        
        if (response.ok) {
            const directory = await response.json();
            const rowSelectionList = document.getElementById('rowSelectionList');
            
            if (directory && directory.length > 0) {
                rowSelectionList.innerHTML = '';
                
                directory.forEach(row => {
                    // Parse the row data to show a preview
                    let rowDataPreview = '';
                    try {
                        const data = JSON.parse(row.data);
                        // Show first few fields as preview
                        rowDataPreview = Object.values(data).slice(0, 3).join(' | ');
                        if (rowDataPreview.length > 50) {
                            rowDataPreview = rowDataPreview.substring(0, 50) + '...';
                        }
                    } catch (e) {
                        rowDataPreview = 'Row ' + row.id;
                    }
                    
                    const checkboxDiv = document.createElement('div');
                    checkboxDiv.style.marginBottom = '5px';
                    checkboxDiv.innerHTML = 
                        '<label style="display: flex; align-items: center; padding: 5px; cursor: pointer;">' +
                            '<input type="checkbox" value="' + row.id + '" style="margin-right: 8px;">' +
                            '<span style="font-weight: bold; margin-right: 8px;">Row ' + row.id + ':</span>' +
                            '<span style="color: #666; font-size: 0.9em;">' + rowDataPreview + '</span>' +
                        '</label>';
                    rowSelectionList.appendChild(checkboxDiv);
                });
            } else {
                rowSelectionList.innerHTML = '<div style="color: #666; font-style: italic;">No rows found in directory</div>';
            }
        } else {
            document.getElementById('rowSelectionList').innerHTML = '<div style="color: #dc3545;">Failed to load directory rows</div>';
        }
    } catch (error) {
        console.error('Error loading directory rows:', error);
        document.getElementById('rowSelectionList').innerHTML = '<div style="color: #dc3545;">Error loading rows</div>';
    }
}

async function loadModerators() {
    try {
        const response = await fetch('/api/moderators?dir=' + directoryId, {
            credentials: 'same-origin'
        });
        
        if (response.ok) {
            const moderators = await response.json();
            displayModerators(moderators);
        } else {
            document.getElementById('moderatorsContent').innerHTML = '<p style="color: red;">Failed to load moderators</p>';
        }
    } catch (error) {
        console.error('Error loading moderators:', error);
        document.getElementById('moderatorsContent').innerHTML = '<p style="color: red;">Network error</p>';
    }
}

function displayModerators(moderators) {
    const content = document.getElementById('moderatorsContent');
    
    if (moderators.length === 0) {
        content.innerHTML = '<p>No moderators assigned to this directory yet.</p>';
        return;
    }
    
    let html = '<div style="display: grid; gap: 15px;">';
    moderators.forEach(mod => {
        html += '<div style="background: white; padding: 15px; border: 1px solid #ddd; border-radius: 5px;">';
        html += '<div style="font-weight: bold; margin-bottom: 5px;">' + escapeHtml(mod.username) + '</div>';
        html += '<div style="color: #666; font-size: 14px; margin-bottom: 5px;">' + escapeHtml(mod.user_email) + '</div>';
        html += '<div style="margin-bottom: 5px;"><span style="background: #e3f2fd; padding: 2px 6px; border-radius: 3px; font-size: 12px;">' + mod.auth_provider + '</span></div>';
        html += '<div style="font-size: 12px; color: #888;">Appointed by: ' + escapeHtml(mod.appointed_by) + ' (' + mod.appointed_by_type + ')</div>';
        html += '<div style="font-size: 12px; color: #888;">Created: ' + new Date(mod.created_at).toLocaleDateString() + '</div>';
        html += '<div style="margin-top: 10px;">';
        html += '<button onclick="removeModerator(\'' + mod.user_email + '\')" style="background: #dc3545; color: white; border: none; padding: 5px 10px; border-radius: 3px; cursor: pointer; font-size: 12px;">Remove</button>';
        html += '</div>';
        html += '</div>';
    });
    html += '</div>';
    
    content.innerHTML = html;
}

async function removeModerator(email) {
    if (!confirm('Are you sure you want to remove this moderator?')) {
        return;
    }
    
    try {
        const response = await fetch('/api/moderators/remove?dir=' + directoryId, {
            method: 'DELETE',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfToken
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                moderator_email: email,
                directory_id: directoryId
            })
        });
        
        if (response.ok) {
            alert('Moderator removed successfully!');
            await loadModerators();
        } else {
            const error = await response.text();
            alert('Failed to remove moderator: ' + error);
        }
    } catch (error) {
        console.error('Error removing moderator:', error);
        alert('Network error occurred');
    }
}

function clearModeratorForm() {
    document.getElementById('moderatorEmail').value = '';
    document.getElementById('moderatorUsername').value = '';
    document.getElementById('authProvider').value = 'google';
    document.getElementById('canEdit').checked = true;
    document.getElementById('canApprove').checked = false;
    document.getElementById('requiresApproval').checked = true;
    
    // Reset row access controls
    document.querySelector('input[name="rowAccessType"][value="all"]').checked = true;
    document.getElementById('specificRowsSection').style.display = 'none';
    
    // Uncheck all row checkboxes
    const checkboxes = document.querySelectorAll('#rowSelectionList input[type="checkbox"]');
    checkboxes.forEach(cb => cb.checked = false);
}

// Utility Functions
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}