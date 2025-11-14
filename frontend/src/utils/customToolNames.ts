// Custom tool names by category
// These should match the tool names from the backend CreateWorkspaceToolExecutors and CreateHumanToolExecutors

// Workspace tool sub-categories (FRONTEND ONLY - for UI organization)
// The backend only sees individual tool names, not sub-categories
export const BASIC_WORKSPACE_TOOLS = [
  'list_workspace_files',
  'read_workspace_file',
  'update_workspace_file',
  'delete_workspace_file',
  'move_workspace_file',
  'diff_patch_workspace_file',
] as const;

export const ADVANCED_WORKSPACE_TOOLS = [
  'regex_search_workspace_files',
  'semantic_search_workspace_files',
  'sync_workspace_to_github',
  'get_workspace_github_status',
] as const;

export const PLUS_TOOLS = [
  'execute_shell_command',
  'read_image',
] as const;

// All workspace tools (combined)
export const WORKSPACE_TOOLS = [
  ...BASIC_WORKSPACE_TOOLS,
  ...ADVANCED_WORKSPACE_TOOLS,
  ...PLUS_TOOLS,
] as const;

export const HUMAN_TOOLS = [
  'human_feedback',
] as const;

export type WorkspaceToolName = typeof WORKSPACE_TOOLS[number];
export type HumanToolName = typeof HUMAN_TOOLS[number];
export type CustomToolName = WorkspaceToolName | HumanToolName;

// Helper to get all tools for a category
export function getToolsByCategory(category: string): string[] {
  switch (category) {
    case 'workspace_tools':
      return [...WORKSPACE_TOOLS];
    case 'human_tools':
      return [...HUMAN_TOOLS];
    default:
      return [];
  }
}

// Helper to get tools for a workspace sub-category
export function getToolsByWorkspaceSubCategory(subCategory: string): string[] {
  switch (subCategory) {
    case 'basic_workspace':
      return [...BASIC_WORKSPACE_TOOLS];
    case 'advanced_workspace':
      return [...ADVANCED_WORKSPACE_TOOLS];
    case 'plus_tools':
      return [...PLUS_TOOLS];
    default:
      return [];
  }
}

// Helper to get all available custom tools
export function getAllCustomTools(): string[] {
  return [...WORKSPACE_TOOLS, ...HUMAN_TOOLS];
}

