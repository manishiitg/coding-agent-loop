import React from 'react';
import type { OrchestratorAgentStartEvent } from '../../../generated/events';
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer';
import { useExpandable } from '../useExpandable';
import { Plus, Minus } from 'lucide-react';
import { useLLMStore } from '../../../stores'
import { getModelDisplayName } from '../../../utils/llmDisplay'
import { isEvaluationAgentEvent } from './eventDisplayUtils'

function titleCaseIdentifier(value: string): string {
  return value
    .replace(/[-_]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/\b\w+/g, word => {
      const lower = word.toLowerCase()
      if (lower === 'kb') return 'KB'
      if (lower === 'id') return 'ID'
      if (lower === 'api') return 'API'
      return `${lower.charAt(0).toUpperCase()}${lower.slice(1)}`
    })
}

function friendlyAgentName(event: OrchestratorAgentStartEvent): string {
  const rawName = event.agent_name || ''
  const stepTitle = typeof event.input_data?.step_title === 'string' ? event.input_data.step_title : ''
  const title = stepTitle || rawName
  const executionMarker = title.lastIndexOf('execution-')
  if (executionMarker >= 0) {
    return titleCaseIdentifier(title.slice(executionMarker + 'execution-'.length))
  }
  return titleCaseIdentifier(title.replace(/^step-\d+-(sub-)?/, '')) || 'Work item'
}

function formatWorkshopIteration(iteration?: number, inputIteration?: string): string | null {
  if (inputIteration && inputIteration.trim() !== '') return inputIteration
  if (typeof iteration === 'number' && iteration >= 0) return `iteration-${iteration}`
  return null
}

function isMessageSequenceItemEvent(event: OrchestratorAgentStartEvent): boolean {
  return event.metadata?.message_sequence_item === true ||
    event.metadata?.message_sequence_item === 'true' ||
    event.agent_name?.startsWith('message-sequence-') === true
}

function isWorkflowStepExecutionEvent(event: OrchestratorAgentStartEvent): boolean {
  const agentName = event.agent_name || ''
  return agentName.startsWith('step-') && agentName.includes('-execution-')
}

function isSequenceWorkEvent(event: OrchestratorAgentStartEvent): boolean {
  const agentName = (event.agent_name || '').toLowerCase()
  return isWorkflowStepExecutionEvent(event) && agentName.includes('sequence')
}

interface OrchestratorAgentStartEventDisplayProps {
  event: OrchestratorAgentStartEvent;
  isCollapsed?: boolean;
  eventCount?: number;
  onToggleCollapse?: () => void;
  toolCallCount?: number;
  latestToolLabel?: string;
}

