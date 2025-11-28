import type { PlannerFile } from '../services/api-types';
import type { PresetLLMConfig } from '../services/api-types';

export interface CustomPreset {
  id: string;
  label: string;
  query: string;
  createdAt: number;
  selectedServers?: string[];
  selectedTools?: string[]; // NEW: Array of "server:tool" strings
  agentMode?: 'simple' | 'workflow';
  selectedFolder?: PlannerFile; // Single folder
  llmConfig?: PresetLLMConfig; // LLM configuration for this preset
  useCodeExecutionMode?: boolean; // MCP code execution mode
}

export interface PredefinedPreset {
  id: string
  label: string
  query: string
  selectedServers?: string[];
  selectedTools?: string[]; // NEW: Array of "server:tool" strings
  agentMode?: 'simple' | 'workflow';
  selectedFolder?: PlannerFile;
  llmConfig?: PresetLLMConfig; // LLM configuration for this preset
  useCodeExecutionMode?: boolean; // MCP code execution mode
}
