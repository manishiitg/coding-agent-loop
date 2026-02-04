import React, { useState, useMemo } from 'react'
import {
  Bot,
  User,
  Settings,
  Wrench,
  ChevronDown,
  ChevronRight,
  Code,
  FileText
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

// Type definitions for conversation structure
interface ConversationPart {
  Text?: string
  ID?: string
  FunctionCall?: { Name: string; Arguments: string }
  ToolCallID?: string
  Name?: string
  Content?: string
}

interface ConversationMessage {
  Role: 'system' | 'human' | 'ai' | 'tool'
  Parts: ConversationPart[]
}

interface ConversationData {
  conversation_history: ConversationMessage[]
}

interface ConversationViewerProps {
  content: string // Raw JSON string
}

// Role-based styling configuration
const roleConfig = {
  system: {
    icon: Settings,
    label: 'System',
    bgClass: 'bg-purple-50 dark:bg-purple-950/30',
    borderClass: 'border-purple-200 dark:border-purple-800',
    iconClass: 'text-purple-500',
    labelClass: 'text-purple-700 dark:text-purple-300'
  },
  human: {
    icon: User,
    label: 'User',
    bgClass: 'bg-blue-50 dark:bg-blue-950/30',
    borderClass: 'border-blue-200 dark:border-blue-800',
    iconClass: 'text-blue-500',
    labelClass: 'text-blue-700 dark:text-blue-300'
  },
  ai: {
    icon: Bot,
    label: 'Assistant',
    bgClass: 'bg-green-50 dark:bg-green-950/30',
    borderClass: 'border-green-200 dark:border-green-800',
    iconClass: 'text-green-500',
    labelClass: 'text-green-700 dark:text-green-300'
  },
  tool: {
    icon: Wrench,
    label: 'Tool',
    bgClass: 'bg-orange-50 dark:bg-orange-950/30',
    borderClass: 'border-orange-200 dark:border-orange-800',
    iconClass: 'text-orange-500',
    labelClass: 'text-orange-700 dark:text-orange-300'
  }
}

// Collapsible content component
const CollapsibleContent: React.FC<{
  content: string
  defaultExpanded?: boolean
  maxPreviewLength?: number
  className?: string
  isJson?: boolean
}> = ({ content, defaultExpanded = false, maxPreviewLength = 200, className, isJson = false }) => {
  const [isExpanded, setIsExpanded] = useState(defaultExpanded)
  const shouldCollapse = content.length > maxPreviewLength

  if (!shouldCollapse) {
    return isJson ? (
      <pre className={cn('whitespace-pre-wrap text-[10px] font-mono', className)}>{content}</pre>
    ) : (
      <div className={className}>
        <MarkdownRenderer content={content} className="!text-[11px] [&_p]:!text-[11px] [&_li]:!text-[11px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_code]:!text-[10px]" />
      </div>
    )
  }

  return (
    <div>
      <div className={cn('overflow-hidden', !isExpanded && 'max-h-20')}>
        {isJson ? (
          <pre className={cn('whitespace-pre-wrap text-[10px] font-mono', className)}>{content}</pre>
        ) : (
          <div className={className}>
            <MarkdownRenderer content={content} className="!text-[11px] [&_p]:!text-[11px] [&_li]:!text-[11px] [&_h1]:!text-base [&_h2]:!text-sm [&_h3]:!text-xs [&_code]:!text-[10px]" />
          </div>
        )}
      </div>
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="flex items-center gap-1 mt-1.5 text-[10px] font-medium text-muted-foreground hover:text-foreground transition-colors"
      >
        {isExpanded ? (
          <>
            <ChevronDown className="w-2.5 h-2.5" /> Show less
          </>
        ) : (
          <>
            <ChevronRight className="w-2.5 h-2.5" /> Show more ({content.length} chars)
          </>
        )}
      </button>
    </div>
  )
}

// Tool call display component
const ToolCallDisplay: React.FC<{
  name: string
  arguments: string
  callId?: string
}> = ({ name, arguments: args, callId }) => {
  const [showArgs, setShowArgs] = useState(false)

  let formattedArgs = args
  try {
    const parsed = JSON.parse(args)
    formattedArgs = JSON.stringify(parsed, null, 2)
  } catch {
    // Keep original if not valid JSON
  }

  return (
    <div className="bg-muted/50 rounded border border-border p-1.5 my-1">
      <div className="flex items-center gap-1.5">
        <Code className="w-3 h-3 text-orange-500 flex-shrink-0" />
        <span className="font-mono text-[11px] font-semibold text-foreground">{name}</span>
        {callId && (
          <span className="text-[9px] font-mono text-muted-foreground bg-muted px-1 py-0.5 rounded">
            {callId}
          </span>
        )}
        <button
          onClick={() => setShowArgs(!showArgs)}
          className="ml-auto flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
        >
          {showArgs ? <ChevronDown className="w-2.5 h-2.5" /> : <ChevronRight className="w-2.5 h-2.5" />}
          Args
        </button>
      </div>
      {showArgs && (
        <pre className="mt-1.5 p-1.5 bg-background rounded text-[10px] font-mono overflow-x-auto max-h-48 overflow-y-auto text-muted-foreground">
          {formattedArgs}
        </pre>
      )}
    </div>
  )
}

// Tool response display component
const ToolResponseDisplay: React.FC<{
  toolName?: string
  toolCallId?: string
  content: string
}> = ({ toolName, toolCallId, content }) => {
  const [showContent, setShowContent] = useState(false)

  let displayContent = content
  try {
    const parsed = JSON.parse(content)
    displayContent = JSON.stringify(parsed, null, 2)
  } catch {
    // Keep original if not valid JSON
  }

  const isLongContent = displayContent.length > 300

  return (
    <div className="bg-muted/30 rounded border border-border p-1.5 my-1">
      <div className="flex items-center gap-1.5">
        <FileText className="w-3 h-3 text-orange-500 flex-shrink-0" />
        {toolName && (
          <span className="font-mono text-[11px] font-medium text-foreground">{toolName}</span>
        )}
        {toolCallId && (
          <span className="text-[9px] font-mono text-muted-foreground bg-muted px-1 py-0.5 rounded">
            {toolCallId}
          </span>
        )}
        {isLongContent && (
          <button
            onClick={() => setShowContent(!showContent)}
            className="ml-auto flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground transition-colors"
          >
            {showContent ? <ChevronDown className="w-2.5 h-2.5" /> : <ChevronRight className="w-2.5 h-2.5" />}
            {showContent ? 'Hide' : 'Show'}
          </button>
        )}
      </div>
      {(isLongContent ? showContent : true) && (
        <pre className="mt-1.5 p-1.5 bg-background rounded text-[10px] font-mono overflow-x-auto max-h-48 overflow-y-auto text-muted-foreground whitespace-pre-wrap">
          {displayContent}
        </pre>
      )}
    </div>
  )
}

// Message component
const MessageDisplay: React.FC<{
  message: ConversationMessage
  index: number
}> = ({ message, index }) => {
  const config = roleConfig[message.Role] || roleConfig.system
  const Icon = config.icon

  // Render parts based on their content
  const renderParts = () => {
    return message.Parts.map((part, partIndex) => {
      // Text content
      if (part.Text) {
        const isSystem = message.Role === 'system'
        return (
          <CollapsibleContent
            key={partIndex}
            content={part.Text}
            defaultExpanded={!isSystem}
            maxPreviewLength={isSystem ? 500 : 1000}
            className="text-[11px] text-foreground"
          />
        )
      }

      // Tool call (function call from AI)
      if (part.FunctionCall) {
        return (
          <ToolCallDisplay
            key={partIndex}
            name={part.FunctionCall.Name}
            arguments={part.FunctionCall.Arguments}
            callId={part.ID}
          />
        )
      }

      // Tool response
      if (part.ToolCallID || part.Content !== undefined) {
        return (
          <ToolResponseDisplay
            key={partIndex}
            toolName={part.Name}
            toolCallId={part.ToolCallID}
            content={part.Content || ''}
          />
        )
      }

      return null
    })
  }

  return (
    <div
      className={cn(
        'rounded border p-2 mb-2',
        config.bgClass,
        config.borderClass
      )}
    >
      <div className="flex items-center gap-1.5 mb-1.5">
        <Icon className={cn('w-3 h-3', config.iconClass)} />
        <span className={cn('text-[10px] font-semibold uppercase tracking-wider', config.labelClass)}>
          {config.label}
        </span>
        <span className="text-[9px] text-muted-foreground">#{index + 1}</span>
      </div>
      <div className="space-y-1.5">{renderParts()}</div>
    </div>
  )
}

// Main ConversationViewer component
export const ConversationViewer: React.FC<ConversationViewerProps> = ({ content }) => {
  const [showRawJson, setShowRawJson] = useState(false)

  // Parse the conversation JSON
  const { messages, parseError } = useMemo(() => {
    try {
      const data: ConversationData = JSON.parse(content)
      if (data.conversation_history && Array.isArray(data.conversation_history)) {
        return { messages: data.conversation_history, parseError: null }
      }
      return { messages: null, parseError: 'Invalid conversation format: missing conversation_history' }
    } catch (e) {
      return { messages: null, parseError: `Failed to parse conversation: ${e}` }
    }
  }, [content])

  // If parsing failed, show raw JSON with error message
  if (parseError || !messages) {
    return (
      <div>
        <div className="text-[10px] text-amber-600 dark:text-amber-400 mb-1.5 flex items-center gap-1">
          <Settings className="w-2.5 h-2.5" />
          {parseError || 'Could not parse conversation format'}
        </div>
        <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-80 overflow-y-auto text-[10px] font-mono">
          {content}
        </pre>
      </div>
    )
  }

  return (
    <div>
      {/* Toggle button */}
      <div className="flex justify-end mb-2">
        <button
          onClick={() => setShowRawJson(!showRawJson)}
          className="flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground hover:text-foreground bg-muted hover:bg-muted/80 rounded transition-colors"
        >
          <Code className="w-2.5 h-2.5" />
          {showRawJson ? 'Formatted' : 'Raw JSON'}
        </button>
      </div>

      {showRawJson ? (
        <pre className="whitespace-pre-wrap overflow-x-auto text-muted-foreground max-h-80 overflow-y-auto text-[10px] font-mono bg-muted/30 p-2 rounded border border-border">
          {content}
        </pre>
      ) : (
        <div className="max-h-80 overflow-y-auto pr-1">
          {messages.map((message, index) => (
            <MessageDisplay key={index} message={message} index={index} />
          ))}
        </div>
      )}
    </div>
  )
}
