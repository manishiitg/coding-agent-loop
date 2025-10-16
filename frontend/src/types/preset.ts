import type { PlannerFile } from '../services/api-types';

export interface CustomPreset {
  id: string;
  label: string;
  query: string;
  createdAt: number;
  selectedServers?: string[];
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow';
  selectedFolder?: PlannerFile; // Single folder
}

export interface PredefinedPreset {
  id: string
  label: string
  query: string
  selectedServers?: string[]
  agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow'
  selectedFolder?: PlannerFile
}
