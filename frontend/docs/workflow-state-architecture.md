# Workflow State Architecture: Single Source of Truth

## Problem Statement

Currently, workflow execution state (group IDs, run folders, start points) is stored and converted in multiple places:
- State might have folder names instead of group_ids
- localStorage might have stale/wrong formats
- UI displays friendly names but uses different values internally
- API receives values that might not match what user selected

This causes bugs like:
- User selects "iteration-4/manishiithuf" but API receives "iteration-4/mahimakh"
- Page reload loses user selections
- Wrong groups executed

## Solution: Single Source of Truth Architecture

### Core Principles

1. **Canonical State**: Store ONLY normalized values in state
2. **Single Normalization Layer**: One place to convert between formats
3. **Clear Separation**: Display values vs Canonical values vs API values
4. **Validation at Boundaries**: Validate when loading from localStorage and building API payloads

### Architecture Layers

```
┌─────────────────────────────────────────────────────────┐
│                    UI LAYER (Display)                    │
│  Shows: "Manishi Ithuf", "Resume from Step 4"          │
│  Uses: getGroupDisplayInfo() to convert group_id → UI   │
└────────────────────┬────────────────────────────────────┘
                     │
                     ▼
┌─────────────────────────────────────────────────────────┐
│              CANONICAL STATE (Single Source)             │
│  Stores: ["group-1"], selectedStartPoint: 4             │
│  Always normalized, never folder names                  │
└────────────────────┬────────────────────────────────────┘
                     │
         ┌───────────┴───────────┐
         │                       │
         ▼                       ▼
┌──────────────────┐    ┌──────────────────┐
│  localStorage    │    │   API LAYER      │
│  (Persistence)   │    │  (Execution)     │
│                  │    │                  │
│  Save:          │    │  Build:          │
│  normalize()    │    │  buildExecOpts() │
│                 │    │                  │
│  Load:          │    │  Uses canonical  │
│  normalize()    │    │  state directly  │
└──────────────────┘    └──────────────────┘
```

### Data Flow

#### 1. User Selection (Checkbox Click)
```
UI Event → toggleGroupSelection(group.groupId)
         → State: selectedGroupIds = ["group-1"]
         → localStorage: Save normalized ["group-1"]
```

#### 2. Page Reload
```
localStorage: ["manishiithuf"] (old format)
           → normalizeGroupIds()
           → State: ["group-1"] (normalized)
           → UI: Shows "Manishi Ithuf" (via getGroupDisplayInfo)
```

#### 3. Execution
```
State: selectedGroupIds = ["group-1"]
     → buildExecutionOptions()
     → API: enabled_group_ids = ["group-1"]
```

### Key Functions

#### `normalizeGroupIds(input, manifest)`
- **Purpose**: Convert any format (folder names, display names, group_ids) to canonical group_ids
- **Input**: `["manishiithuf", "group-2"]` (mixed formats)
- **Output**: `["group-1", "group-2"]` (all group_ids)
- **Used**: When loading from localStorage, when processing user input

#### `validateGroupIds(groupIds, manifest)`
- **Purpose**: Ensure group_ids exist in manifest and are enabled
- **Input**: `["group-1", "group-99"]`
- **Output**: `["group-1"]` (filters out invalid/disabled)
- **Used**: Before sending to API

#### `getGroupDisplayInfo(groupId, manifest, iteration)`
- **Purpose**: Convert canonical group_id to display information
- **Input**: `"group-1"`, manifest, `"iteration-4"`
- **Output**: `{ groupId: "group-1", displayName: "Manishi Ithuf", folderPath: "iteration-4/manishiithuf" }`
- **Used**: In UI to show friendly names

#### `normalizeStartPoint(input)`
- **Purpose**: Normalize start point to canonical format
- **Input**: `"4"`, `4`, `undefined`
- **Output**: `4` (number, 0 = beginning, >0 = step number)
- **Used**: When loading from localStorage

#### `normalizeRunFolder(folderPath, manifest)`
- **Purpose**: Validate and normalize run folder path
- **Input**: `"iteration-4/manishiithuf"`
- **Output**: `"iteration-4/manishiithuf"` (validated) or `"new"` (invalid)
- **Used**: When setting selectedRunFolder

### Implementation Checklist

#### ✅ Phase 1: Normalization Utilities (DONE)
- [x] Create `workflowStateNormalization.ts`
- [x] Implement `normalizeGroupIds()`
- [x] Implement `validateGroupIds()`
- [x] Implement `getGroupDisplayInfo()`
- [x] Implement `normalizeStartPoint()`
- [x] Implement `normalizeRunFolder()`

#### 🔄 Phase 2: Update Store (TODO)
- [ ] Update `loadSavedSettings()` to use `normalizeGroupIds()`
- [ ] Update `saveSettings()` to ensure only canonical values saved
- [ ] Update `toggleGroupSelection()` to validate group_id format
- [ ] Update `setStartPoint()` to use `normalizeStartPoint()`
- [ ] Update `setSelectedRunFolder()` to use `normalizeRunFolder()`

#### 🔄 Phase 3: Update UI (TODO)
- [ ] Update `WorkflowToolbar` to use `getGroupDisplayInfo()` for display
- [ ] Ensure checkboxes always use `group.groupId` (canonical)
- [ ] Update start point dropdown to use normalized values

#### 🔄 Phase 4: Update API Layer (TODO)
- [ ] Update `buildExecutionOptions()` to use `validateGroupIds()`
- [ ] Ensure all values are canonical before sending to API
- [ ] Add validation logging

### Benefits

1. **Consistency**: Same normalization logic everywhere
2. **Reliability**: Old localStorage data automatically normalized
3. **Maintainability**: Single place to fix normalization bugs
4. **Type Safety**: Clear separation between display and canonical values
5. **Debugging**: Easy to trace where values come from

### Migration Strategy

1. **Gradual Migration**: Use normalization functions alongside existing code
2. **Backward Compatible**: Normalize old localStorage data on load
3. **No Breaking Changes**: Existing code continues to work
4. **Incremental**: Update one layer at a time

### Example: Fixing the "manishiithuf" → "mahimakh" Bug

**Before (Current)**:
```typescript
// Multiple places doing normalization
localStorage: "manishiithuf" → loaded as-is → state: "manishiithuf" → API: "manishiithuf" ❌
```

**After (With Normalization)**:
```typescript
// Single normalization point
localStorage: "manishiithuf" 
  → normalizeGroupIds(["manishiithuf"], manifest) 
  → state: ["group-1"] 
  → validateGroupIds(["group-1"], manifest)
  → API: ["group-1"] ✅
```

### Testing Checklist

- [ ] Select group via checkbox → verify correct group_id in state
- [ ] Reload page → verify selections preserved with correct group_ids
- [ ] Select "Resume from Step 4" → verify step number saved correctly
- [ ] Reload page → verify resume point preserved
- [ ] Execute → verify API receives correct group_ids and step numbers
- [ ] Test with old localStorage data (folder names) → verify normalization works
