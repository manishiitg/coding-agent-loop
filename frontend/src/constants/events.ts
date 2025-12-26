// Shared constants for event management
// This ensures consistent limits across frontend components

// Maximum number of events to process/display in the frontend
// This prevents browser freeze when dealing with large event lists
export const MAX_EVENTS_TO_PROCESS = 1000

// Cleanup threshold: start cleanup when events exceed this count
export const CLEANUP_THRESHOLD = MAX_EVENTS_TO_PROCESS + 200

