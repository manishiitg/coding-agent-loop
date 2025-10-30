import type { PlannerFile } from '../services/api-types';
import type { PresetLLMConfig } from '../services/api-types';

export interface CustomPreset {
  id: string;
  label: string;
  query: string;
  createdAt: number;
  selectedServers?: string[];
  selectedTools?: string[]; // NEW: Array of "server:tool" strings
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
  selectedFolder?: PlannerFile; // Single folder
  llmConfig?: PresetLLMConfig; // LLM configuration for this preset
}

export interface PredefinedPreset {
  id: string
  label: string
  query: string
  selectedServers?: string[];
  selectedTools?: string[]; // NEW: Array of "server:tool" strings
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
  selectedFolder?: PlannerFile;
  llmConfig?: PresetLLMConfig; // LLM configuration for this preset
}
