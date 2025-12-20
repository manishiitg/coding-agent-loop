# Plan and Step Config Backend API Refactoring

## Current Problem

**Frontend has full update logic:**
1. Frontend reads `plan.json` and `step_config.json`
2. Frontend merges `agent_configs` from `step_config.json` into plan steps
3. Frontend updates/modifies the data in JavaScript
4. Frontend writes complete files back via `updatePlannerFile` API
5. Frontend tries to separate `agent_configs` but logic is complex and error-prone

**Issues:**
- ❌ Logic duplication between frontend and backend
- ❌ Risk of `agent_configs` ending up in `plan.json` (see `type_safety_and_config_separation_issues.md`)
- ❌ Complex frontend code handling file reads, merges, and writes
- ❌ No single source of truth for update logic
- ❌ Hard to ensure consistency (e.g., stripping `agent_configs` from plan.json)

## Proposed Solution

**Backend handles all update logic:**
1. Frontend sends **update instructions** (what to change, not full files)
2. Backend reads current `plan.json` and `step_config.json`
3. Backend applies updates with proper validation
4. Backend ensures `agent_configs` stays in `step_config.json` only
5. Backend writes both files atomically if needed
6. Backend returns updated state

## New API Endpoints

### 1. Update Plan Step

**Endpoint:** `POST /api/workflow/plan/update-step`

**Request:**
```json
{
  "workspace_path": "Workflow/HDFC Personal Accounts",
  "step_id": "update-sheet-with-transactions",
  "updates": {
    "title": "Updated Title",
    "description": "Updated description",
    "success_criteria": "New criteria"
    // NO agent_configs here - that goes to separate endpoint
  }
}
```

**Response:**
```json
{
  "success": true,
  "message": "Step updated successfully",
  "data": {
    "step": {
      "id": "update-sheet-with-transactions",
      "title": "Updated Title",
      // ... updated step (NO agent_configs)
    }
  }
}
```

**Backend Logic:**
1. Read `plan.json`
2. Find step by `step_id` (recursively search nested steps)
3. Apply updates (merge with existing step)
4. **Strip any `agent_configs` if present** (validation)
5. Write updated `plan.json`
6. Return updated step

---

### 2. Update Step Config (agent_configs)

**Endpoint:** `POST /api/workflow/plan/update-step-config`

**Request:**
```json
{
  "workspace_path": "Workflow/HDFC Personal Accounts",
  "step_id": "update-sheet-with-transactions",
  "agent_configs": {
    "use_code_execution_mode": true,
    "execution_llm": {
      "provider": "vertex",
      "model_id": "gemini-3-flash-preview"
    },
    "execution_max_turns": 50
  }
}
```

**Response:**
```json
{
  "success": true,
  "message": "Step config updated successfully",
  "data": {
    "step_id": "update-sheet-with-transactions",
    "agent_configs": {
      // ... updated config
    }
  }
}
```

**Backend Logic:**
1. Read `step_config.json`
2. Find or create config for `step_id`
3. Merge `agent_configs` (partial update support)
4. Write updated `step_config.json`
5. Return updated config

---

### 3. Batch Update Steps

**Endpoint:** `POST /api/workflow/plan/batch-update-steps`

**Request:**
```json
{
  "workspace_path": "Workflow/HDFC Personal Accounts",
  "updates": [
    {
      "step_id": "step-1",
      "plan_updates": {
        "title": "New Title 1"
      },
      "config_updates": {
        "use_code_execution_mode": true
      }
    },
    {
      "step_id": "step-2",
      "plan_updates": {
        "description": "New Description"
      }
    }
  ]
}
```

**Response:**
```json
{
  "success": true,
  "message": "Batch update completed",
  "data": {
    "updated_steps": 2,
    "updated_configs": 1
  }
}
```

**Backend Logic:**
1. Read both `plan.json` and `step_config.json`
2. Apply all updates
3. Validate (strip `agent_configs` from plan updates)
4. Write both files atomically
5. Return summary

---

### 4. Delete Step

**Endpoint:** `POST /api/workflow/plan/delete-step`

**Request:**
```json
{
  "workspace_path": "Workflow/HDFC Personal Accounts",
  "step_id": "step-to-delete"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Step deleted successfully",
  "data": {
    "deleted_step_id": "step-to-delete",
    "deleted_config": true  // Whether step_config was also removed
  }
}
```

**Backend Logic:**
1. Read `plan.json`
2. Find and remove step (handle nested steps)
3. Read `step_config.json`
4. Remove step config if exists
5. Write both files
6. Return confirmation

