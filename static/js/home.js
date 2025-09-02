let db;
let directoryData = [];
let columnNames = [];
let columnTypes = [];
let currentRow = -1;
let currentCol = -1;
let maxColumns = 0;
let csrfToken = null;
let deleteRowIndex = -1;
let currentDirectoryID = 'default';
let formControlGenerator = null;
let filteredData = [];

// Get directory ID from URL parameters
function getCurrentDirectoryID() {
    const urlParams = new URLSearchParams(window.location.search);
    return urlParams.get('dir') || 'default';
}

// Build URL with directory parameter
function buildAPIURL(path) {
    const url = new URL(path, window.location.origin);
    if (currentDirectoryID !== 'default') {
        url.searchParams.set('dir', currentDirectoryID);
    }
    return url.toString();
}

// Get CSRF token from meta tag if available
function getCSRFToken() {
    const metaTag = document.querySelector('meta[name="csrf-token"]');
    if (metaTag) {
        return metaTag.getAttribute('content');
    }
    return null;
}

// Initialize CSRF token and directory ID
csrfToken = getCSRFToken();
currentDirectoryID = getCurrentDirectoryID();

// Initialize SQL.js and load the database
initSqlJs({
    locateFile: file => `https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.8.0/${file}`
}).then(function(SQL){
    loadUserDirectories();
    loadDirectory();
});

async function loadDirectory() {
    try {
        // Load column metadata first
        await loadColumnMetadata();
        
        const response = await fetch(buildAPIURL('/download/directory.db'), {
            method: 'GET',
            credentials: 'same-origin'
        });
        if (!response.ok) {
            if (response.status === 401 || response.status === 403) {
                document.getElementById('loading').textContent = 'Access denied. Please login to view the directory.';
            } else if (response.status === 404) {
                document.getElementById('loading').textContent = 'Directory not found. Please check the URL or contact the administrator.';
            } else {
                document.getElementById('loading').textContent = 'No directory data available yet. Please check with the administrator.';
            }
            return;
        }
        
        const arrayBuffer = await response.arrayBuffer();
        const uInt8Array = new Uint8Array(arrayBuffer);
        
        const SQL = await initSqlJs({
            locateFile: file => `https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.8.0/${file}`
        });
        
        db = new SQL.Database(uInt8Array);
        
        loadDirectoryData();
        
    } catch (error) {
        console.error('Error loading database:', error);
        const errorMessage = error.message || 'Unknown error';
        document.getElementById('loading').textContent = `Error loading directory data: ${errorMessage}`;
    }
}

async function loadColumnMetadata() {
    try {
        // Load column metadata from the _meta_directory_column_types table
        const response = await fetch(buildAPIURL('/download/directory.db'), {
            method: 'GET',
            credentials: 'same-origin'
        });
        if (!response.ok) {
            columnNames = [];
            columnTypes = [];
            return;
        }
        
        const arrayBuffer = await response.arrayBuffer();
        const uInt8Array = new Uint8Array(arrayBuffer);
        
        const SQL = await initSqlJs({
            locateFile: file => `https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.8.0/${file}`
        });
        
        const metaDb = new SQL.Database(uInt8Array);
        
        // Get column metadata for the current directory
        const tableName = getCurrentDirectoryID();
        const stmt = metaDb.prepare(`SELECT columnName, columnType FROM _meta_directory_column_types WHERE columnTable = ? ORDER BY columnName`);
        stmt.bind([tableName]);
        
        const columns = [];
        const types = [];
        
        while (stmt.step()) {
            const row = stmt.getAsObject();
            columns.push(row.columnName);
            types.push(row.columnType);
        }
        
        stmt.free();
        metaDb.close();
        
        columnNames = columns;
        columnTypes = types;
        
    } catch (error) {
        console.error('Error loading column metadata:', error);
        columnNames = [];
        columnTypes = [];
    }
}

