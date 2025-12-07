# Variable Groups Implementation Plan

## Overview

Add support for multiple variable groups in workflow execution. Each group contains the same variable names but different values. Users can enable/disable groups per run, and the workflow executes sequentially for each enabled group.

## Requirements Summary

| # | Requirement | Details |
|---|-------------|---------|
| 1 | Variables Node | New React Flow node after Start, shows variables + group count |
| 2 | Sidebar Panel | Manage groups: view, edit, add, delete, enable/disable |
| 3 | Execution | Sequential for enabled groups only |
| 4 | Folder Structure | `iteration-X/group-Y/` for multi-group (nested), `iteration-X/` for single |
| 5 | Progress Display | "Running: group-1 • Step 3/10" in toolbar |
| 6 | Stop Behavior | Cancel all remaining groups |
| 7 | Group IDs | Simple: group-1, group-2, group-3... |

---

## Data Structures

### Backend (Go)

```go
// VariableGroup represents a single set of variable values
type VariableGroup struct {
    GroupID  string            `json:"group_id"`  // e.g., "group-1"
    Values   map[string]string `json:"values"`    // Variable name -> value
    Enabled  bool              `json:"enabled"`   // Whether to include in execution
}

// VariableGroupsManifest contains all variable groups
type VariableGroupsManifest struct {
    // Variable definitions (names + descriptions) - shared across all groups
    Variables []Variable `json:"variables"`
    
    // Template objective with {{VARIABLE}} placeholders
    Objective string `json:"objective"`
    
    // Array of variable groups (each with different values)
    Groups []VariableGroup `json:"groups"`
    
    // Extraction date
    ExtractionDate string `json:"extraction_date"`
}

// BatchExecutionProgress tracks execution across multiple groups
type BatchExecutionProgress struct {
    TotalGroups      int                         `json:"total_groups"`
    EnabledGroups    []string                    `json:"enabled_groups"`    // Group IDs to execute
    CompletedGroups  []string                    `json:"completed_groups"`  // Group IDs finished
    CurrentGroup     string                      `json:"current_group"`     // Currently executing
    GroupProgress    map[string]*StepProgress    `json:"group_progress"`    // Per-group step progress
    LastUpdated      time.Time                   `json:"last_updated"`
}
```

### Frontend (TypeScript)

```typescript
// Variable definition (shared across groups)
interface VariableDefinition {
  name: string;
  description: string;
}

// Single variable group
interface VariableGroup {
  group_id: string;           // "group-1", "group-2", etc.
  values: Record<string, string>;  // { "APP_NAME": "my-app", "REGION": "us-east-1" }
  enabled: boolean;
}

// Full manifest
interface VariableGroupsManifest {
  variables: VariableDefinition[];
  objective: string;
  groups: VariableGroup[];
  extraction_date: string;
}

// Batch progress
interface BatchExecutionProgress {
  total_groups: number;
  enabled_groups: string[];
  completed_groups: string[];
  current_group: string;
  group_progress: Record<string, StepProgress>;
}
```

---

## File Structure

### Workspace Layout

```
workspace/
├── variables/
│   └── variables.json          # Now includes groups array
├── planning/
│   └── plan.json
└── runs/
    ├── iteration-1/            # Single group (backward compatible)
    │   ├── execution/
    │   └── steps_done.json
    ├── iteration-2/             # Multi-group: base iteration folder
    │   ├── group-1/            # First group (nested)
    │   │   ├── execution/
    │   │   └── steps_done.json
    │   └── group-3/            # Third group (group-2 was disabled)
    │       ├── execution/
    │       └── steps_done.json
    └── batch_progress.json     # Batch execution tracking (optional)
```

### Updated variables.json Format

**Single Group (backward compatible):**
```json
{
  "objective": "Deploy {{APP_NAME}} to {{REGION}}",
  "variables": [
    { "name": "APP_NAME", "value": "my-app", "description": "Application name" },
    { "name": "REGION", "value": "us-east-1", "description": "AWS region" }
  ],
  "extraction_date": "2025-01-15"
}
```

