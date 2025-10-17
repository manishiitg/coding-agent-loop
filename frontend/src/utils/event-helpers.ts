/**
 * Utility functions for event display formatting
 */

/**
 * Formats a JSON string with proper indentation
 * @param jsonString - The JSON string to format
 * @returns Formatted JSON string or the original string if not valid JSON
 */
export const formatJSON = (jsonString: string): string => {
  try {
    const parsed = JSON.parse(jsonString)
    return JSON.stringify(parsed, null, 2)
  } catch {
    // If it's not valid JSON, return the original string
    return jsonString
  }
}

/**
 * Checks if a string is valid JSON
 * @param str - The string to check
 * @returns True if the string is valid JSON
 */
export const isValidJSON = (str: string): boolean => {
  try {
    JSON.parse(str)
    return true
  } catch {
    return false
  }
}

/**
 * Safely parses JSON and returns the parsed object or null
 * @param jsonString - The JSON string to parse
 * @returns Parsed object or null if invalid JSON
 */
export const safeParseJSON = (jsonString: string): unknown => {
  try {
    return JSON.parse(jsonString)
  } catch {
    return null
  }
}
