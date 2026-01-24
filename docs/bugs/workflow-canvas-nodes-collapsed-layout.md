# Bug: Workflow Canvas Nodes Appear Collapsed/Overlapping

## Status
Fixed

## Date
January 14, 2026

## Description
After simplifying workflow canvas nodes (removing description, success criteria, JSON schema display), all nodes appear collapsed on top of each other or extremely congested, despite the layout algorithm (Dagre) calculating correct positions.

## Symptoms
- All nodes visually appear stacked at approximately the same position
- User reports "everything is collapsed on top of each other"
- Console logs show Dagre IS calculating correct, spread-out positions
- Sub-agent positioning logs show correct horizontal arrangement

## Console Log Evidence
The layout algorithm calculates correct positions:
```
[Layout Debug] start (start): x=80, y=307
[Layout Debug] execution-settings (execution-settings): x=360, y=275
[Layout Debug] variables (variables): x=760, y=265
[Layout Debug] select-website-type (conditional): x=1180, y=275
[Layout Debug] select-website-type-true-0 (orchestrator): x=3520, y=-55
[Layout Debug] sbicorp-login-and-fetch (orchestrator): x=2060, y=390
[Layout Debug] login-and-fetch-portfolio (orchestrator): x=2060, y=950
[Layout Debug] parse-portfolio-data (step): x=2560, y=1105
[Layout Debug] end (end): x=4020, y=442

[Layout Debug] === Sub-agent Positioning ===
[Layout Debug] Orchestrator "select-website-type-true-0" at y=-55, has 6 sub-agents
[Layout Debug] Sub-agents will be arranged horizontally at y=225, starting x=2730
[Layout Debug] Orchestrator "sbicorp-login-and-fetch" at y=390, has 6 sub-agents
[Layout Debug] Sub-agents will be arranged horizontally at y=670, starting x=1270
```

These positions are well-distributed across the canvas (x: 80-4020, y: -55-1230), yet nodes appear collapsed.

## Affected Files
- `frontend/src/components/workflow/hooks/usePlanToFlow.ts` - Layout calculation
- `frontend/src/components/workflow/canvas/WorkflowCanvas.tsx` - Position application

## Root Cause Analysis

The issue was identified in the `detectAndResolveCollisions` functioe n within `frontend/src/components/workflow/hooks/usePlanToFlow.ts`. This function runs *after* the main Dagre layout to resolve any remaining overlaps.

The logic in `calculateShift` was inverted. When a collision was detected between a current node `a` and a previously placed node `b`:

1.  **Vertical Overlap:** If `a` was above `b` (`a.top < b.top`), the code was adding a POSITIVE shift to `a`, moving it DOWN into `b` instead of UP away from `b`.
2.  **Horizontal Overlap:** If `a` was to the left of `b` (`a.left < b.left`), the code was adding a POSITIVE shift to `a`, moving it RIGHT into `b` instead of LEFT away from `b`.

This logic effectively acted as a "black hole", pulling any nodes that were close or slightly overlapping (which became more common with reduced node dimensions and spacing) into a single collapsed pile.

## Resolution
Fixed the `calculateShift` logic in `frontend/src/components/workflow/hooks/usePlanToFlow.ts` to correctly calculate the direction of movement:

- If `a` is above `b` (`a.top < b.top`), move `a` UP (negative shift).
- If `a` is below `b` (`a.top >= b.top`), move `a` DOWN (positive shift).
- If `a` is left of `b` (`a.left < b.left`), move `a` LEFT (negative shift).
- If `a` is right of `b` (`a.left >= b.left`), move `a` RIGHT (positive shift).

This ensures that overlapping nodes are pushed apart rather than pulled together.

## Verification
- Verified code logic in `calculateShift` now correctly pushes nodes away from each other based on their relative positions.
- The Dagre layout provides the initial correct distribution, and `detectAndResolveCollisions` now properly fine-tunes it without destroying the layout.

---

# Feature: Vertical Layout Option

## Status
Implemented

## Date
January 22, 2026

## Description
Added the ability to toggle between horizontal (Left-to-Right) and vertical (Top-to-Bottom) layouts in the workflow canvas. The header nodes (Start → Execution Settings → Variables) always remain horizontal, while the workflow steps follow the selected layout direction.

## Layout Behavior

| Direction | Flow | Branch Positioning | Collision Shift | Sub-agents |
|-----------|------|-------------------|-----------------|------------|
| LR (Horizontal) | Left → Right | TRUE above, FALSE below | Prefer vertical | Row below orchestrator |
| TB (Vertical) | Top → Bottom | TRUE left, FALSE right | Prefer horizontal | Column to right of orchestrator |

## Implementation

### Files Modified

1. **`frontend/src/stores/useWorkflowStore.ts`**
   - Added `LayoutDirection` type export (`'LR' | 'TB'`)
   - Added `LAYOUT_DIRECTION_KEY` localStorage constant for persistence
   - Added `layoutDirection` state with localStorage initialization
   - Added `setLayoutDirection` action

