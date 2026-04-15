# Enhanced Data Table Features

## Overview
All data tables in the application now support advanced features for better data analysis and viewing experience.

## Features Implemented

### 1. **Sorting**
- Click any column header to sort ascending
- Click again to sort descending
- Visual indicators (↑/↓) show current sort direction
- Available on:
  - Data Browser tables (database records)
  - Audit Log table
  - All DataTable component usages

### 2. **Column Visibility Toggle**
- Show/hide specific columns dynamically
- Accessible via "Columns" button in table toolbar
- Dropdown menu with checkboxes for each column
- All columns visible by default
- Available on:
  - Data Browser tables
  - Audit Log table

### 3. **Pagination Controls**
- Navigate through large datasets with Previous/Next buttons
- Current page indicator (e.g., "1 / 25")
- Row count display (e.g., "Rows 1–100 of 2,543")
- Available on:
  - Data Browser (all database tables)
  - Schema browser

### 4. **Page Size Selector**
- Customize number of rows per page
- Options: 25, 50, 100, 200, 500
- Resets to page 1 when changed
- Available on:
  - Data Browser tables
  - Audit Log (100, 200, 500, 1000)

## Usage Guide

### Data Browser
1. **Sort columns**: Click any column header
2. **Adjust rows per page**: Use dropdown next to pagination
3. **Toggle columns**: Click "Columns" button, check/uncheck columns to show/hide

### Audit Log
1. **Sort columns**: Click Time, User, Connection, SQL, Duration, Rows, or Status headers
2. **Filter entries**: Use search box to filter by SQL, user, or connection
3. **Limit results**: Select 100, 200, 500, or 1000 from dropdown
4. **Toggle columns**: Click "Columns" button to customize view

## Technical Implementation

### DataTable Component
Location: `/web/src/components/database/DataTable.vue`

**New Features:**
- `visibleColumns` reactive Set for column visibility state
- `filteredColumns` computed property for rendering
- `page-size-change` event emitter
- Column visibility dropdown menu with checkboxes
- Page size selector in pagination bar

**Props:**
- All existing props maintained
- No breaking changes

**Events:**
- `page-change(page: number)` - existing
- `page-size-change(size: number)` - **NEW**
- `sort(col: string, dir: 'asc'|'desc')` - existing

### AuditLogView
Location: `/web/src/views/AuditLogView.vue`

**New Features:**
- Column visibility state management
- Client-side sorting for all columns
- Sortable column headers with indicators
- Column visibility dropdown

**Sorting Logic:**
- Computed `sortedEntries` property
- Handles string, number comparisons
- Maintains original data immutability

## Keyboard Shortcuts
- **Enter** in Audit Log search: Refresh with filter applied

## Future Enhancements
Potential additions:
- Export visible columns only (CSV/JSON)
- Save column visibility preferences to localStorage
- Multi-column sorting
- Column resizing
- Quick filters (e.g., "Show only errors")
