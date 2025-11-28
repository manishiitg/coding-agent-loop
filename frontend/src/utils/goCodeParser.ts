/**
 * Go Code Parser for Workspace Tools Detection
 * 
 * Parses Go code from write_code tool calls to detect workspace tool usage
 * and extract file paths for visual features (highlighting, auto-scroll, etc.)
 */

export interface WorkspaceToolCall {
  toolName: string
  filepath: string
  operation: 'read' | 'update' | 'delete' | 'unknown'
}

/**
 * Maps Go function names to workspace tool names and operations
 * Simplified to only detect the 3 core workspace tools
 */
const WORKSPACE_TOOL_MAPPING: Record<string, { toolName: string; operation: WorkspaceToolCall['operation'] }> = {
  'ReadWorkspaceFile': { toolName: 'read_workspace_file', operation: 'read' },
  'UpdateWorkspaceFile': { toolName: 'update_workspace_file', operation: 'update' },
  'DeleteWorkspaceFile': { toolName: 'delete_workspace_file', operation: 'delete' },
}

/**
 * Extracts file path from a struct literal parameter
 * Handles both single-line and multi-line struct literals
 */
function extractFilepathFromStruct(code: string, startIndex: number): string | null {
  // Find the opening brace
  const openBraceIndex = code.indexOf('{', startIndex)
  if (openBraceIndex === -1) {
    // No struct literal, might be a variable reference
    // Try to find filepath in variable assignments before the function call
    const varPattern = /(?:filepath|filePath|file_path)\s*[:=]\s*["'`]([^"'`]+)["'`]/i
    const varMatch = code.substring(Math.max(0, startIndex - 300), startIndex).match(varPattern)
    if (varMatch && varMatch[1]) {
      return varMatch[1]
    }
    return null
  }

  // Extract struct content by counting braces (handles nested structs)
  let braceCount = 1
  let endIndex = openBraceIndex + 1
  
  for (let i = openBraceIndex + 1; i < code.length && braceCount > 0; i++) {
    if (code[i] === '{') braceCount++
    if (code[i] === '}') braceCount--
    if (braceCount === 0) {
      endIndex = i
      break
    }
  }
  
  const structContent = code.substring(openBraceIndex, endIndex + 1)

  // Look for Filepath field (case-insensitive, handles both "Filepath:" and "Filepath")
  // Also handles backticks for raw strings
  const filepathPattern = /Filepath\s*:\s*["'`]([^"'`]+)["'`]/i
  const match = structContent.match(filepathPattern)
  
  if (match && match[1]) {
    return match[1]
  }

  // Also try to find filepath in variable assignments before the function call
  // Pattern: filepath := "..." or filepath = "..."
  const varPattern = /(?:filepath|filePath|file_path)\s*[:=]\s*["'`]([^"'`]+)["'`]/i
  const contextBefore = code.substring(Math.max(0, startIndex - 300), startIndex)
  const varMatch = contextBefore.match(varPattern)
  if (varMatch && varMatch[1]) {
    return varMatch[1]
  }

  return null
}

/**
 * Parses Go code to find workspace tool calls and extract file paths
 */
export function parseWorkspaceToolCalls(code: string): WorkspaceToolCall[] {
  if (!code || typeof code !== 'string') {
    return []
  }

  const results: WorkspaceToolCall[] = []
  
  // Pattern to match workspace_tools.FunctionName(...)
  // Handles various whitespace and formatting
  // Also handles cases where package is aliased (e.g., wt "workspace_tools")
  const functionCallPattern = /(?:workspace_tools|wt)\.(\w+)\s*\(/g
  
  let match: RegExpExecArray | null
  while ((match = functionCallPattern.exec(code)) !== null) {
    const functionName = match[1]
    const callStartIndex = match.index + match[0].length
    
    // Check if this is a known workspace tool
    const toolInfo = WORKSPACE_TOOL_MAPPING[functionName]
    if (!toolInfo) {
      continue
    }

    // Extract filepath from the function call
    const filepath = extractFilepathFromStruct(code, callStartIndex)
    
    if (filepath) {
      results.push({
        toolName: toolInfo.toolName,
        filepath: filepath,
        operation: toolInfo.operation
      })
    }
  }

  // Also check for indirect calls via variables
  // Pattern: result := workspace_tools.ReadWorkspaceFile(params)
  // This handles cases where the function call is on a new line
  const indirectPattern = /(?:workspace_tools|wt)\.(\w+)\s*\(\s*(\w+)\s*\)/g
  let indirectMatch: RegExpExecArray | null
  while ((indirectMatch = indirectPattern.exec(code)) !== null) {
    const functionName = indirectMatch[1]
    const paramsVarName = indirectMatch[2]
    const toolInfo = WORKSPACE_TOOL_MAPPING[functionName]
    
    if (toolInfo) {
      // Find where the params variable is defined
      // Handle both workspace_tools and aliased package names
      // Use a more robust pattern that handles nested braces
      const paramsPattern = new RegExp(`${paramsVarName}\\s*[:=]\\s*(?:workspace_tools|wt)\\.\\w+Params\\s*{`, 's')
      const paramsMatch = code.match(paramsPattern)
      
      if (paramsMatch) {
        // Find the matching closing brace by counting braces
        const startIndex = paramsMatch.index! + paramsMatch[0].length
        let braceCount = 1
        let endIndex = startIndex
        
        for (let i = startIndex; i < code.length && braceCount > 0; i++) {
          if (code[i] === '{') braceCount++
          if (code[i] === '}') braceCount--
          if (braceCount === 0) {
            endIndex = i
            break
          }
        }
        
        const structContent = code.substring(startIndex, endIndex)
        
        const filepathMatch = structContent.match(/Filepath\s*:\s*["'`]([^"'`]+)["'`]/i)
        if (filepathMatch && filepathMatch[1]) {
          // Avoid duplicates
          const exists = results.some(r => r.toolName === toolInfo.toolName && r.filepath === filepathMatch[1])
          if (!exists) {
            results.push({
              toolName: toolInfo.toolName,
              filepath: filepathMatch[1],
              operation: toolInfo.operation
            })
          }
        }
      }
    }
  }

  return results
}

/**
 * Selects the most relevant file to highlight from multiple workspace tool calls
 * Priority: update > delete > read
 * This ensures we highlight the most important operation when multiple files are involved
 */
export function selectPrimaryFile(workspaceCalls: WorkspaceToolCall[]): WorkspaceToolCall | null {
  if (workspaceCalls.length === 0) {
    return null
  }

  if (workspaceCalls.length === 1) {
    return workspaceCalls[0]
  }

  // Priority order: higher number = higher priority
  const operationPriority: Record<WorkspaceToolCall['operation'], number> = {
    'update': 3,
    'delete': 2,
    'read': 1,
    'unknown': 0,
  }

  // Find the call with highest priority
  let primaryCall = workspaceCalls[0]
  let highestPriority = operationPriority[primaryCall.operation] || 0

  for (const call of workspaceCalls) {
    const priority = operationPriority[call.operation] || 0
    if (priority > highestPriority) {
      highestPriority = priority
      primaryCall = call
    }
  }

  return primaryCall
}

/**
 * Checks if Go code contains any workspace tool calls
 */
export function hasWorkspaceToolCalls(code: string): boolean {
  if (!code || typeof code !== 'string') {
    return false
  }
  
  // Quick check: does the code import or use workspace_tools?
  if (!code.includes('workspace_tools') && !code.includes('wt.')) {
    return false
  }
  
  // Check for any known workspace tool function names
  const toolNames = Object.keys(WORKSPACE_TOOL_MAPPING)
  return toolNames.some(toolName => {
    // Check both workspace_tools and potential alias (wt)
    const pattern1 = new RegExp(`workspace_tools\\.${toolName}\\s*\\(`, 'i')
    const pattern2 = new RegExp(`wt\\.${toolName}\\s*\\(`, 'i')
    return pattern1.test(code) || pattern2.test(code)
  })
}