function loadDirectoryData() {
    try {
        const tableName = getCurrentDirectoryID();
        
        // Build dynamic SELECT statement using column names
        let selectColumns;
        if (columnNames.length > 0) {
            selectColumns = columnNames.map(col => `\`${col}\``).join(', ');
        } else {
            selectColumns = '*';
        }
        
        const stmt = db.prepare(`SELECT ${selectColumns} FROM \`${tableName}\``);
        const rows = [];
        
        while (stmt.step()) {
            const row = stmt.getAsObject();
            // Convert row object to array format for compatibility with existing rendering code
            const dataArray = columnNames.length > 0 
                ? columnNames.map(col => row[col] || '')
                : Object.values(row);
            
            rows.push({
                id: rows.length, // Use array index as ID since we don't have a dedicated ID column
                data: dataArray
            });
        }
        
        stmt.free();
        
        directoryData = rows;
        filteredData = [...rows]; // Initialize filtered data
        
        // Generate filtering controls if we have column types
        generateFilteringControls();
        
        renderTable();
        
        document.getElementById('loading').style.display = 'none';
        document.getElementById('directoryTable').style.display = 'table';
        
        updateRecordCount();
        
    } catch (error) {
        console.error('Error processing directory data:', error);
        document.getElementById('loading').textContent = 'Error processing directory data.';
    }
}

function renderTable() {
    renderFilteredTable(filteredData.length > 0 ? filteredData : directoryData);
}

function renderFilteredTable(dataToRender) {
    if (!dataToRender || dataToRender.length === 0) {
        document.getElementById('loading').textContent = 'No directory entries found.';
        return;
    }
    
    // Determine the maximum number of columns
    maxColumns = columnNames.length > 0 ? columnNames.length : 0;
    if (maxColumns === 0) {
        dataToRender.forEach(row => {
            if (row.data.length > maxColumns) {
                maxColumns = row.data.length;
            }
        });
    }
    
    // Create header
    const headerRow = document.getElementById('tableHeader');
    headerRow.innerHTML = '';
    const headerRowElement = document.createElement('tr');
    
    for (let i = 0; i < maxColumns; i++) {
        const th = document.createElement('th');
        th.textContent = columnNames[i] || `Column ${i + 1}`;
        headerRowElement.appendChild(th);
    }
    
   /* // Add delete column header
    const deleteHeader = document.createElement('th');
    deleteHeader.textContent = 'Actions';
    deleteHeader.className = 'delete-cell';
    headerRowElement.appendChild(deleteHeader);*/
    
    headerRow.appendChild(headerRowElement);
    
    // Create body
    const tbody = document.getElementById('tableBody');
    tbody.innerHTML = '';
    
    dataToRender.forEach((entry, rowIndex) => {
        const tr = document.createElement('tr');
        
        for (let colIndex = 0; colIndex < maxColumns; colIndex++) {
            const td = document.createElement('td');
            const cellValue = entry.data[colIndex] || '';
            td.textContent = cellValue;
            td.dataset.row = rowIndex;
            td.dataset.col = colIndex;
            
            // Add click handler for corrections
            td.addEventListener('click', function() {
                openCorrectionModal(rowIndex, colIndex, cellValue);
            });
            
            tr.appendChild(td);
        }
        
      /*  // Add delete button cell
        const deleteCell = document.createElement('td');
        deleteCell.className = 'delete-cell';
        
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'delete-btn';
        deleteBtn.innerHTML = '<span class="delete-icon">🗑️</span>';
        deleteBtn.title = 'Delete this row';
        deleteBtn.addEventListener('click', function(e) {
            e.stopPropagation();
            openDeleteModal(rowIndex, entry.data);
        });
        
        deleteCell.appendChild(deleteBtn);
        tr.appendChild(deleteCell);*/
        
        tbody.appendChild(tr);
    });
}

function updateRecordCount() {
    const displayCount = filteredData.length;
    const totalCount = directoryData.length;
    
    let countText = `${displayCount} records`;
    if (displayCount !== totalCount) {
        countText += ` (filtered from ${totalCount} total)`;
    }
    
    document.getElementById('recordCount').textContent = countText;
}

function openCorrectionModal(row, col, currentValue) {
    currentRow = row;
    currentCol = col;
    
    document.getElementById('currentValue').textContent = currentValue;
    document.getElementById('newValue').value = currentValue;
    document.getElementById('correctionModal').style.display = 'block';
    
    // Focus on input field
    setTimeout(() => {
        document.getElementById('newValue').focus();
        document.getElementById('newValue').select();
    }, 100);
}


