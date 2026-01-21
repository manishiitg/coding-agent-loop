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
  useCodeExecutionMode?: boolean
  useToolSearchMode?: boolean
  preDiscoveredTools?: string[]
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
  learningAfterLoopIteration = false,
  useCodeExecutionMode = false,
  useToolSearchMode = false,
  preDiscoveredTools = []
}: NodeConfigFooterProps) => {
  // Determine mode: simple mode is neither code exec nor tool search
  const isSimpleMode = !useCodeExecutionMode && !useToolSearchMode
  const hasConfig = executionLLM ||
    learningLLM ||
    effectiveServers.length > 0 ||
    toolsDisplayInfo.length > 0 ||
    hasWorkspaceTools ||
    hasHumanTools ||
    hasLargeOutput ||
    learningAfterLoopIteration ||
    preDiscoveredTools.length > 0

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
        {/* SIMPLE MODE: Server (tool count) badges - gray */}
        {isSimpleMode && toolsDisplayInfo.length > 0 && toolsDisplayInfo.map((info, i) => (
          <span key={i} className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-400">
            {info.hasAllTools ? `${info.server} (all tools)` : `${info.server} (${info.specificTools} tool${info.specificTools !== 1 ? 's' : ''})`}
          </span>
        ))}

        {/* CODE EXEC MODE: Server names only - amber */}
        {useCodeExecutionMode && effectiveServers.map((server, i) => (
          <span
            key={i}
            className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300"
            title={`Server available for code execution: ${server}`}
          >
            {server}
          </span>
        ))}

        {/* TOOL SEARCH MODE: Pre-discovered tools - yellow */}
        {useToolSearchMode && preDiscoveredTools.length > 0 && (
          <span
            className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300"
            title={`Pre-discovered tools: ${preDiscoveredTools.join(', ')}`}
          >
            {preDiscoveredTools.length <= 3
              ? preDiscoveredTools.join(', ')
              : `${preDiscoveredTools.slice(0, 2).join(', ')} +${preDiscoveredTools.length - 2} more`}
          </span>
        )}

        {/* TOOL SEARCH MODE: De-emphasized server info - muted gray, smaller */}
        {useToolSearchMode && effectiveServers.length > 0 && (
          <span
            className="px-1.5 py-0.5 rounded-md text-[9px] font-medium bg-gray-100 dark:bg-gray-800/40 text-gray-500 dark:text-gray-500"
            title={`Searchable servers: ${effectiveServers.join(', ')}`}
          >
            {effectiveServers.length === 1 ? effectiveServers[0] : `${effectiveServers.length} servers`}
          </span>
        )}
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
          <span className="px-2 py-0.5 rounded-md text-[10px] font-medium bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300" title="Context offloading enabled">
            Context Offloading
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

