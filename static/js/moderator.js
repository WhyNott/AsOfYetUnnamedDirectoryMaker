let csrfToken = '';
let directoryId = '';
let userEmail = '';

window.onload = function() {
    // Get config from global scope or fallback to meta tags
    if (window.moderatorConfig) {
        csrfToken = window.moderatorConfig.csrfToken;
        directoryId = window.moderatorConfig.directoryId;
        userEmail = window.moderatorConfig.userEmail;
    }
    
    // Set up event listeners
    document.getElementById('loadChanges').addEventListener('click', async function() {
        this.style.display = 'none';
        document.getElementById('refreshChanges').style.display = 'inline-block';
        await loadPendingChanges();
    });
    
    document.getElementById('refreshChanges').addEventListener('click', async function() {
        await loadPendingChanges();
    });
    
    document.getElementById('loadHierarchy').addEventListener('click', async function() {
        await loadHierarchy();
    });
};

async function loadPendingChanges() {
    const section = document.getElementById('pendingChangesSection');
    section.innerHTML = '<p>Loading...</p>';
    
    try {
        const response = await fetch('/api/changes/pending?dir=' + directoryId, {
            credentials: 'same-origin'
        });
        
        if (response.ok) {
            const changes = await response.json();
            displayPendingChanges(changes);
        } else {
            section.innerHTML = '<p style="color: red;">Failed to load pending changes</p>';
        }
    } catch (error) {
        console.error('Error loading pending changes:', error);
        section.innerHTML = '<p style="color: red;">Network error occurred</p>';
    }
}

function displayPendingChanges(changes) {
    const section = document.getElementById('pendingChangesSection');
    
    if (changes.length === 0) {
        section.innerHTML = '<p style="color: #666;">No pending changes require your approval at this time.</p>';
        return;
    }
    
    // Separate valid and invalid changes
    const validChanges = changes.filter(change => change.status === 'pending');
    const invalidChanges = changes.filter(change => change.status === 'invalid');
    
    let html = '';
    
    // Display invalid changes first if any
    if (invalidChanges.length > 0) {
        html += '<div class="invalid-changes-section">';
        html += '<h3 style="color: #d32f2f;">Invalid Changes (Schema Changed)</h3>';
        html += '<p style="color: #666; font-size: 0.9em;">These changes are no longer valid because the sheet structure has changed since they were submitted.</p>';
        
        invalidChanges.forEach(change => {
            html += '<div class="pending-change invalid-change">';
            html += '<div class="change-meta" style="background-color: #ffebee;">';
            html += 'Change ID: ' + change.id + ' • ';
            html += 'Type: ' + change.change_type + ' • ';
            html += 'Submitted by: ' + escapeHtml(change.submitted_by) + ' • ';
            html += 'Date: ' + new Date(change.created_at).toLocaleDateString();
            html += '</div>';
            
            html += '<div class="change-content">';
            html += '<strong>Row ID:</strong> ' + change.row_id + '<br>';
            html += '<strong>Column:</strong> ' + escapeHtml(change.column_name) + '<br>';
            if (change.old_value) {
                html += '<strong>Old Value:</strong> ' + escapeHtml(change.old_value) + '<br>';
            }
            html += '<strong>New Value:</strong> ' + escapeHtml(change.new_value) + '<br>';
            html += '<strong style="color: #d32f2f;">Reason Invalid:</strong> ' + escapeHtml(change.invalid_reason || 'Schema changed');
            html += '</div>';
            
            html += '<div class="change-actions">';
            html += '<button onclick="dismissInvalidChange(' + change.id + ')" class="button button-secondary">Dismiss</button>';
            html += '</div>';
            
            html += '</div>';
        });
        html += '</div><br>';
    }
    
    // Display valid pending changes
    if (validChanges.length > 0) {
        html += '<div class="valid-changes-section">';
        html += '<h3>Pending Changes Awaiting Approval</h3>';
        
        validChanges.forEach(change => {
            html += '<div class="pending-change">';
            html += '<div class="change-meta">';
            html += 'Change ID: ' + change.id + ' • ';
            html += 'Type: ' + change.change_type + ' • ';
            html += 'Submitted by: ' + escapeHtml(change.submitted_by) + ' • ';
            html += 'Date: ' + new Date(change.created_at).toLocaleDateString();
            html += '</div>';
            
            html += '<div class="change-content">';
            html += '<strong>Row ID:</strong> ' + change.row_id + '<br>';
            html += '<strong>Column:</strong> ' + escapeHtml(change.column_name) + '<br>';
            if (change.old_value) {
                html += '<strong>Old Value:</strong> ' + escapeHtml(change.old_value) + '<br>';
            }
            html += '<strong>New Value:</strong> ' + escapeHtml(change.new_value);
            html += '</div>';
            
            html += '<div class="change-actions">';
            html += '<input type="text" id="reason_' + change.id + '" class="reason-input" placeholder="Reason (optional)">';
            html += '<button onclick="approveChange(' + change.id + ', \'approve\')" class="button button-success">Approve</button>';
            html += '<button onclick="approveChange(' + change.id + ', \'reject\')" class="button button-danger">Reject</button>';
            html += '</div>';
            
            html += '</div>';
        });
        html += '</div>';
    } else if (invalidChanges.length === 0) {
        html += '<p style="color: #666;">No pending changes require your approval at this time.</p>';
    }
    
    section.innerHTML = html;
}