// Modal functionality
document.querySelector('.close').addEventListener('click', function() {
    document.getElementById('correctionModal').style.display = 'none';
});

document.getElementById('cancelCorrection').addEventListener('click', function() {
    document.getElementById('correctionModal').style.display = 'none';
});

document.getElementById('submitCorrection').addEventListener('click', async function() {
    const newValue = document.getElementById('newValue').value;
    
    if (currentRow === -1 || currentCol === -1) {
        showErrorMessage('Error: Invalid cell selection');
        return;
    }
    
    // Validate input length
    if (newValue.length > 1000) {
        showErrorMessage('Value too long. Maximum 1000 characters allowed.');
        return;
    }
    
    try {
        const headers = {
            'Content-Type': 'application/json',
        };
        
        // Add CSRF token if available
        if (csrfToken) {
            headers['X-CSRF-Token'] = csrfToken;
        }
        
        const response = await fetch(buildAPIURL('/api/corrections'), {
            method: 'POST',
            headers: headers,
            credentials: 'same-origin',
            body: JSON.stringify({
                row: currentRow,
                column: currentCol,
                value: newValue
            })
        });
        
        if (response.ok) {
            showSuccessMessage('Correction submitted successfully! The directory will be updated shortly.');
            document.getElementById('correctionModal').style.display = 'none';
            
            // Refresh the page after a short delay to get the updated data
            setTimeout(() => {
                window.location.reload();
            }, 2000);
        } else {
            const errorText = await response.text();
            let errorMessage = 'Error submitting correction';
            
            if (response.status === 400) {
                errorMessage = 'Invalid input: ' + errorText;
            } else if (response.status === 401) {
                errorMessage = 'Please login to make corrections';
            } else if (response.status === 403) {
                errorMessage = 'Access denied. CSRF token may be invalid.';
            } else if (response.status >= 500) {
                errorMessage = 'Server error. Please try again later.';
            } else {
                errorMessage += ': ' + errorText;
            }
            
            showErrorMessage(errorMessage);
        }
    } catch (error) {
        console.error('Error submitting correction:', error);
        showErrorMessage('Network error submitting correction. Please check your connection and try again.');
    }
});

// Close modal when clicking outside of it
window.addEventListener('click', function(event) {
    const modal = document.getElementById('correctionModal');
    if (event.target === modal) {
        modal.style.display = 'none';
    }
});

// Handle Enter key in the correction input
document.getElementById('newValue').addEventListener('keypress', function(e) {
    if (e.key === 'Enter') {
        document.getElementById('submitCorrection').click();
    }
});

// Add Row functionality
document.getElementById('addRowBtn').addEventListener('click', function() {
    openAddRowModal();
});

function openAddRowModal() {
    const inputsContainer = document.getElementById('addRowInputs');
    inputsContainer.innerHTML = '';
    
    // Create input fields for each column
    for (let i = 0; i < Math.max(maxColumns, 3); i++) {
        const div = document.createElement('div');
        div.style.marginBottom = '10px';
        
        const label = document.createElement('label');
        label.textContent = `${columnNames[i] || `Column ${i + 1}`}:`;
        label.style.display = 'block';
        label.style.marginBottom = '5px';
        
        const input = document.createElement('input');
        input.type = 'text';
        input.id = `newRowCol${i}`;
        input.style.width = '100%';
        input.style.padding = '8px';
        input.style.border = '1px solid #ddd';
        input.style.borderRadius = '4px';
        
        div.appendChild(label);
        div.appendChild(input);
        inputsContainer.appendChild(div);
    }
    
    document.getElementById('addRowModal').style.display = 'block';
    
    // Focus on first input
    setTimeout(() => {
        document.getElementById('newRowCol0').focus();
    }, 100);
}

// Add Row Modal close handlers
document.getElementById('closeAddRow').addEventListener('click', function() {
    document.getElementById('addRowModal').style.display = 'none';
});

document.getElementById('cancelNewRow').addEventListener('click', function() {
    document.getElementById('addRowModal').style.display = 'none';
});

