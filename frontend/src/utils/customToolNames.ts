// Custom tool names by category
// These should match the tool names from the backend CreateWorkspaceBasicToolExecutors, CreateWorkspaceGitToolExecutors, and CreateWorkspaceAdvancedToolExecutors

// workspace_basic: 8 core file/folder management and search tools
// Maps to backend "workspace_basic" category
export const WORKSPACE_BASIC_TOOLS = [
  'list_workspace_files',
  'read_workspace_file',
  'update_workspace_file',
  'regex_search_workspace_files',
  'semantic_search_workspace_files',
  'glob_discover_workspace_files',
  'delete_workspace_file',
  'move_workspace_file',
] as const;

// workspace_git: 2 GitHub sync tools
// Maps to backend "workspace_git" category
export const WORKSPACE_GIT_TOOLS = [
  'sync_workspace_to_github',
  'get_workspace_github_status',
] as const;

// workspace_advanced: 4 advanced tools (shell, image, PDF, diff patch)
// Maps to backend "workspace_advanced" category
export const WORKSPACE_ADVANCED_TOOLS = [
  'execute_shell_command',
  'read_image',
  'read_pdf',
  'diff_patch_workspace_file',
] as const;

// workspace_browser: 1 browser automation tool
// Maps to backend "workspace_browser" category
export const WORKSPACE_BROWSER_TOOLS = [
  'agent_browser',
] as const;

// All workspace tools (combined) - for backward compatibility with "workspace_tools" category
export const WORKSPACE_TOOLS = [
  ...WORKSPACE_BASIC_TOOLS,
  ...WORKSPACE_GIT_TOOLS,
  ...WORKSPACE_ADVANCED_TOOLS,
  ...WORKSPACE_BROWSER_TOOLS,
] as const;

export const HUMAN_TOOLS = [
  'human_feedback',
] as const;

export type WorkspaceBasicToolName = typeof WORKSPACE_BASIC_TOOLS[number];
export type WorkspaceGitToolName = typeof WORKSPACE_GIT_TOOLS[number];
export type WorkspaceAdvancedToolName = typeof WORKSPACE_ADVANCED_TOOLS[number];
export type WorkspaceBrowserToolName = typeof WORKSPACE_BROWSER_TOOLS[number];
export type WorkspaceToolName = typeof WORKSPACE_TOOLS[number];
export type HumanToolName = typeof HUMAN_TOOLS[number];
export type CustomToolName = WorkspaceToolName | HumanToolName;

// Helper to get all tools for a category
// Supports: workspace_tools (all), workspace_basic (8), workspace_git (2), workspace_advanced (4), human_tools
export function getToolsByCategory(category: string, capabilities?: { semantic_search_enabled?: boolean, github_sync_enabled?: boolean }): string[] {
  const isSemanticEnabled = capabilities?.semantic_search_enabled ?? true;
  const isGitEnabled = capabilities?.github_sync_enabled ?? true;

  const filterTools = (tools: readonly string[]) => {
    return tools.filter(tool => {
      if (tool === 'semantic_search_workspace_files' && !isSemanticEnabled) return false;
      if (WORKSPACE_GIT_TOOLS.includes(tool as WorkspaceGitToolName) && !isGitEnabled) return false;
      return true;
    });
  };

  switch (category) {
    case 'workspace_tools':
      // Backward compatible - returns all workspace tools
      return filterTools(WORKSPACE_TOOLS);
    case 'workspace_basic':
      // Basic file/folder operations (8 tools)
      return filterTools(WORKSPACE_BASIC_TOOLS);
    case 'workspace_git':
      // GitHub sync tools (2 tools)
      return isGitEnabled ? [...WORKSPACE_GIT_TOOLS] : [];
    case 'workspace_advanced':
      // Advanced tools (4 tools: shell + image + PDF + diff patch)
      return [...WORKSPACE_ADVANCED_TOOLS];
    case 'workspace_browser':
      // Browser automation tool (1 tool)
      return [...WORKSPACE_BROWSER_TOOLS];
    case 'human_tools':
      return [...HUMAN_TOOLS];
    default:
      return [];
  }
}

// Helper to get all available custom tools
export function getAllCustomTools(capabilities?: { semantic_search_enabled?: boolean, github_sync_enabled?: boolean }): string[] {
  const isSemanticEnabled = capabilities?.semantic_search_enabled ?? true;
  const isGitEnabled = capabilities?.github_sync_enabled ?? true;

  return [
    ...getToolsByCategory('workspace_basic', { semantic_search_enabled: isSemanticEnabled, github_sync_enabled: isGitEnabled }),
    ...(isGitEnabled ? WORKSPACE_GIT_TOOLS : []),
    ...WORKSPACE_ADVANCED_TOOLS,
    ...WORKSPACE_BROWSER_TOOLS,
    ...HUMAN_TOOLS
  ];
}

// Helper to get the category for a specific tool name
export function getCategoryForTool(toolName: string): string | null {
  if (WORKSPACE_BASIC_TOOLS.includes(toolName as WorkspaceBasicToolName)) {
    return 'workspace_basic';
  }
  if (WORKSPACE_GIT_TOOLS.includes(toolName as WorkspaceGitToolName)) {
    return 'workspace_git';
  }
  if (WORKSPACE_ADVANCED_TOOLS.includes(toolName as WorkspaceAdvancedToolName)) {
    return 'workspace_advanced';
  }
  if (WORKSPACE_BROWSER_TOOLS.includes(toolName as WorkspaceBrowserToolName)) {
    return 'workspace_browser';
  }
  if (HUMAN_TOOLS.includes(toolName as HumanToolName)) {
    return 'human_tools';
  }
  return null;
}