async function approveChange(changeId, action) {
    const reasonInput = document.getElementById('reason_' + changeId);
    const reason = reasonInput ? reasonInput.value.trim() : '';
    
    if (action === 'reject' && !reason) {
        alert('Please provide a reason for rejecting this change');
        return;
    }
    
    try {
        const response = await fetch('/api/changes/approve?dir=' + directoryId, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfToken
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                change_id: changeId,
                action: action,
                reason: reason
            })
        });
        
        if (response.ok) {
            alert('Change ' + action + 'd successfully!');
            await loadPendingChanges(); // Reload the changes
        } else {
            const error = await response.text();
            alert('Failed to ' + action + ' change: ' + error);
        }
    } catch (error) {
        console.error('Error processing change:', error);
        alert('Network error occurred');
    }
}

async function dismissInvalidChange(changeId) {
    if (!confirm('Are you sure you want to dismiss this invalid change? This action cannot be undone.')) {
        return;
    }
    
    try {
        const response = await fetch('/api/changes/dismiss?dir=' + directoryId, {
            method: 'DELETE',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfToken
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                change_id: changeId
            })
        });
        
        if (response.ok) {
            alert('Invalid change dismissed successfully!');
            await loadPendingChanges(); // Reload the changes
        } else {
            const error = await response.text();
            alert('Failed to dismiss change: ' + error);
        }
    } catch (error) {
        console.error('Error dismissing change:', error);
        alert('Network error occurred');
    }
}

async function loadHierarchy() {
    const section = document.getElementById('hierarchySection');
    section.innerHTML = '<p>Loading...</p>';
    
    try {
        const response = await fetch('/api/moderators/hierarchy?dir=' + directoryId + '&moderator_email=' + encodeURIComponent(userEmail), {
            credentials: 'same-origin'
        });
        
        if (response.ok) {
            const hierarchy = await response.json();
            displayHierarchy(hierarchy);
        } else {
            section.innerHTML = '<p style="color: red;">Failed to load hierarchy</p>';
        }
    } catch (error) {
        console.error('Error loading hierarchy:', error);
        section.innerHTML = '<p style="color: red;">Network error occurred</p>';
    }
}

function displayHierarchy(hierarchy) {
    const section = document.getElementById('hierarchySection');
    
    if (hierarchy.length === 0) {
        section.innerHTML = '<p style="color: #666;">You have not appointed any moderators yet.</p>';
        return;
    }
    
    let html = '<div style="display: grid; gap: 10px;">';
    hierarchy.forEach(h => {
        html += '<div style="background: white; padding: 10px; border-radius: 5px; border: 1px solid #ddd;">';
        html += '<strong>' + escapeHtml(h.child_moderator_email) + '</strong><br>';
        html += '<small style="color: #666;">Appointed on: ' + new Date(h.created_at).toLocaleDateString() + '</small>';
        html += '</div>';
    });
    html += '</div>';
    
    section.innerHTML = html;
}

function escapeHtml(text) {
    if (typeof text !== 'string') return text;
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}