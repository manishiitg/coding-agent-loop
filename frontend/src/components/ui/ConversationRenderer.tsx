import React, { useMemo, useState } from 'react'
import { MarkdownRenderer } from './MarkdownRenderer'

// ---------- Types ----------

interface FunctionCallData {
  Name: string
  Arguments: string
}

interface ConversationPart {
  // Text message
  Text?: string
  text?: string
  // Tool call (AI → tool)
  ID?: string
  Type?: string
  FunctionCall?: FunctionCallData
  ThoughtSignature?: string
  // Tool result (tool → AI)
  ToolCallID?: string
  Name?: string
  Content?: string
}

interface ConversationMessage {
  Role: string
  Parts: ConversationPart[]
}

interface ConversationData {
  conversation_history: ConversationMessage[]
  session_id?: string
  phase_id?: string
  updated_at?: string
  step_index?: number
  step_path?: string
  loop_iteration?: number
  retry_attempt?: number
  timestamp?: string
}

interface ConversationRendererProps {
  content: string
}

// ---------- Helpers ----------

/** Detect if parsed JSON is a conversation log */
export function isConversationJSON(filePath: string, parsed: unknown): parsed is ConversationData {
  const lowerPath = filePath.toLowerCase()
  if (!lowerPath.endsWith('-conversation.json') && !lowerPath.endsWith('/conversation.json')) return false
  if (!parsed || typeof parsed !== 'object') return false
  const obj = parsed as Record<string, unknown>
  return Array.isArray(obj.conversation_history)
}

function classifyPart(part: ConversationPart): 'text' | 'tool_call' | 'tool_result' | 'unknown' {
  if (part.FunctionCall) return 'tool_call'
  if (part.ToolCallID) return 'tool_result'
  if (part.Text || part.text) return 'text'
  return 'unknown'
}

function getRoleConfig(role: string, hasToolCall: boolean, hasToolResult: boolean) {
  if (hasToolResult) {
    return { label: 'Tool Result', color: 'text-orange-700 dark:text-orange-300', bgColor: 'bg-orange-50 dark:bg-orange-900/20 border-orange-200 dark:border-orange-800', icon: '📎' }
  }
  if (hasToolCall) {
    return { label: 'Tool Call', color: 'text-amber-700 dark:text-amber-300', bgColor: 'bg-amber-50 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800', icon: '🔧' }
  }
  switch (role.toLowerCase()) {
    case 'system':
      return { label: 'System', color: 'text-purple-700 dark:text-purple-300', bgColor: 'bg-purple-50 dark:bg-purple-900/30 border-purple-200 dark:border-purple-800', icon: '⚙️' }
    case 'human':
    case 'user':
      return { label: 'User', color: 'text-blue-700 dark:text-blue-300', bgColor: 'bg-blue-50 dark:bg-blue-900/30 border-blue-200 dark:border-blue-800', icon: '👤' }
    case 'ai':
    case 'assistant':
      return { label: 'Assistant', color: 'text-green-700 dark:text-green-300', bgColor: 'bg-green-50 dark:bg-green-900/30 border-green-200 dark:border-green-800', icon: '🤖' }
    default:
      return { label: role, color: 'text-gray-700 dark:text-gray-300', bgColor: 'bg-gray-50 dark:bg-gray-900/30 border-gray-200 dark:border-gray-800', icon: '💬' }
  }
}

function tryFormatJson(str: string): string | null {
  try {
    return JSON.stringify(JSON.parse(str), null, 2)
  } catch {
    return null
  }
}

function truncate(str: string, max: number): string {
  return str.length > max ? str.slice(0, max) + '…' : str
}

// ---------- Sub-components ----------