// Submit new row
document.getElementById('submitNewRow').addEventListener('click', async function() {
    const rowData = [];
    const numColumns = Math.max(maxColumns, 3);
    
    for (let i = 0; i < numColumns; i++) {
        const input = document.getElementById(`newRowCol${i}`);
        rowData.push(input ? input.value : '');
    }
    
    // Remove trailing empty strings
    while (rowData.length > 0 && rowData[rowData.length - 1] === '') {
        rowData.pop();
    }
    
    if (rowData.length === 0 || rowData.every(val => val === '')) {
        alert('Please enter at least one value');
        return;
    }
    
    try {
        const headers = {
            'Content-Type': 'application/json',
        };
        
        // Add CSRF token if available
        if (csrfToken) {
            headers['X-CSRF-Token'] = csrfToken;
        }
        
        const response = await fetch(buildAPIURL('/api/add-row'), {
            method: 'POST',
            headers: headers,
            credentials: 'same-origin',
            body: JSON.stringify({
                data: rowData
            })
        });
        
        if (response.ok) {
            showSuccessMessage('Row added successfully! The directory will be updated shortly.');
            document.getElementById('addRowModal').style.display = 'none';
            
            // Refresh the page after a short delay to get the updated data
            setTimeout(() => {
                window.location.reload();
            }, 2000);
        } else {
            const errorText = await response.text();
            let errorMessage = 'Error adding row';
            
            if (response.status === 400) {
                errorMessage = 'Invalid input: ' + errorText;
            } else if (response.status === 401) {
                errorMessage = 'Please login to add rows';
            } else if (response.status === 403) {
                errorMessage = 'Access denied. CSRF token may be invalid.';
            } else if (response.status >= 500) {
                errorMessage = 'Server error. Please try again later.';
            } else {
                errorMessage += ': ' + errorText;
            }
            
            showErrorMessage(errorMessage);
        }
    } catch (error) {
        console.error('Error adding row:', error);
        showErrorMessage('Network error adding row. Please check your connection and try again.');
    }
});

// Close add row modal when clicking outside
window.addEventListener('click', function(event) {
    const addRowModal = document.getElementById('addRowModal');
    if (event.target === addRowModal) {
        addRowModal.style.display = 'none';
    }
});

// Utility functions for user feedback
function showErrorMessage(message) {
    // Try using a toast notification if available, otherwise use alert
    if (window.createToast) {
        window.createToast(message, 'error');
    } else {
        alert(message);
    }
}

function showSuccessMessage(message) {
    // Try using a toast notification if available, otherwise use alert
    if (window.createToast) {
        window.createToast(message, 'success');
    } else {
        alert(message);
    }
}

// Input validation helpers
function validateCellValue(value) {
    if (typeof value !== 'string') {
        return false;
    }
    if (value.length > 1000) {
        return false;
    }
    return true;
}

// Debounce function for search
function debounce(func, wait) {
    let timeout;
    return function executedFunction(...args) {
        const later = () => {
            clearTimeout(timeout);
            func(...args);
        };
        clearTimeout(timeout);
        timeout = setTimeout(later, wait);
    };
}

// Generate filtering controls based on column types
function generateFilteringControls() {
    const filterContainer = document.getElementById('filterControls');
    if (!filterContainer || columnNames.length === 0) {
        return;
    }
    
    // Initialize form control generator if not already done
    if (!formControlGenerator) {
        formControlGenerator = new FormControlGenerator('filterControls', 'basicFilterFields');
    }
    
    // Create field pairs from column names and types
    const fieldPairs = columnNames.map((name, index) => {
        const type = columnTypes[index] || 'basic';
        return [name, type];
    });
    
    formControlGenerator.generateControls(fieldPairs);
    
    // Add apply filters button
    const applyButton = document.createElement('button');
    applyButton.textContent = 'Apply Filters';
    applyButton.className = 'btn btn-primary';
    applyButton.addEventListener('click', applyFilters);
    
    const clearButton = document.createElement('button');
    clearButton.textContent = 'Clear Filters';
    clearButton.className = 'btn btn-secondary';
    clearButton.addEventListener('click', clearFilters);
    
    const buttonGroup = document.createElement('div');
    buttonGroup.className = 'filter-button-group';
    buttonGroup.appendChild(applyButton);
    buttonGroup.appendChild(clearButton);
    
    filterContainer.appendChild(buttonGroup);
}

