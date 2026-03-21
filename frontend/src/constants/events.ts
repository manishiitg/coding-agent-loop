// Shared constants for event management
// This ensures consistent limits across frontend components

// Maximum number of events to process/display in the frontend
// Virtualization handles DOM rendering; the CPU cost is in JS computation (filtering, tree building).
// 500 top-level events is generous — "Load Older Events" button handles history access.
// Sub-agent child events are capped separately per delegation (MAX_CHILD_EVENTS_PER_DELEGATION).
// Maximum events per session in memory.
// Lower = faster renders + less memory. "Load Older Events" handles history.
// With parallel background agents, each generates ~20-50 events, so 300 covers 5+ agents.
export const MAX_EVENTS_TO_PROCESS = 300

// Per-delegation child event budget.
// SubAgentHierarchy renders 20 at a time; we keep extra for stats.
export const MAX_CHILD_EVENTS_PER_DELEGATION = 30

// Cleanup threshold: start cleanup when events exceed this count
export const CLEANUP_THRESHOLD = 350

