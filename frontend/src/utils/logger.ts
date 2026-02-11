/**
 * Configurable logger utility
 *
 * Provides structured logging with categories and levels that can be
 * enabled/disabled in production. Replaces heavy console.log debug statements.
 *
 * Usage:
 *   import { logger } from '../utils/logger'
 *   logger.debug('ChatStore', 'Tab created', { tabId, sessionId })
 *   logger.warn('WorkflowLayout', 'Duplicate tab detected', { tabId })
 */

// Log levels in order of severity
export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

// Categories for different parts of the application
export type LogCategory =
  | 'ChatArea'
  | 'ChatStore'
  | 'TabStore'
  | 'EventStore'
  | 'SessionStore'
  | 'WorkflowLayout'
  | 'ChatTabs'
  | 'WorkflowChatTabs'
  | 'Polling'
  | 'Reconnection'
  | 'Memory'
  | 'General'

interface LoggerConfig {
  // Global enable/disable
  enabled: boolean
  // Minimum log level to display
  minLevel: LogLevel
  // Categories to enable (empty = all enabled)
  enabledCategories: LogCategory[]
  // Show timestamps in logs
  showTimestamp: boolean
}

// Default configuration - can be overridden
const defaultConfig: LoggerConfig = {
  enabled: import.meta.env.DEV, // Only enabled in development by default
  minLevel: 'debug',
  enabledCategories: [], // Empty means all categories enabled
  showTimestamp: false
}

// Current configuration (mutable for runtime changes)
let config: LoggerConfig = { ...defaultConfig }

// Level priority for filtering
const levelPriority: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3
}

/**
 * Check if a log should be displayed based on current config
 */
function shouldLog(level: LogLevel, category: LogCategory): boolean {
  if (!config.enabled) return false

  // Check level
  if (levelPriority[level] < levelPriority[config.minLevel]) return false

  // Check category (empty array means all enabled)
  if (config.enabledCategories.length > 0 && !config.enabledCategories.includes(category)) {
    return false
  }

  return true
}

/**
 * Format the log prefix
 */
function formatPrefix(level: LogLevel, category: LogCategory): string {
  const timestamp = config.showTimestamp ? `[${new Date().toISOString()}] ` : ''
  return `${timestamp}[${category}]`
}

/**
 * Main logger object with methods for each log level
 */
export const logger = {
  /**
   * Debug level - verbose information for debugging
   */
  debug(category: LogCategory, message: string, data?: unknown): void {
    if (!shouldLog('debug', category)) return
    const prefix = formatPrefix('debug', category)
    if (data !== undefined) {
      console.log(prefix, message, data)
    } else {
      console.log(prefix, message)
    }
  },

  /**
   * Info level - general information
   */
  info(category: LogCategory, message: string, data?: unknown): void {
    if (!shouldLog('info', category)) return
    const prefix = formatPrefix('info', category)
    if (data !== undefined) {
      console.info(prefix, message, data)
    } else {
      console.info(prefix, message)
    }
  },

  /**
   * Warn level - warnings that don't break functionality
   */
  warn(category: LogCategory, message: string, data?: unknown): void {
    if (!shouldLog('warn', category)) return
    const prefix = formatPrefix('warn', category)
    if (data !== undefined) {
      console.warn(prefix, message, data)
    } else {
      console.warn(prefix, message)
    }
  },

  /**
   * Error level - errors that may affect functionality
   */
  error(category: LogCategory, message: string, data?: unknown): void {
    if (!shouldLog('error', category)) return
    const prefix = formatPrefix('error', category)
    if (data !== undefined) {
      console.error(prefix, message, data)
    } else {
      console.error(prefix, message)
    }
  },

  /**
   * Configure the logger at runtime
   */
  configure(newConfig: Partial<LoggerConfig>): void {
    config = { ...config, ...newConfig }
  },

  /**
   * Enable logging
   */
  enable(): void {
    config.enabled = true
  },

  /**
   * Disable logging
   */
  disable(): void {
    config.enabled = false
  },

  /**
   * Set minimum log level
   */
  setMinLevel(level: LogLevel): void {
    config.minLevel = level
  },

  /**
   * Enable specific categories only
   */
  setCategories(categories: LogCategory[]): void {
    config.enabledCategories = categories
  },

  /**
   * Get current configuration (for debugging)
   */
  getConfig(): LoggerConfig {
    return { ...config }
  },

  /**
   * Reset to default configuration
   */
  reset(): void {
    config = { ...defaultConfig }
  }
}

// Expose logger configuration on window for debugging in browser console
if (typeof window !== 'undefined') {
  (window as unknown as { __logger: typeof logger }).__logger = logger
}