---

### 5. Add Step

**Endpoint:** `POST /api/workflow/plan/add-step`

**Request:**
```json
{
  "workspace_path": "Workflow/HDFC Personal Accounts",
  "step": {
    "id": "new-step-id",
    "title": "New Step",
    "description": "Step description",
    "success_criteria": "Criteria",
    // ... step fields (NO agent_configs)
  },
  "insert_after_step_id": "existing-step-id",  // Optional
  "parent_step_id": "parent-step-id",          // Optional (for nested steps)
  "branch_type": "if_true"                     // Optional (for conditional branches)
}
```

**Response:**
```json
{
  "success": true,
  "message": "Step added successfully",
  "data": {
    "step": {
      // ... added step
    }
  }
}
```

---

## Implementation Details

### Backend Structure

```
agent_go/cmd/server/
  workflow_routes.go          # New: API route handlers
  workflow_handlers.go        # New: Business logic handlers
```

### Handler Functions

```go
// workflow_handlers.go

type PlanUpdateRequest struct {
    WorkspacePath string                 `json:"workspace_path"`
    StepID        string                 `json:"step_id"`
    Updates       map[string]interface{} `json:"updates"`  // Partial step update
}

type StepConfigUpdateRequest struct {
    WorkspacePath string                 `json:"workspace_path"`
    StepID        string                 `json:"step_id"`
    AgentConfigs *AgentConfigs           `json:"agent_configs"`
}

type BatchUpdateRequest struct {
    WorkspacePath string                 `json:"workspace_path"`
    Updates       []StepUpdate           `json:"updates"`
}

type StepUpdate struct {
    StepID       string                 `json:"step_id"`
    PlanUpdates  map[string]interface{} `json:"plan_updates,omitempty"`
    ConfigUpdates *AgentConfigs         `json:"config_updates,omitempty"`
}

// Core handler functions
func (api *StreamingAPI) handleUpdatePlanStep(w http.ResponseWriter, r *http.Request)
func (api *StreamingAPI) handleUpdateStepConfig(w http.ResponseWriter, r *http.Request)
func (api *StreamingAPI) handleBatchUpdateSteps(w http.ResponseWriter, r *http.Request)
func (api *StreamingAPI) handleDeleteStep(w http.ResponseWriter, r *http.Request)
func (api *StreamingAPI) handleAddStep(w http.ResponseWriter, r *http.Request)

// Helper functions
func updateStepInPlan(plan *PlanningResponse, stepID string, updates map[string]interface{}) error
func stripAgentConfigsFromStep(step *PlanStep)  // Remove agent_configs if present
func validateStepUpdate(updates map[string]interface{}) error
func findStepInPlan(plan *PlanningResponse, stepID string) (*PlanStep, []int)  // Returns step and path indices
```

### Validation Rules

1. **Plan Updates:**
   - ❌ Reject if `updates` contains `agent_configs` → return error
   - ✅ Strip `agent_configs` if somehow present (defensive)
   - ✅ Validate step structure (ID, required fields)
   - ✅ Validate nested step references

2. **Config Updates:**
   - ✅ Validate `agent_configs` structure
   - ✅ Merge with existing config (partial updates)
   - ✅ Create config if step_id doesn't exist

3. **Atomic Operations:**
   - ✅ Read both files before writing
   - ✅ Write both files in transaction (if possible)
   - ✅ Rollback on error

---

## Frontend Changes

### Before (Current)

```typescript
// usePlanData.ts
const updateStep = async (stepIndex: number, updates: Partial<PlanStep>) => {
  // Read plan
  const updatedSteps = [...plan.steps]
  updatedSteps[stepIndex] = { ...updatedSteps[stepIndex], ...updates }
  
  // Separate plan and config updates
  const shouldSavePlan = hasPlanRelatedFields(updates)
  if (shouldSavePlan) {
    await savePlan(updatedPlan)  // Writes full plan.json
  }
  
  if ('agent_configs' in updates) {
    await saveStepConfig(stepId, updates.agent_configs)  // Reads, merges, writes step_config.json
  }
}
```

### After (New)

```typescript
// usePlanData.ts
const updateStep = async (stepIndex: number, updates: Partial<PlanStep>) => {
  const stepId = plan.steps[stepIndex].id
  
  // Separate plan and config updates
  const { agent_configs, ...planUpdates } = updates
  
  // Send update instructions to backend
  const promises = []
  
  if (Object.keys(planUpdates).length > 0) {
    promises.push(
      agentApi.updatePlanStep(workspacePath, stepId, planUpdates)
    )
  }
  
  if (agent_configs !== undefined) {
    promises.push(
      agentApi.updateStepConfig(workspacePath, stepId, agent_configs)
    )
  }
  
  await Promise.all(promises)
  
  // Refresh plan from backend
  await loadPlan()
}
```

