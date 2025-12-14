import { memo } from 'react'

interface ToolsDisplayInfo {
  server: string
  hasAllTools: boolean
  specificTools: number
}

interface WorkspaceToolsInfo {
  enabled: number
  total: number
}

interface HumanToolsInfo {
  enabled: number
  total: number
}

interface NodeConfigFooterProps {
  executionLLM?: string | null
  executionMaxTurns?: number
  learningLLM?: string | null
  learningDetailLevel?: string | null
  lockLearnings?: boolean
  effectiveServers?: string[]
  toolsDisplayInfo?: ToolsDisplayInfo[]
  workspaceToolsInfo?: WorkspaceToolsInfo
  hasWorkspaceTools?: boolean
  humanToolsInfo?: HumanToolsInfo
  hasHumanTools?: boolean
  hasLargeOutput?: boolean
  learningAfterLoopIteration?: boolean
}

export const NodeConfigFooter = memo(({
  executionLLM,
  executionMaxTurns = 100,
  learningLLM,
  learningDetailLevel,
  lockLearnings = false,
  effectiveServers = [],
  toolsDisplayInfo = [],
  workspaceToolsInfo,
  hasWorkspaceTools = false,
  humanToolsInfo,
  hasHumanTools = false,
  hasLargeOutput = false,
  learningAfterLoopIteration = false
}: NodeConfigFooterProps) => {
  const hasConfig = executionLLM || 
    learningLLM || 
    effectiveServers.length > 0 || 
    toolsDisplayInfo.length > 0 || 
    hasWorkspaceTools || 
    hasHumanTools || 
    hasLargeOutput ||
    learningAfterLoopIteration

  if (!hasConfig) {
    return null
  }

  return (
    <div className="px-4 py-2.5 bg-gray-50 dark:bg-gray-800/30 border-t border-gray-200 dark:border-gray-700">
      <div className="flex flex-wrap gap-1.5">
        {executionLLM && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300">
            {executionLLM}
          </span>
        )}
        {executionMaxTurns !== 100 && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-100 dark:bg-gray-800/30 text-gray-600 dark:text-gray-400">
            {executionMaxTurns} turns
          </span>
        )}
        {learningLLM && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-100 dark:bg-gray-800/30 text-amber-600 dark:text-amber-400">
            Learn: {learningLLM}{learningDetailLevel && ` (${learningDetailLevel})`}{lockLearnings && ' 🔒'}
          </span>
        )}
        {lockLearnings && !learningLLM && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300" title="Learnings are locked - learning agent will not run but existing learnings will be used">
            Learnings Locked 🔒
          </span>
        )}
        {effectiveServers.map((s, i) => (
          <span key={i} className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300">
            {s}
          </span>
        ))}
        {toolsDisplayInfo.length > 0 && toolsDisplayInfo.map((info, i) => (
          <span key={i} className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
            {info.hasAllTools ? `${info.server} (all tools)` : `${info.server} (${info.specificTools} tool${info.specificTools !== 1 ? 's' : ''})`}
          </span>
        ))}
        {hasWorkspaceTools && workspaceToolsInfo && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300" title={`Workspace tools: ${workspaceToolsInfo.enabled}/${workspaceToolsInfo.total}`}>
            WS: {workspaceToolsInfo.enabled}/{workspaceToolsInfo.total}
          </span>
        )}
        {hasHumanTools && humanToolsInfo && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-orange-100 dark:bg-orange-900/30 text-orange-700 dark:text-orange-300" title={`Human tools: ${humanToolsInfo.enabled}/${humanToolsInfo.total}`}>
            Human: {humanToolsInfo.enabled}/{humanToolsInfo.total}
          </span>
        )}
        {hasLargeOutput && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300" title="Large output virtual tools enabled">
            Large Output
          </span>
        )}
        {learningAfterLoopIteration && (
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-cyan-50 dark:bg-cyan-900/20 text-cyan-600 dark:text-cyan-400" title="Learning runs after each loop iteration">
            Learn Each Iteration
          </span>
        )}
      </div>
    </div>
  )
})

NodeConfigFooter.displayName = 'NodeConfigFooter'