export const OrchestratorAgentStartEventDisplay: React.FC<OrchestratorAgentStartEventDisplayProps> = ({
  event,
  isCollapsed,
  eventCount,
  toolCallCount,
  latestToolLabel,
}) => {
  const { isExpanded: isInputsExpanded, toggle } = useExpandable(true)
  const savedLLMs = useLLMStore(state => state.savedLLMs)
  const availableLLMs = useLLMStore(state => state.availableLLMs)
  const modelMetadataCatalog = useLLMStore(state => state.modelMetadataCatalog)
  const modeFlags = event as OrchestratorAgentStartEvent & {
    use_code_execution_mode?: boolean
    use_learn_code_mode?: boolean
  }
  const useScriptedMode =
    modeFlags.use_learn_code_mode ||
    event.input_data?.IsScriptedMode === 'true' ||
    event.input_data?.is_learn_code_mode === 'true' ||
    event.input_data?.workshop_mode === 'learn_code'

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  const hasInputData = event.input_data && Object.keys(event.input_data).length > 0;

  const agentType = (event as unknown as { agent_type?: string })?.agent_type
  const isEvaluationAgent = isEvaluationAgentEvent(event)
  const isMessageSequenceItem = isMessageSequenceItemEvent(event)
  const isSequenceWork = isSequenceWorkEvent(event)
  const isWorkflowStepExecution = isWorkflowStepExecutionEvent(event)
  const isWorkshopStep = agentType?.startsWith('workshop-')
  const isWorkshopStepExecution = agentType === 'workshop-step-execution'
  const workshopGroup = event.input_data?.group_name
  const workshopIteration = formatWorkshopIteration(event.iteration, event.input_data?.iteration)
  const workshopMeta = (workshopGroup || workshopIteration)
    ? [
        workshopGroup ? `Group: ${workshopGroup}` : null,
        workshopIteration ? `Iteration: ${workshopIteration}` : null,
      ].filter(Boolean).join(' | ')
    : ''

  const getLabel = () => {
    if (isMessageSequenceItem) return 'Sequence Item'
    if (isSequenceWork) return 'Sequence Work'
    if (isWorkflowStepExecution) return 'Step'
    if (isEvaluationAgent && agentType === 'evaluation_scoring') return 'Evaluation Scoring'
    if (isEvaluationAgent && (agentType === 'todo_planner_execution' || agentType === 'generic_execution' || agentType === 'workshop-step-execution')) return 'Evaluation Step'
    if (isEvaluationAgent) return 'Evaluation Agent'
    if (agentType === 'workshop-step-execution') return 'Step Execution'
    if (agentType === 'workshop-step-learning') return 'Learning Agent'
    if (agentType === 'workshop-step-debug') return 'Optimization Agent'
    if (agentType === 'workshop-background-task') return 'Background Task'
    if (agentType === 'todo_planner_execution') return 'Sub-Agent'
    if (agentType === 'generic_execution') return 'Generic Agent'
    if (agentType === 'todo_task_orchestrator') return 'Todo Orchestrator'
    if (agentType === 'planning') return 'Planning Agent'
    if (agentType === 'execution') return 'Execution Agent'
    if (agentType === 'validation') return 'Validation Agent'
    if (agentType === 'organizer') return 'Organizer Agent'
    if (agentType === 'plan_breakdown') return 'Plan Breakdown Agent'
    return 'Agent'
  }

  const getTreeRoleLabel = () => {
    if (agentType === 'todo_task_orchestrator') return 'Orchestrator'
    if (isWorkflowStepExecution || isWorkshopStepExecution || isEvaluationAgent) return 'Automation step'
    if (agentType === 'todo_planner_execution' || agentType === 'generic_execution') return 'Sub-agent'
    return getLabel()
  }

  const getAgentIcon = () => {
    if (isMessageSequenceItem) return '👤'
    if (isWorkflowStepExecution) return '▶️'
    if (isEvaluationAgent) return '🧪'
    if (agentType === 'workshop-step-execution') return '▶️'
    if (agentType === 'workshop-step-learning') return '📚'
    if (agentType === 'workshop-step-debug') return '🔧'
    if (agentType === 'workshop-background-task') return '⏳'
    if (agentType === 'todo_planner_execution') return '⚡'
    if (agentType === 'generic_execution') return '⚡'
    if (agentType === 'todo_task_orchestrator') return '📋'
    if (agentType === 'plan_breakdown') return '🔍'
    if (agentType === 'planning') return '📋'
    if (agentType === 'execution') return '⚡'
    if (agentType === 'validation') return '✅'
    if (agentType === 'organizer') return '🗂️'
    return '🤖'
  }

  const getAgentColor = () => {
    if (isMessageSequenceItem) return 'slate'
    if (isWorkflowStepExecution) return 'cyan'
    if (isEvaluationAgent) return 'blue'
    if (agentType === 'workshop-step-execution') return 'cyan'
    if (agentType === 'workshop-step-learning') return 'amber'
    if (agentType === 'workshop-step-debug') return 'orange'
    if (agentType === 'workshop-background-task') return 'slate'
    if (agentType === 'todo_planner_execution') return 'purple'
    if (agentType === 'generic_execution') return 'purple'
    if (agentType === 'todo_task_orchestrator') return 'indigo'
    if (agentType === 'plan_breakdown') return 'emerald'
    if (agentType === 'planning') return 'blue'
    if (agentType === 'execution') return 'purple'
    if (agentType === 'validation') return 'emerald'
    if (agentType === 'organizer') return 'orange'
    return 'yellow'
  }

  const agentColor = getAgentColor();
  const agentIcon = getAgentIcon();
  
  const getColorClasses = (color: string) => {
    switch (color) {
      case 'emerald':
        return {
          bg: 'bg-emerald-50 dark:bg-emerald-900/20',
          border: 'border-emerald-200 dark:border-emerald-800',
          text: 'text-emerald-700 dark:text-emerald-300',
          textSecondary: 'text-emerald-600 dark:text-emerald-400',
          hover: 'hover:text-emerald-800 dark:hover:text-emerald-200'
        };
      case 'blue':
        return {
          bg: 'bg-blue-50 dark:bg-blue-900/20',
          border: 'border-blue-200 dark:border-blue-800',
          text: 'text-blue-700 dark:text-blue-300',
          textSecondary: 'text-blue-600 dark:text-blue-400',
          hover: 'hover:text-blue-800 dark:hover:text-blue-200'
        };
      case 'purple':
        return {
          bg: 'bg-purple-50 dark:bg-purple-900/20',
          border: 'border-purple-200 dark:border-purple-800',
          text: 'text-purple-700 dark:text-purple-300',
          textSecondary: 'text-purple-600 dark:text-purple-400',
          hover: 'hover:text-purple-800 dark:hover:text-purple-200'
        };
      case 'orange':
        return {
          bg: 'bg-orange-50 dark:bg-orange-900/20',
          border: 'border-orange-200 dark:border-orange-800',
          text: 'text-orange-700 dark:text-orange-300',
          textSecondary: 'text-orange-600 dark:text-orange-400',
          hover: 'hover:text-orange-800 dark:hover:text-orange-200'
        };
      case 'indigo':
        return {
          bg: 'bg-indigo-50 dark:bg-indigo-900/20',
          border: 'border-indigo-200 dark:border-indigo-800',
          text: 'text-indigo-700 dark:text-indigo-300',
          textSecondary: 'text-indigo-600 dark:text-indigo-400',
          hover: 'hover:text-indigo-800 dark:hover:text-indigo-200'
        };
      case 'cyan':
        return {
          bg: 'bg-cyan-950/30 dark:bg-cyan-950/30',
          border: 'border-cyan-800 dark:border-cyan-800',
          text: 'text-cyan-400 dark:text-cyan-400',
          textSecondary: 'text-cyan-500 dark:text-cyan-500',
          hover: 'hover:text-cyan-300 dark:hover:text-cyan-300'
        };
      case 'amber':
        return {
          bg: 'bg-amber-950/30 dark:bg-amber-950/30',
          border: 'border-amber-800 dark:border-amber-800',
          text: 'text-amber-400 dark:text-amber-400',
          textSecondary: 'text-amber-500 dark:text-amber-500',
          hover: 'hover:text-amber-300 dark:hover:text-amber-300'
        };
      case 'slate':
        return {
          bg: 'bg-slate-900/40 dark:bg-slate-900/40',
          border: 'border-slate-700 dark:border-slate-700',
          text: 'text-slate-300 dark:text-slate-300',
          textSecondary: 'text-slate-400 dark:text-slate-400',
          hover: 'hover:text-slate-200 dark:hover:text-slate-200'
        };
      default:
        return {
          bg: 'bg-yellow-50 dark:bg-yellow-900/20',
          border: 'border-yellow-200 dark:border-yellow-800',
          text: 'text-yellow-700 dark:text-yellow-300',
          textSecondary: 'text-yellow-600 dark:text-yellow-400',
          hover: 'hover:text-yellow-800 dark:hover:text-yellow-200'
        };
    }
  };

  const colors = getColorClasses(agentColor);
  const hasSystemPrompt = !!(event as OrchestratorAgentStartEvent & { system_prompt?: string }).system_prompt;
  const hasUserMessage = !!(event as OrchestratorAgentStartEvent & { user_message?: string }).user_message;
  const hasExpandableContent = event.objective || (event.input_data && event.input_data.context) || hasInputData || hasSystemPrompt || hasUserMessage;
  const modelDisplayName = getModelDisplayName({
    provider: event.provider,
    modelId: event.model_id,
    metadata: modelMetadataCatalog,
    savedLLMs,
    availableLLMs,
  })
  const modeLabel = isEvaluationAgent
    ? (useScriptedMode ? 'Eval Learn Code' : modeFlags.use_code_execution_mode ? 'Eval Code Exec' : 'Eval')
    : (useScriptedMode ? 'Learn Code' : modeFlags.use_code_execution_mode ? 'Code Exec' : null)
  const displayName = friendlyAgentName(event)
  const treeRoleLabel = getTreeRoleLabel()

  return (
    <div className={`p-2 ${colors.bg} border ${colors.border} rounded transition-all duration-200 ${isWorkshopStep ? 'border-l-4 ml-2' : ''}`}>
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <div className={`w-6 h-6 ${colors.bg} rounded-full flex items-center justify-center flex-shrink-0`}>
              <span className="text-sm">{agentIcon}</span>
            </div>
            <div className="min-w-0 flex-1">
              <div className={`flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm font-medium ${colors.text}`}>
                <span className="min-w-0 truncate">{displayName}</span>
                <span className={`shrink-0 rounded border px-1.5 py-0.5 text-[10px] font-medium leading-none ${colors.border} ${colors.textSecondary}`}>
                  {treeRoleLabel}
                </span>
                {modeLabel && (
                  <span className={`shrink-0 text-[10px] font-normal ${colors.textSecondary}`}>{modeLabel}</span>
                )}
                <span className={`shrink-0 text-[10px] font-normal ${colors.textSecondary}`}>
                  {workshopMeta || `Model: ${modelDisplayName}`}
                </span>
                {event.step_index !== undefined && (
                  <span className={`shrink-0 text-[10px] font-normal ${colors.textSecondary}`}>Step {event.step_index}</span>
                )}
                {toolCallCount !== undefined && toolCallCount > 0 && (
                  <span className={`shrink-0 text-[10px] font-normal ${colors.textSecondary}`}>{toolCallCount} tools</span>
                )}
                {latestToolLabel && (
                  <span className={`shrink-0 text-[10px] font-normal ${colors.textSecondary}`}>{latestToolLabel}</span>
                )}
                {isCollapsed && eventCount !== undefined && (
                  <span className={`text-[10px] font-normal ${colors.textSecondary}`}>{eventCount} events collapsed</span>
                )}
              </div>
            </div>
          </div>
        </div>
        
        {/* Right side: Time and expand buttons */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className={`text-xs ${colors.textSecondary}`}>
              {formatTimestamp(event.timestamp)}
            </div>
          )}

          {/* Toggle for inputs/details */}
          {!isCollapsed && hasExpandableContent && (
            <button
              onClick={toggle}
              className={`p-0.5 ${colors.hover} rounded ${colors.text} transition-colors flex items-center gap-1`}
              title={isInputsExpanded ? "Collapse inputs (Alt+Click for all)" : "Expand inputs (Alt+Click for all)"}
            >
              <span className="text-[10px] uppercase font-bold">Inputs</span>
              {isInputsExpanded ? <Minus className="w-3 h-3" /> : <Plus className="w-3 h-3" />}
            </button>
          )}
          
        </div>
      </div>

      {/* Objective - always visible when not collapsed */}
      {!isCollapsed && event.objective && (
        <div className={`mt-2 text-xs ${colors.textSecondary}`}>
          <ConversationMarkdownRenderer content={event.objective} maxHeight="200px" />
        </div>
      )}

      {/* Expandable content - only show when not collapsed AND inputs expanded */}
      {!isCollapsed && isInputsExpanded && (
        <div className="mt-3 space-y-3">
          {/* Objective is always shown above; only show here if inputs are expanded for full height */}

          {/* System Prompt */}
          {hasSystemPrompt && (
            <div>
              <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>System Prompt:</div>
              <div className={`${colors.bg} rounded p-3 text-sm border ${colors.border} max-h-[400px] overflow-y-auto`}>
                <ConversationMarkdownRenderer
                  content={(event as OrchestratorAgentStartEvent & { system_prompt?: string }).system_prompt || ''}
                  maxHeight="400px"
                  disablePathLinking
                />
              </div>
            </div>
          )}

          {/* User Message */}
          {hasUserMessage && (
            <div>
              <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>User Message:</div>
              <div className={`${colors.bg} rounded p-3 text-sm border ${colors.border} max-h-[400px] overflow-y-auto`}>
                <ConversationMarkdownRenderer
                  content={(event as OrchestratorAgentStartEvent & { user_message?: string }).user_message || ''}
                  maxHeight="400px"
                  disablePathLinking
                />
              </div>
            </div>
          )}

          {/* Input Data */}
          {hasInputData && (
            <div>
              <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Input Data:</div>
              <div className={`${colors.bg} rounded p-3 text-sm`}>
                {/* Step Number - Highlighted */}
                {event.input_data?.step_number && (
                  <div className="mb-3 p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded">
                    <div className="text-xs font-bold text-blue-700 dark:text-blue-300">
                      Step #{event.input_data.step_number}
                    </div>
                  </div>
                )}
                {Object.entries(event.input_data || {})
                  .filter(([key]) => key !== 'step_number' && key !== 'context')
                  .filter(([, value]) => {
                    // Filter out empty values: null, undefined, empty string
                    if (value === null || value === undefined || value === '') return false;
                    // For string values, check if trimmed string is empty
                    if (typeof value === 'string' && value.trim() === '') return false;
                    return true;
                  })
                  .map(([key, value]) => (
                    <div key={key} className="mb-2 last:mb-0">
                      <div className={`font-medium ${colors.text} mb-1`}>{key}:</div>
                      <div className={colors.textSecondary}>
                        <ConversationMarkdownRenderer 
                          content={value} 
                          maxHeight="200px" 
                        />
                      </div>
                    </div>
                  ))}
              </div>
            </div>
          )}

          {/* Additional metadata */}
          <div className={`text-xs ${colors.textSecondary} space-y-1`}>
            {event.plan_id && (
              <div>Plan ID: {event.plan_id}</div>
            )}
            {event.step_index !== undefined && (
              <div>Step Index: {event.step_index}</div>
            )}
            {event.iteration !== undefined && (
              <div>Iteration: {event.iteration}</div>
            )}
          </div>
        </div>
      )}

    </div>
  );
};
