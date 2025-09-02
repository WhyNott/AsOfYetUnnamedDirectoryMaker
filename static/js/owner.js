// owner Panel JavaScript
// Uses global window.ownerConfig set by template

if (!window.ownerConfig) {
    console.error('window.ownerConfig is not defined. Check that the template is loading correctly.');
    throw new Error('Configuration not found');
}

const { csrfToken, previewURL, importURL, ownerURL, directoryId } = window.ownerConfig;

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
    
    // Add column names and types from the preview section
    const previewContent = document.getElementById('previewContent');
    if (previewContent) {
        // Get all column type selects and hidden column name inputs
        const columnTypeSelects = previewContent.querySelectorAll('select[name^="column_type_"]');
        const columnNameInputs = previewContent.querySelectorAll('input[name^="column_name_"]');
        
        columnTypeSelects.forEach(select => {
            formData.append(select.name, select.value);
        });
        
        columnNameInputs.forEach(input => {
            formData.append(input.name, input.value);
        });
    }
    
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
            // If no redirect but successful, go to owner with success
            window.location.href = ownerURL + '&imported=true';
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
    
    let html = '<div class="sheet-info">';
    html += '<strong>Sheet Name:</strong> ' + escapeHtml(preview.sheet_name) + '<br>';
    html += '<strong>Data Rows:</strong> ' + preview.row_count + '<br>';
    html += '<strong>Columns (' + preview.columns.length + '):</strong>';
    html += '</div>';
    
    html += '<div class="column-config">';
    html += '<p><strong>Configure Column Types:</strong></p>';
    html += '<div class="column-list">';
    
    preview.columns.forEach((column, index) => {
        html += '<div class="column-item">';
        html += '<div class="column-name">' + (index + 1) + '. ' + escapeHtml(column) + '</div>';
        html += '<div class="column-type-selector">';
        html += '<label for="column_type_' + index + '">Type:</label>';
        html += '<select id="column_type_' + index + '" name="column_type_' + index + '">';
        html += '<option value="basic"' + (preview.column_types[index] === 'basic' ? ' selected' : '') + '>Basic</option>';
        html += '<option value="numeric"' + (preview.column_types[index] === 'numeric' ? ' selected' : '') + '>Numeric</option>';
        html += '<option value="location"' + (preview.column_types[index] === 'location' ? ' selected' : '') + '>Location</option>';
        html += '<option value="tag"' + (preview.column_types[index] === 'tag' ? ' selected' : '') + '>Tag</option>';
        html += '<option value="category"' + (preview.column_types[index] === 'category' ? ' selected' : '') + '>Category</option>';
        html += '</select>';
        html += '</div>';
        html += '<input type="hidden" name="column_name_' + index + '" value="' + escapeHtml(column) + '">';
        html += '</div>';
    });
    html += '</div>';
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

// Moderation Rules Functions
document.getElementById('showModerationRules').addEventListener('click', function() {
    document.getElementById('moderationRulesForm').style.display = 'block';
    document.getElementById('currentRules').style.display = 'none';
    loadColumnsForRules();
});

document.getElementById('viewCurrentRules').addEventListener('click', function() {
    document.getElementById('moderationRulesForm').style.display = 'none';
    document.getElementById('currentRules').style.display = 'block';
    loadCurrentRules();
});

document.getElementById('cancelModerationRule').addEventListener('click', function() {
    document.getElementById('moderationRulesForm').style.display = 'none';
    clearModerationRuleForm();
});

document.getElementById('filterType').addEventListener('change', function() {
    const filterType = this.value;
    const textOptions = document.getElementById('textFilterOptions');
    const numericOptions = document.getElementById('numericFilterOptions');
    
    if (filterType === 'numeric_range') {
        textOptions.style.display = 'none';
        numericOptions.style.display = 'block';
    } else {
        textOptions.style.display = 'block';
        numericOptions.style.display = 'none';
    }
});

