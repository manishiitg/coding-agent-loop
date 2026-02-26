// Shared constants for event management
// This ensures consistent limits across frontend components

// Maximum number of events to process/display in the frontend
// Virtualization handles DOM rendering; the CPU cost is in JS computation (filtering, tree building).
// 500 top-level events is generous — "Load Older Events" button handles history access.
// Sub-agent child events are capped separately per delegation (MAX_CHILD_EVENTS_PER_DELEGATION).
export const MAX_EVENTS_TO_PROCESS = 500

// Per-delegation child event budget.
// SubAgentHierarchy renders 20 at a time; we keep extra in memory for delegation stats (token counts, tool calls).
export const MAX_CHILD_EVENTS_PER_DELEGATION = 40

// Cleanup threshold: start cleanup when events exceed this count
// Keep buffer small to avoid memory buildup
export const CLEANUP_THRESHOLD = 600