function ToolCallPart({ part }: { part: ConversationPart }) {
  const [expanded, setExpanded] = useState(false)
  const fc = part.FunctionCall!
  const formattedArgs = tryFormatJson(fc.Arguments)
  const displayArgs = formattedArgs || fc.Arguments

  return (
    <div className="rounded border border-amber-200 dark:border-amber-800 bg-amber-50/50 dark:bg-amber-900/10 p-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-xs font-mono font-semibold text-amber-800 dark:text-amber-200">
            {fc.Name}
          </span>
          {part.ID && (
            <span className="text-[10px] text-amber-500 dark:text-amber-400 font-mono">
              {part.ID}
            </span>
          )}
        </div>
        <button
          onClick={() => setExpanded(e => !e)}
          className="text-[10px] text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 px-1.5 py-0.5 rounded hover:bg-white/50 dark:hover:bg-gray-700/50"
        >
          {expanded ? 'Hide args' : 'Show args'}
        </button>
      </div>
      {!expanded && (
        <p className="text-[11px] font-mono text-gray-500 dark:text-gray-400 mt-1 truncate">
          {truncate(fc.Arguments, 120)}
        </p>
      )}
      {expanded && (
        <pre className="text-[11px] font-mono text-gray-700 dark:text-gray-300 mt-1 overflow-x-auto whitespace-pre-wrap break-words bg-white/50 dark:bg-black/20 rounded p-2 max-h-[300px] overflow-y-auto">
          {displayArgs}
        </pre>
      )}
    </div>
  )
}

function ToolResultPart({ part }: { part: ConversationPart }) {
  const [expanded, setExpanded] = useState(false)
  const content = part.Content || ''
  const isError = content.toLowerCase().includes('failed') || content.toLowerCase().includes('error')
  const isLong = content.length > 300
  const formattedContent = tryFormatJson(content)

  return (
    <div className={`rounded border p-2 ${isError
      ? 'border-red-200 dark:border-red-800 bg-red-50/50 dark:bg-red-900/10'
      : 'border-orange-200 dark:border-orange-800 bg-orange-50/50 dark:bg-orange-900/10'
    }`}>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          {part.Name && (
            <span className="text-xs font-mono font-semibold text-orange-800 dark:text-orange-200">
              {part.Name}
            </span>
          )}
          {part.ToolCallID && (
            <span className="text-[10px] text-orange-500 dark:text-orange-400 font-mono">
              {part.ToolCallID}
            </span>
          )}
          {isError && (
            <span className="text-[10px] bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 px-1.5 py-0.5 rounded">
              error
            </span>
          )}
          <span className="text-[10px] text-gray-400">
            {content.length.toLocaleString()} chars
          </span>
        </div>
        {isLong && (
          <button
            onClick={() => setExpanded(e => !e)}
            className="text-[10px] text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 px-1.5 py-0.5 rounded hover:bg-white/50 dark:hover:bg-gray-700/50"
          >
            {expanded ? 'Collapse' : 'Expand'}
          </button>
        )}
      </div>
      <pre className={`text-[11px] font-mono mt-1 overflow-x-auto whitespace-pre-wrap break-words bg-white/50 dark:bg-black/20 rounded p-2 ${
        isError ? 'text-red-700 dark:text-red-300' : 'text-gray-700 dark:text-gray-300'
      } ${!expanded && isLong ? 'max-h-[80px] overflow-hidden' : 'max-h-[400px] overflow-y-auto'}`}>
        {formattedContent || content || '(empty)'}
      </pre>
    </div>
  )
}

