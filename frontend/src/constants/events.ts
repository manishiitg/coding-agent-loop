// Shared constants for event management
// This ensures consistent limits across frontend components

// Maximum number of events to process/display in the frontend
// Virtualization handles DOM rendering; the CPU cost is in JS computation (filtering, tree building).
// 1000 top-level events keeps several follow-up turns worth of tools/sub-agent activity
// visible before the UI starts pruning older details.
// Sub-agent child events are capped separately per delegation (MAX_CHILD_EVENTS_PER_DELEGATION).
// Maximum events per session in memory.
// Lower = faster renders + less memory. "Load Older Events" handles history.
// With parallel background agents, 300 was too aggressive for normal chat continuation.
export const MAX_EVENTS_TO_PROCESS = 5000

// Per-delegation child event budget.
// SubAgentHierarchy renders 20 at a time; we keep extra for stats and follow-up continuity.
export const MAX_CHILD_EVENTS_PER_DELEGATION = 300

// Cleanup threshold: start cleanup when events exceed this count
export const CLEANUP_THRESHOLD = 6000
