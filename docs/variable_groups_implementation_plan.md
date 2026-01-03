# Variable Groups Implementation

## 📋 Overview

Variable groups enable batch execution of workflows with multiple sets of variable values. Each group contains the same variable names but different values. Users can enable/disable groups per run, and the workflow executes sequentially for each enabled group. The system supports both single-group (backward compatible) and multi-group modes.

**Key Features:**
- Multiple variable groups with shared variable definitions
- Enable/disable groups for selective execution
- Sequential batch execution for enabled groups
- Nested folder structure for multi-group runs
- Backward compatible with single-group format
- Optional display names for groups

---

## 📁 Key Files & Locations

| Component | File Path | Key Functions/Exports |
|-----------|-----------|---------------------|
| **Variable Structures** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/variable_extraction_agent.go`](file:///Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/variable_extraction_agent.go) | `VariableGroup`, `VariablesManifest`, `HasGroups()`, `GetEnabledGroups()` |
| **Batch Execution** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_batch_execution.go`](file:///Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_batch_execution.go) | `runBatchExecution()`, `getEnabledGroupsForExecution()` |
| **API Handlers** | [`agent_go/cmd/server/workflow.go`](file:///Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/workflow.go) | `handleGetVariableGroups()`, `handleUpdateVariableGroups()` |
| **Variables Node** | [`frontend/src/components/workflow/nodes/VariablesNode.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/nodes/VariablesNode.tsx) | `VariablesNode` React Flow node component |
| **Variables Sidebar** | [`frontend/src/components/workflow/canvas/VariablesSidebar.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/canvas/VariablesSidebar.tsx) | `VariablesSidebar` group management UI |
| **API Types** | [`frontend/src/services/api-types.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/services/api-types.ts) | `VariableGroup`, `VariablesManifest`, `BatchExecutionProgress` |
| **API Service** | [`frontend/src/services/api.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/services/api.ts) | `agentApi.getVariableGroups()`, `agentApi.updateVariableGroups()` |

---

## 🧩 Data Structures

### Backend (Go)

```go
// Variable represents a single variable definition
type Variable struct {
    Name        string `json:"name"`        // e.g., "AWS_ACCOUNT_ID"
    Value       string `json:"value"`       // Original value (used in single-group mode)
    Description string `json:"description"` // e.g., "AWS account number for deployment"
}

// VariableGroup represents a single set of variable values for batch execution
type VariableGroup struct {
    GroupID     string            `json:"group_id"`     // e.g., "group-1", "group-2"
    DisplayName string            `json:"display_name"` // Optional user-friendly name (e.g., "Production", "Staging")
    Values      map[string]string `json:"values"`       // Variable name -> value mapping
    Enabled     bool              `json:"enabled"`     // Whether to include in execution
}

// VariablesManifest contains all extracted variables
// Supports both single-group (backward compatible) and multi-group modes
type VariablesManifest struct {
    Objective      string          `json:"objective"`        // Templated objective with {{VARS}}
    Variables      []Variable      `json:"variables"`        // List of variable definitions
    Groups         []VariableGroup `json:"groups,omitempty"` // Array of variable groups (multi-group mode)
    ExtractionDate string          `json:"extraction_date"`
}
```

**Key Methods:**
- `HasGroups() bool` - Returns true if manifest has multiple groups
- `GetEnabledGroups() []VariableGroup` - Returns only enabled groups (creates virtual group for single-group mode)
- `GetVariableValues(groupID string) map[string]string` - Gets values for specific group
- `AddGroup() *VariableGroup` - Adds new group with empty values
- `DeleteGroup(groupID string) bool` - Removes group by ID
- `ToggleGroup(groupID string, enabled bool) bool` - Enables/disables group
- `UpdateGroupValues(groupID string, values map[string]string) bool` - Updates group values

### Frontend (TypeScript)

```typescript
// Variable definition (shared across groups)
interface Variable {
  name: string;
  value?: string;  // Used in single-group mode
  description: string;
}

// Single variable group
interface VariableGroup {
  group_id: string;  // e.g., "group-1", "group-2" (used as fallback for folder names)
  display_name?: string;  // Optional user-friendly name (e.g., "Production", "Staging")
  values: Record<string, string>;  // Variable name -> value mapping
  enabled: boolean;
}

// Full manifest
interface VariablesManifest {
  objective: string;  // Templated objective with {{VARS}}
  variables: Variable[];  // Variable definitions
  groups?: VariableGroup[];  // Array of variable groups (multi-group mode)
  extraction_date: string;
}

// Batch execution progress
interface BatchExecutionProgress {
  total_groups: number;
  enabled_groups: string[];  // Group IDs to execute
  completed_groups: string[];  // Group IDs that finished
  current_group: string;  // Currently executing group ID
  group_progress: Record<string, StepProgress>;  // Per-group step progress
  iteration_number: number;
}
```

---

## 🔌 API Endpoints

### Get Variable Groups

```
GET  /api/workflow/variable-groups?workspace_path={path}
     Response: {
       success: boolean
       manifest?: VariablesManifest
       error?: string
     }
```

**Purpose:** Load variable groups from `variables/variables.json`.

### Update Variable Groups

```
PUT  /api/workflow/variable-groups?workspace_path={path}
     Body: VariablesManifest
     Response: {
       success: boolean
       message: string
     }
```

**Purpose:** Save updated variable groups to `variables/variables.json`. Used for all group operations (add, update, delete, toggle).

**Note:** The frontend manages group operations locally and sends the complete manifest to update. There are no separate endpoints for individual group operations.

---

## 🔄 Execution Flow

### Single Group (Backward Compatible)

```
1. Load variables.json
2. Check if "groups" field exists and has length > 0
3. If NO groups: use old format (single set of values from Variables[].Value)
4. Create virtual group "group-1" with values from Variables
5. Run workflow once
6. Output: runs/iteration-1/ (top-level folder, not nested)
```

### Multiple Groups

```
1. Load variables.json
2. Check if "groups" field exists and has length > 0
3. If YES: filter to enabled groups only (or use ExecutionOptions.EnabledGroupIDs)
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

### Group Selection Priority

The system uses the following priority for determining which groups to execute:

1. **ExecutionOptions.EnabledGroupIDs** (if specified) - Uses explicitly requested group IDs
2. **Manifest Enabled Groups** - Falls back to groups with `enabled: true` in manifest

**Code:**
```go
// From controller_batch_execution.go
func (hcpo *HumanControlledTodoPlannerOrchestrator) getEnabledGroupsForExecution() []VariableGroup {
    // Check if ExecutionOptions specifies specific group IDs
    if hcpo.executionOptions != nil && len(hcpo.executionOptions.EnabledGroupIDs) > 0 {
        // Use specified group IDs from ExecutionOptions
        return groups // Filtered by requested IDs
    }
    // Fall back to manifest's enabled groups
    return hcpo.variablesManifest.GetEnabledGroups()
}
```

### Folder Naming Logic

```go
// Single group: iteration-X (top-level folder)
// Multiple groups: iteration-X/group-Y (nested folder structure)

// Implementation determines folder name based on total enabled groups
if totalGroups <= 1 {
    return fmt.Sprintf("iteration-%d", iterationNum)
}
return fmt.Sprintf("iteration-%d/%s", iterationNum, groupID)
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

## 📊 File Structure

### Workspace Layout

```
workspace/
├── variables/
│   └── variables.json          # Contains groups array (if multi-group)
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

### variables.json Format

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
      "display_name": "Production",
      "values": { "APP_NAME": "my-app", "REGION": "us-east-1" },
      "enabled": true
    },
    {
      "group_id": "group-2",
      "display_name": "Staging",
      "values": { "APP_NAME": "my-app-2", "REGION": "us-west-2" },
      "enabled": true
    },
    {
      "group_id": "group-3",
      "display_name": "Development",
      "values": { "APP_NAME": "my-app-dev", "REGION": "eu-west-1" },
      "enabled": false
    }
  ],
  "extraction_date": "2025-01-15"
}
```

---

## 🎨 Frontend Components

### VariablesNode

**File:** [`frontend/src/components/workflow/nodes/VariablesNode.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/nodes/VariablesNode.tsx)

**Features:**
- Displays variable count and group count
- Shows enabled groups indicator (e.g., "2/3 Groups")
- Lists variable names and values for each group
- Shows group status (running, selected, enabled, disabled)
- Supports display names for groups
- Click to open VariablesSidebar

**Display Format:**
- Header: "Variables (N)" with group count if multiple groups
- Group list: Shows all groups with their values (first 3 variables visible)
- Status indicators: Running (blue), Selected (purple), Enabled (green), Disabled (gray)

### VariablesSidebar

**File:** [`frontend/src/components/workflow/canvas/VariablesSidebar.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/canvas/VariablesSidebar.tsx)

**Features:**
- Table view of all groups
- Edit values inline
- Enable/disable toggles
- Add new group button
- Delete group button
- Edit display names
- Save changes to backend

**Operations:**
- **Add Group**: Creates new group with empty values for all variables
- **Update Group**: Updates values or display name
- **Toggle Enabled**: Enables/disables group
- **Delete Group**: Removes group from manifest
- **Save**: Sends complete manifest to backend via `PUT /api/workflow/variable-groups`

---

## 🔄 Backend Implementation

### Batch Execution

**File:** [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_batch_execution.go`](file:///Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/controller_batch_execution.go)

**Key Functions:**
- `getEnabledGroupsForExecution()` - Determines which groups to execute (respects ExecutionOptions.EnabledGroupIDs)
- `shouldUseBatchExecution()` - Checks if batch mode should be used (>1 enabled group)
- `runBatchExecution()` - Executes workflow sequentially for each enabled group

**Execution Flow:**
1. Get enabled groups (from ExecutionOptions or manifest)
2. Validate groups exist
3. For each group:
   - Set variable values
   - Create run folder (nested structure for multi-group)
   - Execute workflow
   - Track progress
   - Emit events
4. Return batch execution result

### Events

**Batch Execution Events:**
- `batch_execution_started` - Batch execution begins
- `group_execution_started` - Individual group execution starts
- `group_execution_completed` - Individual group execution completes
- `batch_execution_completed` - All groups completed
- `batch_progress_updated` - Progress update during batch execution

---

## 🔍 Migration & Backward Compatibility

### Old Format Detection

The system automatically detects single-group format:

```go
func (m *VariablesManifest) HasGroups() bool {
    return len(m.Groups) > 0
}

func (m *VariablesManifest) GetEnabledGroups() []VariableGroup {
    if !m.HasGroups() {
        // Single group mode: create a virtual group from Variables
        values := make(map[string]string)
        for _, v := range m.Variables {
            values[v.Name] = v.Value
        }
        return []VariableGroup{{
            GroupID: "group-1",
            Values:  values,
            Enabled: true,
        }}
    }
    // Multi-group mode: return enabled groups
    var enabled []VariableGroup
    for _, g := range m.Groups {
        if g.Enabled {
            enabled = append(enabled, g)
        }
    }
    return enabled
}
```

### Backward Compatibility

✅ **Single-group format** is fully supported:
- Old format with `Variables[].Value` works without changes
- System creates virtual group automatically
- Folder structure remains `iteration-X/` (not nested)

✅ **Multi-group format**:
- Requires `groups` array in `variables.json`
- Variables array contains only definitions (no values)
- Folder structure uses nested format: `iteration-X/group-Y/`

---

## 🛠️ Common Issues & Solutions

| Issue | Cause | Solution |
|-------|-------|----------|
| Groups not executing | Groups disabled in manifest | Check `enabled: true` for groups |
| Wrong groups executing | ExecutionOptions.EnabledGroupIDs not respected | Verify group IDs match manifest |
| Folder structure wrong | Single group using nested format | Check `totalGroups <= 1` logic |
| Variables not loading | variables.json missing or invalid | Verify file exists and JSON is valid |
| Groups not saving | API call failing | Check workspace_path parameter |

---

## 🔍 For LLMs: Quick Reference

### Key Types

```typescript
// Get variable groups
const response = await agentApi.getVariableGroups(workspacePath)
const manifest = response.manifest

// Update variable groups (complete manifest)
await agentApi.updateVariableGroups(workspacePath, manifest)

// Check if multi-group mode
const isMultiGroup = manifest?.groups && manifest.groups.length > 1

// Get enabled groups
const enabledGroups = manifest?.groups?.filter(g => g.enabled) || []
```

### Constraints

✅ **Allowed:**
- Multiple groups with same variable names
- Optional `display_name` for groups
- Enabling/disabling groups
- ExecutionOptions.EnabledGroupIDs for selective execution
- Nested folder structure for multi-group

❌ **Forbidden:**
- Different variable names across groups (all groups share same variables)
- Empty groups array (use single-group format instead)
- Missing `group_id` field

### Common Patterns

**Pattern 1: Add New Group**
```typescript
const newGroup: VariableGroup = {
  group_id: `group-${manifest.groups.length + 1}`,
  values: {}, // Initialize with empty values
  enabled: true
}
manifest.groups.push(newGroup)
await agentApi.updateVariableGroups(workspacePath, manifest)
```

**Pattern 2: Toggle Group Enabled**
```typescript
const group = manifest.groups.find(g => g.group_id === groupId)
if (group) {
  group.enabled = !group.enabled
  await agentApi.updateVariableGroups(workspacePath, manifest)
}
```

**Pattern 3: Execute Specific Groups**
```typescript
const executionOptions: ExecutionOptions = {
  enabledGroupIDs: ['group-1', 'group-3'], // Only execute these groups
  // ... other options
}
```

---

## 📖 Related Documentation

- [Workflow Orchestrator](workflow_orchestrator.md) - Workflow execution architecture
- [Step Config Format](step_config_format_specification.md) - Step configuration details

---

## Summary

Variable groups enable batch execution with multiple sets of variable values. The system supports both single-group (backward compatible) and multi-group modes. Groups are managed via `VariablesManifest` stored in `variables/variables.json`. Execution can be controlled via `ExecutionOptions.EnabledGroupIDs` or manifest's `enabled` flags. Folder structure uses nested format (`iteration-X/group-Y`) for multi-group and top-level (`iteration-X`) for single-group.
