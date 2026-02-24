// Shared constants for event management
// This ensures consistent limits across frontend components

// Maximum number of events to process/display in the frontend
// Virtualization handles DOM rendering; the CPU cost is in JS computation (filtering, tree building).
// 500 events is generous — "Load Older Events" button handles history access.
export const MAX_EVENTS_TO_PROCESS = 500

// Cleanup threshold: start cleanup when events exceed this count
// Keep buffer small to avoid memory buildup
export const CLEANUP_THRESHOLD = 600

