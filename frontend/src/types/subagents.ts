export interface SubAgentFrontmatter {
  name: string;
  description: string;
  default_reasoning_level?: string;
  default_tool_mode?: string;
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

export interface ValidateSubAgentRequest {
  github_url: string;
  github_token?: string;
}

export interface ValidateSubAgentResponse {
  valid: boolean;
  frontmatter?: SubAgentFrontmatter;
  error?: string;
  files?: string[];
  exists: boolean;
}

export interface ImportSubAgentRequest {
  github_url: string;
  github_token?: string;
}

export interface ImportSubAgentResponse {
  success: boolean;
  subagent_name?: string;
  error?: string;
}