document.getElementById('rangeType').addEventListener('change', function() {
    const rangeType = this.value;
    const thresholdInput = document.getElementById('thresholdInput');
    const rangeInputs = document.getElementById('rangeInputs');
    
    if (rangeType === 'between') {
        thresholdInput.style.display = 'none';
        rangeInputs.style.display = 'block';
    } else {
        thresholdInput.style.display = 'block';
        rangeInputs.style.display = 'none';
    }
});

document.getElementById('addModerationRule').addEventListener('click', addModerationRule);

async function loadColumnsForRules() {
    try {
        const response = await fetch('/api/columns?dir=' + directoryId, {
            credentials: 'same-origin'
        });
        
        if (response.ok) {
            const columns = await response.json();
            const select = document.getElementById('filterColumn');
            select.innerHTML = '<option value="">Select a column...</option>';
            
            columns.forEach((column, index) => {
                const option = document.createElement('option');
                option.value = column;
                option.textContent = column;
                select.appendChild(option);
            });
        }
    } catch (error) {
        console.error('Error loading columns:', error);
    }
}

async function addModerationRule() {
    const ruleName = document.getElementById('ruleName').value.trim();
    const filterColumn = document.getElementById('filterColumn').value;
    const filterType = document.getElementById('filterType').value;
    
    if (!ruleName || !filterColumn || !filterType) {
        alert('Please fill in all required fields');
        return;
    }
    
    // Build the filter configuration
    let filter;
    if (filterType === 'numeric_range') {
        const rangeType = document.getElementById('rangeType').value;
        if (rangeType === 'between') {
            const min = parseFloat(document.getElementById('minValue').value);
            const max = parseFloat(document.getElementById('maxValue').value);
            if (isNaN(min) || isNaN(max)) {
                alert('Please enter valid numeric values');
                return;
            }
            filter = {
                type: 'numeric_range',
                range: { type: 'between', min: min, max: max }
            };
        } else {
            const threshold = parseFloat(document.getElementById('threshold').value);
            if (isNaN(threshold)) {
                alert('Please enter a valid threshold value');
                return;
            }
            filter = {
                type: 'numeric_range',
                range: { type: rangeType, threshold: threshold }
            };
        }
    } else {
        const values = document.getElementById('filterValues').value
            .split(',')
            .map(v => v.trim())
            .filter(v => v);
        if (values.length === 0) {
            alert('Please enter at least one filter value');
            return;
        }
        filter = {
            type: filterType,
            values: values
        };
    }
    
    // Build the control
    const control = {
        column: { type: 'single', value: filterColumn },
        filter: filter
    };
    
    // For now, just alert with the configuration (in a real implementation, this would save to backend)
    alert('Moderation rule configuration:\n' + JSON.stringify(control, null, 2) + 
          '\n\nNote: Full implementation would save this configuration for use when appointing moderators.');
    
    clearModerationRuleForm();
    document.getElementById('moderationRulesForm').style.display = 'none';
}

function loadCurrentRules() {
    // Placeholder - in full implementation this would load from backend
    document.getElementById('rulesContent').innerHTML = 
        '<p style="color: #666; font-style: italic;">No moderation rules configured yet.</p>' +
        '<p style="font-size: 0.9em; color: #666;">' +
        'Rules will be applied when appointing moderators. Each moderator can be given specific filter-based permissions that determine which rows they can access and modify.' +
        '</p>';
}

function clearModerationRuleForm() {
    document.getElementById('ruleName').value = '';
    document.getElementById('filterColumn').value = '';
    document.getElementById('filterType').value = 'categories';
    document.getElementById('filterValues').value = '';
    document.getElementById('rangeType').value = 'above';
    document.getElementById('threshold').value = '';
    document.getElementById('minValue').value = '';
    document.getElementById('maxValue').value = '';
    document.getElementById('requireEdit').checked = true;
    document.getElementById('requireAdd').checked = true;
    document.getElementById('requireDelete').checked = true;
    
    // Reset display
    document.getElementById('textFilterOptions').style.display = 'block';
    document.getElementById('numericFilterOptions').style.display = 'none';
    document.getElementById('thresholdInput').style.display = 'block';
    document.getElementById('rangeInputs').style.display = 'none';
}

// Utility Functions
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}