2. **`frontend/src/components/workflow/hooks/usePlanToFlow.ts`**
   - Added `layoutDirection` to `UsePlanToFlowOptions` interface
   - Converted `DAGRE_CONFIG` to `getDagreConfig(direction)` function with direction-aware spacing
   - Updated `positionBranchNodes()` for direction-aware branch positioning
   - Updated `detectAndResolveCollisions()` to prefer direction-appropriate shifts
   - Updated sub-agent positioning (horizontal row for LR, vertical column for TB)
   - Added header node positioning to keep Start/Settings/Variables horizontal
   - Added end node positioning based on layout direction

3. **`frontend/src/components/workflow/canvas/WorkflowToolbar.tsx`**
   - Added toggle button with ArrowRight/ArrowDown icons
   - Shows tooltip indicating current direction and switch action

4. **`frontend/src/components/workflow/canvas/WorkflowCanvas.tsx`**
   - Subscribed to `layoutDirection` from store
   - Passed `layoutDirection` to `usePlanToFlow`
   - Added position change detection for layout direction changes
   - Skip saved position restoration when direction changes
   - Updated layout file format to version 1.2 with `layoutDirection` field

### Key Features
- **Persistent preference**: Layout direction is saved to localStorage
- **Header always horizontal**: Start, Execution Settings, and Variables nodes remain in a horizontal row regardless of direction
- **Direction-aware collision resolution**: Prefers shifting nodes perpendicular to the flow direction
- **Layout file support**: Direction is saved with the layout file (version 1.2)

## Usage
Click the arrow icon (→ for horizontal, ↓ for vertical) in the Layout Controls group of the toolbar to toggle between layouts. The canvas will automatically redraw with the new layout direction.

## Bug Fix: Collision Resolution Alignment & Explosion (Jan 22, 2026)
Fixed two critical issues in the layout algorithm:

1. **Partial Node Alignment:** The previous logic "dodged" collisions by shifting nodes perpendicular to the overlap (e.g., if too close vertically, it shifted horizontally). This caused misalignment and chaotic layouts. The fix prioritizes shifting *along the axis of overlap* to preserve column/row alignment (e.g., separating vertically if overlapping vertically).

2. **Layout Explosion:** The collision logic was aggressively separating any nodes that shared an X or Y coordinate range (even if far apart in the other dimension), causing the layout to "explode" and push nodes very far apart or overlapping others. The fix adds distance checks (`vDistance < MIN_SEPARATION` and `hDistance < MIN_SEPARATION`) to only shift nodes that are actually too close.

## Bug Fix: Nested Branch Detachment (Jan 22, 2026)
Fixed an issue where nested branches were detached from their parent nodes during layout calculation. The `positionBranchNodes` function was using the initial (old) position of parent nodes when calculating child positions, ignoring any moves applied to the parent in previous iterations (e.g., if the parent was part of another branch). The fix ensures the up-to-date parent position is used.

## Bug Fix: Header Node Overlap / Layout Restoration (Jan 22, 2026)
Fixed an issue where header nodes (`start`, `execution-settings`, `variables`) appeared vertically stacked ("on top of each other") or overlapping, despite correct auto-layout logic. The root cause was that `WorkflowCanvas` was restoring old/bad positions from saved layout files, overwriting the new enforced horizontal placement.
The fix explicitly excludes header nodes from position restoration in `WorkflowCanvas.tsx`, ensuring they always adhere to the enforced horizontal layout regardless of saved state.

---

# Bug: Header Nodes Not Forced Horizontal in Vertical (TB) Layout

## Status
Fixed

## Date
January 23, 2026

## Description
In Vertical (`TB`) layout mode, the header nodes (`start`, `execution-settings`, `variables`) were incorrectly appearing in a vertical stack instead of a horizontal row. They should always be forced into a horizontal row at the top, regardless of the `layoutDirection`.

## Symptoms
- In `TB` mode, `start`, `execution-settings`, and `variables` nodes were arranged vertically by Dagre.
- The first step of the workflow would overlap with this vertical stack.
- Header nodes appeared overlapping or too close together.
- User reported: "Start -> Execution Mode -> Variables on top of each other... step1 should start from the right of these 3 it also overlaptops on top of them".

## Root Cause Analysis

The issue had multiple contributing factors:

1. **Dagre was positioning header nodes vertically**: In `TB` mode, Dagre treated all nodes (including header nodes) as part of the vertical flow, causing `start`, `execution-settings`, and `variables` to be stacked vertically.

2. **Header nodes were not excluded from Dagre**: The layout algorithm processed header nodes the same as workflow step nodes, allowing Dagre to position them vertically.

