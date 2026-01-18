// Shared constants for event management
// This ensures consistent limits across frontend components

// Maximum number of events to process/display in the frontend
// This prevents browser freeze when dealing with large event lists
// Increased to 1000 since we are using virtualization (Virtuoso)
export const MAX_EVENTS_TO_PROCESS = 1000

// Cleanup threshold: start cleanup when events exceed this count
// Keep buffer small to avoid memory buildup
export const CLEANUP_THRESHOLD = 1200