// Apply filters based on control values
function applyFilters() {
    if (!formControlGenerator) {
        return;
    }
    
    const filterValues = formControlGenerator.getAllValues();
    filteredData = directoryData.filter(row => {
        return matchesFilters(row, filterValues);
    });
    
    renderTable();
}

// Clear all filters
function clearFilters() {
    if (formControlGenerator) {
        formControlGenerator.clear();
        generateFilteringControls();
    }
    
    filteredData = [...directoryData];
    renderTable();
    
    // Clear search box as well
    const searchBox = document.getElementById('searchBox');
    if (searchBox) {
        searchBox.value = '';
    }
}

// Check if a row matches the current filters
function matchesFilters(row, filterValues) {
    for (const [columnName, filterData] of Object.entries(filterValues.controls)) {
        const columnIndex = columnNames.indexOf(columnName);
        if (columnIndex === -1) continue;
        
        const cellValue = row.data[columnIndex] || '';
        
        switch (filterData.type) {
            case 'tag':
            case 'category':
            case 'location':
                if (filterData.value && filterData.value.length > 0) {
                    const cellTags = cellValue.split(',').map(tag => tag.trim().toLowerCase());
                    const filterTags = filterData.value.map(tag => tag.toLowerCase());
                    const hasMatch = filterTags.some(filterTag => 
                        cellTags.some(cellTag => cellTag.includes(filterTag))
                    );
                    if (!hasMatch) return false;
                }
                break;
                
            case 'numeric':
                const numValue = parseFloat(cellValue);
                if (!isNaN(numValue) && filterData.value) {
                    if (filterData.value.min !== null && numValue < filterData.value.min) {
                        return false;
                    }
                    if (filterData.value.max !== null && numValue > filterData.value.max) {
                        return false;
                    }
                }
                break;
        }
    }
    
    return true;
}

// Update search to use debounced function and work with filtered data
// Search only applies to basic fields (non-control fields)
const debouncedSearch = debounce(function(searchTerm) {
    if (searchTerm.trim() === '') {
        // If no search term, show all filtered data
        renderFilteredTable(filteredData);
        return;
    }
    
    // Get basic field indices (fields that don't have special controls)
    const basicFieldIndices = [];
    if (formControlGenerator && formControlGenerator.basicFields) {
        formControlGenerator.basicFields.forEach(fieldName => {
            const index = columnNames.indexOf(fieldName);
            if (index !== -1) {
                basicFieldIndices.push(index);
            }
        });
    }
    
    // If no basic fields are defined, fall back to searching all fields
    if (basicFieldIndices.length === 0) {
        const searchFiltered = filteredData.filter(row => {
            return row.data.some(cellValue => {
                return cellValue && cellValue.toString().toLowerCase().includes(searchTerm);
            });
        });
        renderFilteredTable(searchFiltered);
        return;
    }
    
    // Filter the already filtered data by search term, but only in basic fields
    const searchFiltered = filteredData.filter(row => {
        return basicFieldIndices.some(columnIndex => {
            const cellValue = row.data[columnIndex];
            return cellValue && cellValue.toString().toLowerCase().includes(searchTerm);
        });
    });
    
    renderFilteredTable(searchFiltered);
}, 300);

document.getElementById('searchBox').addEventListener('input', function(e) {
    const searchTerm = e.target.value.toLowerCase();
    debouncedSearch(searchTerm);
});

// Delete row functionality
function openDeleteModal(rowIndex, rowData) {
    deleteRowIndex = rowIndex;
    
    // Show the row data in a readable format
    const displayData = rowData.filter(cell => cell && cell.trim() !== '').join(' | ');
    document.getElementById('deleteRowData').textContent = displayData || 'Empty row';
    
    // Clear the reason field
    document.getElementById('deleteReason').value = '';
    
    // Show the modal
    document.getElementById('deleteRowModal').style.display = 'block';
    
    // Focus on reason field
    setTimeout(() => {
        document.getElementById('deleteReason').focus();
    }, 100);
}

// Delete modal event handlers
document.getElementById('closeDeleteRow').addEventListener('click', function() {
    document.getElementById('deleteRowModal').style.display = 'none';
});