function MessageBubble({ message, index, totalMessages }: { message: ConversationMessage; index: number; totalMessages: number }) {
  const parts = message.Parts || []
  const partTypes = parts.map(classifyPart)
  const hasToolCall = partTypes.includes('tool_call')
  const hasToolResult = partTypes.includes('tool_result')
  const textParts = parts.filter((_, i) => partTypes[i] === 'text')
  const toolCallParts = parts.filter((_, i) => partTypes[i] === 'tool_call')
  const toolResultParts = parts.filter((_, i) => partTypes[i] === 'tool_result')
  const text = textParts.map(p => p.Text || p.text || '').join('\n').trim()

  const isSystem = message.Role.toLowerCase() === 'system'
  const isToolMsg = hasToolCall || hasToolResult

  const [expanded, setExpanded] = useState(() => {
    if (isSystem) return false
    // Tool call/result messages: collapsed by default to reduce noise
    if (isToolMsg) return false
    return true
  })

  const role = getRoleConfig(message.Role, hasToolCall, hasToolResult)
  const isLong = text.length > 500

  // Collapsed preview
  const preview = useMemo(() => {
    if (expanded) return null
    if (hasToolCall) {
      const names = toolCallParts.map(p => p.FunctionCall?.Name).filter(Boolean)
      return names.join(', ')
    }
    if (hasToolResult) {
      const names = toolResultParts.map(p => p.Name).filter(Boolean)
      const hasError = toolResultParts.some(p => (p.Content || '').toLowerCase().includes('failed') || (p.Content || '').toLowerCase().includes('error'))
      return names.join(', ') + (hasError ? ' (error)' : '')
    }
    if (isSystem) {
      return truncate(text.replace(/\n/g, ' '), 150)
    }
    return null
  }, [expanded, hasToolCall, hasToolResult, isSystem, text, toolCallParts, toolResultParts])

  return (
    <div className="group">
      <div className="flex items-center gap-2 mb-1">
        <span className="text-[10px] text-gray-400 dark:text-gray-500 font-mono">
          {index + 1}/{totalMessages}
        </span>
        <div className="h-px flex-1 bg-gray-100 dark:bg-gray-800" />
      </div>

      <div className={`rounded-lg border p-3 ${role.bgColor} transition-all`}>
        {/* Header */}
        <div className="flex items-center justify-between mb-1">
          <div className="flex items-center gap-2">
            <span className="text-sm">{role.icon}</span>
            <span className={`text-xs font-semibold uppercase tracking-wide ${role.color}`}>
              {role.label}
            </span>
            {hasToolCall && (
              <span className="text-[10px] bg-amber-100 dark:bg-amber-800/40 text-amber-700 dark:text-amber-300 px-1.5 py-0.5 rounded">
                {toolCallParts.length} call{toolCallParts.length > 1 ? 's' : ''}
              </span>
            )}
            {hasToolResult && (
              <span className="text-[10px] bg-orange-100 dark:bg-orange-800/40 text-orange-700 dark:text-orange-300 px-1.5 py-0.5 rounded">
                {toolResultParts.length} result{toolResultParts.length > 1 ? 's' : ''}
              </span>
            )}
            {isSystem && (
              <span className="text-[10px] bg-purple-100 dark:bg-purple-800 text-purple-600 dark:text-purple-300 px-1.5 py-0.5 rounded">
                {text.length.toLocaleString()} chars
              </span>
            )}
          </div>
          {(isSystem || isLong || isToolMsg) && (
            <button
              onClick={() => setExpanded(e => !e)}
              className="text-[10px] text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 px-2 py-0.5 rounded hover:bg-white/50 dark:hover:bg-gray-700/50 transition-colors"
            >
              {expanded ? 'Collapse' : 'Expand'}
            </button>
          )}
        </div>

        {/* Collapsed preview */}
        {!expanded && preview && (
          <p className="text-[11px] text-gray-500 dark:text-gray-400 italic cursor-pointer font-mono truncate" onClick={() => setExpanded(true)}>
            {preview}
          </p>
        )}

        {/* Expanded content */}
        {expanded && (
          <div className="space-y-2 mt-1">
            {/* Text content */}
            {text && (
              <div className={`prose prose-sm max-w-none dark:prose-invert prose-p:my-1 prose-headings:my-2 prose-pre:my-1 prose-ul:my-1 prose-ol:my-1 ${isSystem ? 'max-h-[600px] overflow-y-auto' : ''}`}>
                <MarkdownRenderer content={text} className="max-w-none" />
              </div>
            )}

            {/* Tool calls */}
            {toolCallParts.length > 0 && (
              <div className="space-y-1.5">
                {toolCallParts.map((part, i) => (
                  <ToolCallPart key={i} part={part} />
                ))}
              </div>
            )}

            {/* Tool results */}
            {toolResultParts.length > 0 && (
              <div className="space-y-1.5">
                {toolResultParts.map((part, i) => (
                  <ToolResultPart key={i} part={part} />
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

// ---------- Main component ----------

export function ConversationRenderer({ content }: ConversationRendererProps) {
  const data = useMemo(() => {
    try {
      return JSON.parse(content) as ConversationData
    } catch {
      return null
    }
  }, [content])

  const [showRaw, setShowRaw] = useState(false)
  const [filterMode, setFilterMode] = useState<'all' | 'chat' | 'tools'>('all')

  if (!data?.conversation_history) {
    return (
      <pre className="text-xs font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap break-words">
        {content}
      </pre>
    )
  }

  const messages = data.conversation_history
  const isBuilderLog = !!data.session_id
  const isExecutionLog = data.step_index !== undefined

  // Count tool calls and results
  const toolCallCount = messages.reduce((acc, m) => acc + (m.Parts || []).filter(p => p.FunctionCall).length, 0)
  const toolResultErrorCount = messages.reduce((acc, m) => acc + (m.Parts || []).filter(p => p.ToolCallID && (p.Content || '').toLowerCase().includes('failed')).length, 0)

  // Filter messages
  const filteredMessages = messages.filter(msg => {
    if (filterMode === 'all') return true
    const parts = msg.Parts || []
    const hasToolParts = parts.some(p => p.FunctionCall || p.ToolCallID)
    if (filterMode === 'tools') return hasToolParts
    // 'chat' mode: show non-tool messages
    return !hasToolParts
  })

  return (
    <div className="space-y-3">
      {/* Header */}
      <div className="flex items-center justify-between flex-wrap gap-2">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-semibold text-gray-700 dark:text-gray-300">
            {isBuilderLog ? '💬 Builder Conversation' : isExecutionLog ? '⚡ Step Execution Log' : '💬 Conversation'}
          </span>
          <span className="text-xs bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400 px-2 py-0.5 rounded-full">
            {messages.length} messages
          </span>
          {toolCallCount > 0 && (
            <span className="text-xs bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-300 px-2 py-0.5 rounded-full">
              {toolCallCount} tool calls
            </span>
          )}
          {toolResultErrorCount > 0 && (
            <span className="text-xs bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 px-2 py-0.5 rounded-full">
              {toolResultErrorCount} errors
            </span>
          )}
          {isExecutionLog && data.step_path && (
            <span className="text-xs bg-indigo-100 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300 px-2 py-0.5 rounded-full">
              {data.step_path}
            </span>
          )}
          {isExecutionLog && data.retry_attempt !== undefined && data.retry_attempt > 1 && (
            <span className="text-xs bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300 px-2 py-0.5 rounded-full">
              retry #{data.retry_attempt}
            </span>
          )}
          {data.phase_id && (
            <span className="text-xs bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 px-2 py-0.5 rounded-full">
              {data.phase_id}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          {(data.updated_at || data.timestamp) && (
            <span className="text-[10px] text-gray-400 dark:text-gray-500 mr-2">
              {new Date(data.updated_at || data.timestamp!).toLocaleString()}
            </span>
          )}
          {/* Filter buttons */}
          {toolCallCount > 0 && (
            <div className="flex rounded border border-gray-200 dark:border-gray-700 overflow-hidden">
              {(['all', 'chat', 'tools'] as const).map(mode => (
                <button
                  key={mode}
                  onClick={() => setFilterMode(mode)}
                  className={`text-[10px] px-2 py-0.5 transition-colors ${
                    filterMode === mode
                      ? 'bg-gray-200 dark:bg-gray-700 text-gray-800 dark:text-gray-200'
                      : 'text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-800'
                  }`}
                >
                  {mode === 'all' ? 'All' : mode === 'chat' ? 'Chat only' : 'Tools only'}
                </button>
              ))}
            </div>
          )}
          <button
            onClick={() => setShowRaw(r => !r)}
            className="text-[10px] text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 px-2 py-1 rounded border border-gray-200 dark:border-gray-700 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors ml-1"
          >
            {showRaw ? 'Chat View' : 'Raw JSON'}
          </button>
        </div>
      </div>

      {showRaw ? (
        <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
          <pre className="text-xs font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap break-words leading-relaxed">
            {content}
          </pre>
        </div>
      ) : (
        <div className="space-y-3">
          {filteredMessages.map((msg, i) => {
            const originalIndex = messages.indexOf(msg)
            return (
              <MessageBubble key={originalIndex} message={msg} index={originalIndex} totalMessages={messages.length} />
            )
          })}
        </div>
      )}
    </div>
  )
}