3. **Layout restoration was overriding correct positions**: Saved layout files contained old vertical positions for header nodes, and the restoration logic was applying these positions even after the correct horizontal positions were calculated.

4. **Position enforcement happened too late**: Manual header positioning was applied after Dagre ran, but Dagre's vertical layout was still being used as the base, causing conflicts.

## Resolution

The fix involved multiple changes to ensure header nodes always remain horizontal:

### 1. Exclude Header Nodes from Dagre Layout (`usePlanToFlow.ts`)

Header nodes are now explicitly excluded from Dagre processing:
```typescript
// Exclude header nodes - they're positioned manually before Dagre runs
if (node.id === 'start' || node.id === 'execution-settings' || node.id === 'variables') {
  excludedNodeIds.add(node.id)
}
```

### 2. Position Header Nodes Before Dagre Runs (`usePlanToFlow.ts`)

Header nodes are positioned horizontally **before** Dagre processes the remaining nodes:
```typescript
const HEADER_GAP = 100 // Gap between header nodes
const HEADER_START_X = 80 // Starting X position
const HEADER_Y = 80 // Y position

// Position header nodes horizontally BEFORE Dagre
const headerNodesWithPositions = nodes.map(node => {
  if (node.id === 'start') {
    return { ...node, position: { x: HEADER_START_X, y: HEADER_Y } }
  }
  if (node.id === 'execution-settings') {
    const execX = HEADER_START_X + startDims.width + HEADER_GAP
    return { ...node, position: { x: execX, y: HEADER_Y } }
  }
  if (node.id === 'variables') {
    const varsX = HEADER_START_X + startDims.width + HEADER_GAP + execDims.width + HEADER_GAP
    return { ...node, position: { x: varsX, y: HEADER_Y } }
  }
  return node
})
```

### 3. Enforce Header Positions After Dagre (`usePlanToFlow.ts`)

After Dagre runs, header node positions are explicitly enforced to ensure they remain horizontal:
```typescript
// Enforce positions (even though they should already be correct since header nodes are excluded from Dagre)
layoutedResult.nodes[startNodeIndex] = { ...layoutedResult.nodes[startNodeIndex], position: startPos }
layoutedResult.nodes[execSettingsNodeIndex] = { ...layoutedResult.nodes[execSettingsNodeIndex], position: execPos }
layoutedResult.nodes[variablesNodeIndex] = { ...layoutedResult.nodes[variablesNodeIndex], position: varsPos }
```

### 4. Exclude Header Nodes from Layout Restoration (`WorkflowCanvas.tsx`)

Header nodes are excluded from both saving and loading saved layouts:
- In `saveLayout()`: Header node positions are not saved
- In `loadSavedLayout()`: Header node positions are skipped when restoring

### 5. Safety Net: Force Header Positions (`WorkflowCanvas.tsx`)

A `useEffect` hook ensures header nodes maintain correct positions even if something tries to override them:
```typescript
// Ensure header nodes maintain correct positions (safety net)
React.useEffect(() => {
  // Check if any header node position has been overridden and restore it
  if (needsFix) {
    if (execNode) updateNode('execution-settings', { position: execNode.position })
    if (varsNode) updateNode('variables', { position: varsNode.position })
    if (startNode) updateNode('start', { position: startNode.position })
  }
}, [nodes, initialNodes, updateNode])
```

### 6. Workflow Steps Start from Right Edge

Both `TB` and `LR` modes now position the first workflow step starting from the right edge of the header row:
- **TB mode**: First step starts at `headerRowEndX + HEADER_TO_WORKFLOW_GAP` (right edge, below header row)
- **LR mode**: First step starts at `headerRowEndX + HEADER_TO_WORKFLOW_GAP` (right edge, aligned with header row)

### 7. Reduced Node Spacing

Node spacing was reduced for a more compact layout:
- **TB mode**: `nodesep: 120` (horizontal), `ranksep: 300` (vertical)
- **LR mode**: `nodesep: 300` (vertical), `ranksep: 120` (horizontal)

## Affected Files
- `frontend/src/components/workflow/hooks/usePlanToFlow.ts`
  - Excluded header nodes from Dagre processing
  - Added pre-Dagre horizontal positioning of header nodes
  - Added post-Dagre position enforcement
  - Updated workflow step positioning to start from right edge
  - Reduced node spacing values

- `frontend/src/components/workflow/canvas/WorkflowCanvas.tsx`
  - Excluded header nodes from layout save/restore
  - Added safety net useEffect to enforce header positions
  - Restored layout restoration feature (was temporarily disabled during debugging)

## Verification
- ✅ Header nodes (`start`, `execution-settings`, `variables`) always appear horizontally in both `TB` and `LR` modes
- ✅ Header nodes have proper spacing (100px gap) and don't overlap
- ✅ First workflow step starts from the right edge of the header row
- ✅ Layout restoration works correctly without overriding header positions
- ✅ Node spacing is more compact and visually appealing
