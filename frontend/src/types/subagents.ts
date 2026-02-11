export interface SubAgentFrontmatter {
  name: string;
  description: string;
  default_reasoning_level?: string;
  default_tool_mode?: string;
  skills?: string;  // Comma-separated skill folder names
  servers?: string; // Comma-separated MCP server names
}

export interface SubAgent {
  frontmatter: SubAgentFrontmatter;
  content: string;
  folder_name: string;
  file_path: string;
}

export interface UpdateSubAgentRequest {
  content: string; // Full SUBAGENT.md content (frontmatter + body)
}

export interface ListSubAgentsResponse {
  subagents: SubAgent[];
  total: number;
}
