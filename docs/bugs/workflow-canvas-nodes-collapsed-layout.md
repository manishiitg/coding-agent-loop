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

The issue was identified in the `detectAndResolveCollisions` function within `frontend/src/components/workflow/hooks/usePlanToFlow.ts`. This function runs *after* the main Dagre layout to resolve any remaining overlaps.

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