### New API Service Methods

```typescript
// services/api.ts

export const agentApi = {
  // ... existing methods
  
  updatePlanStep: async (
    workspacePath: string,
    stepId: string,
    updates: Partial<PlanStep>
  ) => {
    const response = await fetch(
      `${API_BASE_URL}/api/workflow/plan/update-step?workspace_path=${encodeURIComponent(workspacePath)}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ step_id: stepId, updates })
      }
    )
    return response.json()
  },
  
  updateStepConfig: async (
    workspacePath: string,
    stepId: string,
    agentConfigs: AgentConfigs | undefined
  ) => {
    const response = await fetch(
      `${API_BASE_URL}/api/workflow/plan/update-step-config?workspace_path=${encodeURIComponent(workspacePath)}`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ step_id: stepId, agent_configs: agentConfigs })
      }
    )
    return response.json()
  },
  
  batchUpdateSteps: async (
    workspacePath: string,
    updates: Array<{
      stepId: string
      planUpdates?: Partial<PlanStep>
      configUpdates?: AgentConfigs
    }>
  ) => {
    // ... implementation
  },
  
  deleteStep: async (workspacePath: string, stepId: string) => {
    // ... implementation
  },
  
  addStep: async (
    workspacePath: string,
    step: PlanStep,
    options?: { insertAfterStepId?: string; parentStepId?: string; branchType?: 'if_true' | 'if_false' }
  ) => {
    // ... implementation
  }
}
```

---

## Migration Strategy

### Phase 1: Add Backend APIs (Backward Compatible)
1. ✅ Implement new API endpoints
2. ✅ Keep existing `updatePlannerFile` endpoint (for other files)
3. ✅ Test new endpoints independently

### Phase 2: Update Frontend (Gradual)
1. ✅ Update `usePlanData.ts` to use new APIs
2. ✅ Keep old code commented for rollback
3. ✅ Test with real workflows

### Phase 3: Remove Old Logic
1. ✅ Remove frontend file read/write logic
2. ✅ Remove `mergeStepConfigs` function (backend handles it)
3. ✅ Remove `hasPlanRelatedFields` check (backend validates)

### Phase 4: Cleanup
1. ✅ Remove unused frontend code
2. ✅ Update documentation
3. ✅ Add backend validation tests

---

## Benefits

1. ✅ **Single Source of Truth**: All update logic in backend
2. ✅ **Data Consistency**: Backend ensures `agent_configs` never in `plan.json`
3. ✅ **Simpler Frontend**: Frontend just sends updates, no file handling
4. ✅ **Better Validation**: Backend can validate before writing
5. ✅ **Atomic Operations**: Backend can update both files atomically
6. ✅ **Easier Testing**: Backend logic can be unit tested
7. ✅ **Type Safety**: Backend can enforce Go struct validation

---

## Related Issues

- **Type Safety Issue**: This refactoring will make it easier to implement type-safe step types (see `type_safety_and_config_separation_issues.md`)
- **Config Separation Issue**: This directly fixes the `agent_configs` in `plan.json` problem

---

## Files to Modify

### Backend
- `agent_go/cmd/server/workflow.go` - Add new route handlers
- `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/planning_management.go` - Reuse existing plan read/write logic
- `agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/step_config.go` - Reuse existing config read/write logic

### Frontend
- `frontend/src/services/api.ts` - Add new API methods
- `frontend/src/components/workflow/hooks/usePlanData.ts` - Replace update logic
- `frontend/src/components/workflow/canvas/WorkflowCanvas.tsx` - Update to use new APIs
- `frontend/src/components/workflow/canvas/StepSidebar.tsx` - Update to use new APIs
- `frontend/src/components/events/orchestrator/TodoStepsExtractedEvent.tsx` - Update save logic

---

## Estimated Effort

- **Backend APIs**: ~1 week
- **Frontend Migration**: ~1 week
- **Testing**: ~3 days
- **Total**: ~2.5 weeks

---

## Next Steps

1. Design detailed API request/response schemas
2. Implement backend endpoints
3. Add validation and error handling
4. Update frontend to use new APIs
5. Test with existing workflows
6. Remove old frontend logic