**Multiple Groups:**
```json
{
  "objective": "Deploy {{APP_NAME}} to {{REGION}}",
  "variables": [
    { "name": "APP_NAME", "description": "Application name" },
    { "name": "REGION", "description": "AWS region" }
  ],
  "groups": [
    {
      "group_id": "group-1",
      "values": { "APP_NAME": "my-app", "REGION": "us-east-1" },
      "enabled": true
    },
    {
      "group_id": "group-2", 
      "values": { "APP_NAME": "my-app-2", "REGION": "us-west-2" },
      "enabled": true
    },
    {
      "group_id": "group-3",
      "values": { "APP_NAME": "my-app-dev", "REGION": "eu-west-1" },
      "enabled": false
    }
  ],
  "extraction_date": "2025-01-15"
}
```

---

## API Endpoints

### New Endpoints

```
GET  /api/workflow/variable-groups?workspace_path={path}
     Response: { manifest: VariableGroupsManifest }

POST /api/workflow/variable-groups?workspace_path={path}
     Body: { group: VariableGroup }
     Response: { success: true, group_id: "group-3" }

PUT  /api/workflow/variable-groups/{group_id}?workspace_path={path}
     Body: { values: {...}, enabled: true/false }
     Response: { success: true }

DELETE /api/workflow/variable-groups/{group_id}?workspace_path={path}
     Response: { success: true }

PUT /api/workflow/variable-groups/toggle?workspace_path={path}
     Body: { group_ids: ["group-1", "group-3"], enabled: true }
     Response: { success: true }
```

---

## Backend Changes

### 1. Data Structure Updates

**File: `variable_extraction_agent.go`**
- Update `VariablesManifest` to support `Groups` field
- Add `VariableGroup` struct
- Update parsing logic to handle both old and new formats

### 2. Variable Loading

**File: `variable_management.go`**
- `LoadVariableValues()` - Accept optional `groupID` parameter
- `LoadVariableGroups()` - New function to load all groups
- `SaveVariableGroups()` - Save updated groups
- `GetEnabledGroups()` - Return only enabled groups

### 3. Execution Loop Changes

**File: `controller.go`**
- `CreateTodoList()` - Wrap in outer loop for batch execution
- `CreateTodoListForGroup()` - Execute workflow for single group
- Track batch progress across groups
- Handle folder naming: `iteration-X/group-Y` (nested structure for multi-group)
- Respect `run_mode` and `selectedRunFolder` settings

### 4. Progress Tracking

**File: `controller_progress.go`**
- `BatchExecutionProgress` struct
- `loadBatchProgress()` / `saveBatchProgress()`
- Emit `batch_progress_updated` events

### 5. New Events

**File: `events/types.go`**
```go
BatchExecutionStarted   EventType = "batch_execution_started"
GroupExecutionStarted   EventType = "group_execution_started"
GroupExecutionCompleted EventType = "group_execution_completed"
BatchExecutionCompleted EventType = "batch_execution_completed"
BatchProgressUpdated    EventType = "batch_progress_updated"
```

---

## Frontend Changes

### 1. New Components

**VariablesNode (`frontend/src/components/workflow/nodes/VariablesNode.tsx`)**
```tsx
// Display: variable names + group count + enabled indicator
// Shows: "Variables • 2/10 Groups"
// Lists variable names
// Shows enabled groups at bottom
```

**VariablesSidebar (`frontend/src/components/workflow/canvas/VariablesSidebar.tsx`)**
```tsx
// Tabs or table view of all groups
// Edit values inline
// Enable/disable toggles
// Add new group button
// Delete group button
```

### 2. Updated Components

**usePlanToFlow.ts**
- Add Variables node after Start node
- Connect Start → Variables → Step 1

**WorkflowToolbar.tsx**
- Show batch progress: "Running: group-1 • Step 3/10"
- Group selector in iteration dropdown (optional)

**useWorkflowStore.ts**
- Add `variableGroups` state
- Add `loadVariableGroups()` action
- Add `updateVariableGroup()` action
- Add `toggleGroupEnabled()` action
- Add `currentExecutingGroup` state

### 3. New Types

**api-types.ts**
```typescript
interface VariableGroup { ... }
interface VariableGroupsManifest { ... }
interface BatchExecutionProgress { ... }
```

---

## Execution Flow

### Single Group (Backward Compatible)

```
1. Load variables.json
2. Check if "groups" field exists
3. If NO groups: use old format (single set of values)
4. Run workflow once
5. Output: runs/iteration-1/
```

### Multiple Groups

