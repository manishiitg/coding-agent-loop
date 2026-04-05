import type { PlannerFile } from '../services/api-types';
import type { PresetLLMConfig } from '../services/api-types';

export interface CustomPreset {
  id: string;
  label: string;
  query?: string;
  createdAt: number;
  selectedServers?: string[];
  selectedTools?: string[]; // NEW: Array of "server:tool" strings
  selectedSkills?: string[]; // Skill folder names for workflow
  selectedSecrets?: string[]; // Secret IDs for injection (persisted to DB as names)
  selectedGlobalSecretNames?: string[] | null; // null=all global secrets, []=none, [...]=specific
  agentMode?: 'simple' | 'workflow';
  selectedFolder?: PlannerFile; // Single folder
  llmConfig?: PresetLLMConfig; // LLM configuration for this preset
  useCodeExecutionMode?: boolean; // MCP code execution mode
  enableContextSummarization?: boolean; // Enable context summarization
  enableContextEditing?: boolean; // Enable context editing (dynamic context reduction)
  enableBrowserAccess?: boolean; // Deprecated: use browserMode
  browserMode?: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth'; // Browser mode (source of truth)
  camofoxHeaded?: boolean; // Show visible browser window for stealth mode
  employee_id?: string; // Assigned employee ID
}

export interface PredefinedPreset {
  id: string
  label: string
  query?: string
  selectedServers?: string[];
  selectedTools?: string[]; // NEW: Array of "server:tool" strings
  selectedSkills?: string[]; // Skill folder names for workflow
  selectedGlobalSecretNames?: string[] | null; // null=all global secrets, []=none, [...]=specific
  agentMode?: 'simple' | 'workflow';
  selectedFolder?: PlannerFile;
  llmConfig?: PresetLLMConfig; // LLM configuration for this preset
  useCodeExecutionMode?: boolean; // MCP code execution mode
  enableContextSummarization?: boolean; // Enable context summarization
  enableContextEditing?: boolean; // Enable context editing (dynamic context reduction)
  enableBrowserAccess?: boolean; // Deprecated: use browserMode
  browserMode?: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth'; // Browser mode (source of truth)
  camofoxHeaded?: boolean; // Show visible browser window for stealth mode
  employee_id?: string; // Assigned employee ID
}