document.getElementById('cancelDelete').addEventListener('click', function() {
    document.getElementById('deleteRowModal').style.display = 'none';
});

document.getElementById('confirmDelete').addEventListener('click', async function() {
    if (deleteRowIndex === -1) {
        showErrorMessage('Error: No row selected for deletion');
        return;
    }
    
    const reason = document.getElementById('deleteReason').value || '';
    
    try {
        const headers = {
            'Content-Type': 'application/json',
        };
        
        // Add CSRF token if available
        if (csrfToken) {
            headers['X-CSRF-Token'] = csrfToken;
        }
        
        const response = await fetch(buildAPIURL('/api/delete-row'), {
            method: 'DELETE',
            headers: headers,
            credentials: 'same-origin',
            body: JSON.stringify({
                row: deleteRowIndex,
                reason: reason
            })
        });
        
        if (response.ok) {
            showSuccessMessage('Row deleted successfully! The directory will be updated shortly.');
            document.getElementById('deleteRowModal').style.display = 'none';
            
            // Refresh the page after a short delay to get the updated data
            setTimeout(() => {
                window.location.reload();
            }, 2000);
        } else {
            const errorText = await response.text();
            let errorMessage = 'Error deleting row';
            
            if (response.status === 400) {
                errorMessage = 'Invalid input: ' + errorText;
            } else if (response.status === 401) {
                errorMessage = 'Please login to delete rows';
            } else if (response.status === 403) {
                errorMessage = 'Access denied. CSRF token may be invalid.';
            } else if (response.status === 404) {
                errorMessage = 'Row not found or already deleted';
            } else if (response.status >= 500) {
                errorMessage = 'Server error. Please try again later.';
            } else {
                errorMessage += ': ' + errorText;
            }
            
            showErrorMessage(errorMessage);
        }
    } catch (error) {
        console.error('Error deleting row:', error);
        showErrorMessage('Network error deleting row. Please check your connection and try again.');
    }
});

// Close delete modal when clicking outside
window.addEventListener('click', function(event) {
    const deleteModal = document.getElementById('deleteRowModal');
    if (event.target === deleteModal) {
        deleteModal.style.display = 'none';
    }
});

// Handle Enter key in the delete reason input
document.getElementById('deleteReason').addEventListener('keypress', function(e) {
    if (e.key === 'Enter') {
        document.getElementById('confirmDelete').click();
    }
});

// Load user directories for the directory selector
async function loadUserDirectories() {
    const directorySelector = document.getElementById('directorySelector');
    if (!directorySelector) {
        // Not authenticated or selector not present
        return;
    }
    
    try {
        const response = await fetch('/api/user-directories', {
            method: 'GET',
            credentials: 'same-origin'
        });
        
        if (response.ok) {
            const directories = await response.json();
            
            // Clear existing options
            directorySelector.innerHTML = '';
            
            // Add options for each directory
            directories.forEach(function(directory) {
                const option = document.createElement('option');
                option.value = directory.id;
                option.textContent = directory.name + ' (' + directory.id + ')';
                
                // Select current directory
                if (directory.id === currentDirectoryID) {
                    option.selected = true;
                }
                
                directorySelector.appendChild(option);
            });
            
            // Only show selector if user has multiple directories
            if (directories.length > 1) {
                directorySelector.style.display = 'inline-block';
            } else {
                directorySelector.style.display = 'none';
            }
            
        } else {
            // Hide selector on error
            directorySelector.style.display = 'none';
        }
    } catch (error) {
        console.error('Error loading user directories:', error);
        directorySelector.style.display = 'none';
    }
}

// Handle directory selection change
document.addEventListener('DOMContentLoaded', function() {
    const directorySelector = document.getElementById('directorySelector');
    if (directorySelector) {
        directorySelector.addEventListener('change', function() {
            const selectedDirectoryID = this.value;
            if (selectedDirectoryID && selectedDirectoryID !== currentDirectoryID) {
                // Redirect to the selected directory
                const url = new URL(window.location);
                if (selectedDirectoryID === 'default') {
                    url.searchParams.delete('dir');
                } else {
                    url.searchParams.set('dir', selectedDirectoryID);
                }
                window.location.href = url.toString();
            }
        });
    }
});