```
1. Load variables.json
2. Check if "groups" field exists
3. If YES: filter to enabled groups only
4. Determine base iteration folder based on run_mode:
   a. If user selected folder: use it (extract iteration-X from nested paths)
   b. If create_new_runs_always: create new iteration folder
   c. If use_same_run: use latest existing iteration folder
5. For each enabled group:
   a. Set hcpo.variableValues = group.Values
   b. Set folder name = iteration-X/group-Y (nested structure)
   c. Run full workflow
   d. Mark group as completed
   e. Emit group_execution_completed event
6. Emit batch_execution_completed event
7. Output: runs/iteration-1/group-1/, runs/iteration-1/group-3/
```

### Folder Naming Logic

```go
// createBatchRunFolderName creates the run folder name for batch execution
// Format: iteration-X/group-Y (nested folder structure for multiple groups)
// Format: iteration-X (when single group - use top-level folder)
func createBatchRunFolderName(iterationNum int, groupID string, totalGroups int) string {
    if totalGroups <= 1 {
        // Single group or no groups - use top-level iteration folder (not nested)
        return fmt.Sprintf("iteration-%d", iterationNum)
    }
    // Multiple groups - use nested folder structure
    return fmt.Sprintf("iteration-%d/%s", iterationNum, groupID)
}
```

### Run Mode Behavior

Batch execution respects the `run_mode` setting from `ExecutionOptions`:

1. **User Selected Folder** (`selectedRunFolder` is set):
   - Uses the selected folder directly
   - If nested (e.g., `iteration-1/group-1`), extracts base iteration folder (`iteration-1`)
   - Creates group subfolders within the selected iteration

2. **Create New Run Mode** (`create_new_runs_always`):
   - Always creates a new iteration folder
   - Uses `getNextIterationNumber()` to find the next available iteration
   - Creates nested group folders: `iteration-X/group-Y`

3. **Use Same Run Mode** (`use_same_run`):
   - Uses the latest existing iteration folder (finds highest iteration number)
   - If no folders exist, creates `iteration-1`
   - Creates group subfolders within the existing iteration: `iteration-X/group-Y`

---

## Migration & Backward Compatibility

### Old Format Detection

```go
func (manifest *VariablesManifest) HasGroups() bool {
    return len(manifest.Groups) > 0
}

func (manifest *VariablesManifest) GetVariableValues(groupID string) map[string]string {
    if !manifest.HasGroups() {
        // Old format: values are in Variables[].Value
        values := make(map[string]string)
        for _, v := range manifest.Variables {
            values[v.Name] = v.Value
        }
        return values
    }
    // New format: find group by ID
    for _, g := range manifest.Groups {
        if g.GroupID == groupID {
            return g.Values
        }
    }
    return nil
}
```

### Migration (Optional)

Users can optionally migrate old format to new:
```json
// Old format with values
{ "variables": [{ "name": "X", "value": "1" }] }

// Becomes new format
{ 
  "variables": [{ "name": "X", "description": "" }],
  "groups": [{ "group_id": "group-1", "values": { "X": "1" }, "enabled": true }]
}
```

---

## Implementation Order

### Phase 1: Backend Foundation
1. ✅ Add data structures
2. ✅ Add API endpoints
3. ✅ Update variable loading

### Phase 2: Frontend Variables Node
4. Create VariablesNode component
5. Create VariablesSidebar component
6. Update usePlanToFlow

### Phase 3: Execution Integration
7. Modify execution loop for batch
8. Update folder naming
9. Add batch progress events

### Phase 4: Frontend Polish
10. Update toolbar for batch progress
11. Update store with group state
12. Test end-to-end

---

## Success Criteria

| Criteria | Status |
|----------|--------|
| Variables node shows in React Flow after Start | 🔲 |
| Click node opens sidebar with groups | 🔲 |
| Can add new group (empty values) | 🔲 |
| Can edit group values | 🔲 |
| Can enable/disable groups | 🔲 |
| Can delete groups | 🔲 |
| Single group: no folder suffix (iteration-X) | 🔲 |
| Multi-group: correct nested folder naming (iteration-X/group-Y) | 🔲 |
| Run mode respected (use_same_run vs create_new_runs_always) | 🔲 |
| Selected folder respected in batch execution | 🔲 |
| Execution runs enabled groups only | 🔲 |
| Progress shows current group | 🔲 |
| Stop cancels all groups | 🔲 |
| Backward compatible with old format | 🔲 |

