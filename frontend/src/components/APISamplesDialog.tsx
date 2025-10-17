import React, { useState, useEffect } from 'react'
import { X, Copy, Check, ExternalLink, Play, Square, Workflow, Search, MessageCircle } from 'lucide-react'
import { Button } from './ui/Button'
import { useModeStore } from '../stores/useModeStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'

interface APISamplesDialogProps {
  isOpen: boolean
  onClose: () => void
}

interface CodeBlockProps {
  children: string
  language?: string
}

const CodeBlock: React.FC<CodeBlockProps> = ({ children, language = 'bash' }) => {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(children)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy:', err)
    }
  }

  return (
    <div className="relative">
      <pre className="bg-gray-900 text-gray-100 p-4 rounded-lg overflow-x-auto text-sm">
        <code className={`language-${language}`}>{children}</code>
      </pre>
      <button
        onClick={handleCopy}
        className="absolute top-2 right-2 p-1.5 bg-gray-700 hover:bg-gray-600 rounded text-gray-300 hover:text-white transition-colors"
        title="Copy to clipboard"
      >
        {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
      </button>
    </div>
  )
}

export const APISamplesDialog: React.FC<APISamplesDialogProps> = ({ isOpen, onClose }) => {
  const { selectedModeCategory } = useModeStore()
  const { getActivePreset } = usePresetApplication()
  
  // Handle ESC key to close dialog
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, onClose])
  
  if (!isOpen) return null
  
  const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'deep-research' | 'workflow')
  
  // Get mode-specific examples
  const getModeIcon = (mode: string) => {
    switch (mode) {
      case 'chat': return <MessageCircle className="w-4 h-4 text-blue-600" />
      case 'deep-research': return <Search className="w-4 h-4 text-green-600" />
      case 'workflow': return <Workflow className="w-4 h-4 text-purple-600" />
      default: return <MessageCircle className="w-4 h-4 text-blue-600" />
    }
  }

  const getModeName = (mode: string) => {
    switch (mode) {
      case 'chat': return 'Chat Mode'
      case 'deep-research': return 'Deep Research Mode'
      case 'workflow': return 'Workflow Mode'
      default: return 'Chat Mode'
    }
  }

  // Generate context-aware examples
  const generateExamples = () => {
    const presetId = activePreset?.id || 'your-preset-id'
    const presetLabel = activePreset?.label || 'Your Preset'
    const agentMode = activePreset?.agentMode || 'simple'
    
    if (selectedModeCategory === 'workflow') {
      return {
        executeExample: `# Execute workflow preset (actual execution phase)
curl -X POST http://localhost:8000/api/external/execute \\
  -H "Content-Type: application/json" \\
  -d '{
    "preset_id": "${presetId}",
    "execution_phase": "post-verification",
    "options": {
      "run_management": "create_new_runs_always",
      "execution_strategy": "sequential_execution"
    }
  }'`,
        executeResponse: `{
  "session_id": "session-uuid-123",
  "observer_id": "observer-uuid-456", 
  "status": "started",
  "message": "Workflow execution started",
  "agent_mode": "workflow",
  "preset_label": "${presetLabel}"
}`,
        phases: [
          { id: 'pre-verification', title: 'Planning & Todo Creation', description: 'Create and refine todo list' },
          { id: 'post-verification', title: 'Execution & Review', description: 'Execute approved todo list' }
        ]
      }
    } else if (selectedModeCategory === 'deep-research') {
      return {
        executeExample: `# Execute deep research preset
curl -X POST http://localhost:8000/api/external/execute \\
  -H "Content-Type: application/json" \\
  -d '{
    "preset_id": "${presetId}",
    "options": {
      "execution_mode": "sequential"
    }
  }'`,
        executeResponse: `{
  "session_id": "session-uuid-123",
  "observer_id": "observer-uuid-456", 
  "status": "started",
  "message": "Deep research execution started",
  "agent_mode": "orchestrator",
  "preset_label": "${presetLabel}"
}`,
        phases: []
      }
    } else {
      return {
        executeExample: `# Execute chat preset
curl -X POST http://localhost:8000/api/external/execute \\
  -H "Content-Type: application/json" \\
  -d '{
    "preset_id": "${presetId}"
  }'`,
        executeResponse: `{
  "session_id": "session-uuid-123",
  "observer_id": "observer-uuid-456", 
  "status": "started",
  "message": "Chat execution started",
  "agent_mode": "${agentMode}",
  "preset_label": "${presetLabel}"
}`,
        phases: []
      }
    }
  }

  const examples = generateExamples()

  const pollEventsExample = `# Poll for events using observer_id
curl "http://localhost:8000/api/observer/observer-uuid-456/events?since=0"

# Poll for new events since last check
curl "http://localhost:8000/api/observer/observer-uuid-456/events?since=15"`

  const pollEventsResponse = `{
  "events": [
    {
      "id": "event-1",
      "type": "${selectedModeCategory === 'workflow' ? 'workflow_start' : selectedModeCategory === 'deep-research' ? 'orchestrator_start' : 'conversation_start'}",
      "timestamp": "2025-01-27T10:30:00Z",
      "data": {
        "session_id": "session-uuid-123"${selectedModeCategory === 'workflow' ? ',\n        "phase": "pre-verification"' : ''}
      }
    },
    {
      "id": "event-2", 
      "type": "tool_call_start",
      "timestamp": "2025-01-27T10:30:05Z",
      "data": {
        "tool_name": "aws_cli_query",
        "arguments": {...}
      }
    }
  ],
  "last_event_index": 2,
  "has_more": true,
  "observer_id": "observer-uuid-456"
}`

  const cancelExecutionExample = `# Cancel execution using session_id
curl -X POST http://localhost:8000/api/external/cancel \\
  -H "Content-Type: application/json" \\
  -d '{
    "session_id": "session-uuid-123"
  }'`

  const cancelExecutionResponse = `{
  "session_id": "session-uuid-123",
  "status": "cancelled",
  "message": "Execution cancelled successfully"
}`

  const completeWorkflowExample = `#!/bin/bash

# 1. Connect to ${selectedModeCategory === 'workflow' ? 'workflow' : selectedModeCategory === 'deep-research' ? 'deep research' : 'chat'} preset
echo "🚀 Starting ${selectedModeCategory === 'workflow' ? 'workflow' : selectedModeCategory === 'deep-research' ? 'deep research' : 'chat'} connection..."
RESPONSE=$(curl -s -X POST http://localhost:8000/api/external/execute \\
  -H "Content-Type: application/json" \\
  -d '{"preset_id": "${activePreset?.id || 'your-preset-id'}"${selectedModeCategory === 'workflow' ? ',\n    "execution_phase": "post-verification",\n    "options": {\n      "run_management": "create_new_runs_always",\n      "execution_strategy": "sequential_execution"\n    }' : selectedModeCategory === 'deep-research' ? ',\n    "options": {\n      "execution_mode": "sequential"\n    }' : ''}}')

# Extract IDs
SESSION_ID=$(echo $RESPONSE | jq -r '.session_id')
OBSERVER_ID=$(echo $RESPONSE | jq -r '.observer_id')

echo "📋 Session ID: $SESSION_ID"
echo "👁️  Observer ID: $OBSERVER_ID"

# 2. Poll for events
echo "📡 Polling for events..."
LAST_INDEX=0

while true; do
  EVENTS_RESPONSE=$(curl -s "http://localhost:8000/api/observer/$OBSERVER_ID/events?since=$LAST_INDEX")
  
  EVENTS=$(echo $EVENTS_RESPONSE | jq -r '.events[]')
  LAST_INDEX=$(echo $EVENTS_RESPONSE | jq -r '.last_event_index')
  HAS_MORE=$(echo $EVENTS_RESPONSE | jq -r '.has_more')
  
  if [ "$HAS_MORE" = "true" ]; then
    echo "📊 New events received..."
    echo $EVENTS_RESPONSE | jq '.'
  fi
  
  # Check if connection completed
  if echo $EVENTS_RESPONSE | jq -e '.events[] | select(.type == "conversation_end")' > /dev/null; then
    echo "✅ Connection completed!"
    break
  fi
  
  sleep 2
done

echo "🎉 ${selectedModeCategory === 'workflow' ? 'Workflow' : selectedModeCategory === 'deep-research' ? 'Deep research' : 'Chat'} connection finished!"`

  return (
    <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50 p-4">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl max-w-4xl w-full max-h-[90vh] overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between p-6 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center gap-3">
            <ExternalLink className="w-5 h-5 text-blue-600" />
            <div>
              <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
                External Connection Examples
              </h2>
              <div className="flex items-center gap-2 mt-1">
                {getModeIcon(selectedModeCategory || 'chat')}
                <span className="text-sm text-gray-600 dark:text-gray-400">
                  {getModeName(selectedModeCategory || 'chat')}
                  {activePreset && ` • ${activePreset.label}`}
                </span>
              </div>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-2 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors"
          >
            <X className="w-5 h-5 text-gray-500" />
          </button>
        </div>

        {/* Content */}
        <div className="p-6 overflow-y-auto max-h-[calc(90vh-120px)]">
          <div className="space-y-8">
            {/* Introduction */}
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg p-4">
              <h3 className="font-semibold text-blue-900 dark:text-blue-100 mb-2">
                🚀 {getModeName(selectedModeCategory || 'chat')} External Connection
              </h3>
              <p className="text-sm text-blue-800 dark:text-blue-200">
                {activePreset 
                  ? `Connect to "${activePreset.label}" preset from external systems, curl commands, or Postman.`
                  : `Connect to ${getModeName(selectedModeCategory || 'chat').toLowerCase()} presets from external systems, curl commands, or Postman.`
                }
                {selectedModeCategory === 'workflow' && ' Workflow mode supports execution phases.'}
              </p>
            </div>

            {/* 1. Execute Preset */}
            <div>
              <div className="flex items-center gap-2 mb-3">
                <Play className="w-4 h-4 text-green-600" />
                <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                  1. Connect to {getModeName(selectedModeCategory || 'chat')} Preset
                </h3>
              </div>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
                Start a {selectedModeCategory === 'workflow' ? 'workflow' : selectedModeCategory === 'deep-research' ? 'deep research' : 'chat'} connection and get session_id + observer_id for tracking.
                {selectedModeCategory === 'workflow' && ' Workflow mode supports execution phases.'}
              </p>
              
              <div className="space-y-3">
                <div>
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Request:
                  </h4>
                  <CodeBlock>{examples.executeExample}</CodeBlock>
                </div>
                
                <div>
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Response:
                  </h4>
                  <CodeBlock language="json">{examples.executeResponse}</CodeBlock>
                </div>

                {/* Deep Research Options */}
                {selectedModeCategory === 'deep-research' && (
                  <div>
                    <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      Available Options:
                    </h4>
                    <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-3">
                      <div className="text-sm text-gray-700 dark:text-gray-300 mb-2">
                        <strong>execution_mode:</strong> Controls how agents execute
                      </div>
                      <div className="flex flex-wrap gap-2">
                        <span className="px-2 py-1 bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 text-xs rounded">
                          sequential
                        </span>
                        <span className="px-2 py-1 bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 text-xs rounded">
                          parallel
                        </span>
                      </div>
                    </div>
                    
                    <div className="mt-3 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                      <h5 className="text-sm font-medium text-blue-900 dark:text-blue-100 mb-1">
                        💡 Deep Research Options
                      </h5>
                      <p className="text-xs text-blue-800 dark:text-blue-200 mb-2">
                        You can pass execution options directly in the API call or configure them in the UI.
                      </p>
                      <div className="text-xs text-blue-800 dark:text-blue-200">
                        <strong>Available Options:</strong>
                        <ul className="mt-1 ml-4 space-y-1">
                          <li>• <strong>execution_mode:</strong> "sequential" (default), "parallel"</li>
                        </ul>
                        <div className="mt-2 p-2 bg-blue-100 dark:bg-blue-800/30 rounded">
                          <strong>Default:</strong> "sequential" execution
                        </div>
                      </div>
                    </div>
                  </div>
                )}

                {/* Workflow phases */}
                {selectedModeCategory === 'workflow' && examples.phases.length > 0 && (
                  <div>
                    <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                      Available Phases:
                    </h4>
                    <div className="bg-gray-50 dark:bg-gray-800 rounded-lg p-3 space-y-2">
                      {examples.phases.map((phase) => (
                        <div key={phase.id} className="flex items-start gap-2">
                          <span className={`px-2 py-1 text-xs rounded font-medium ${
                            phase.id === 'post-verification' 
                              ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' 
                              : 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300'
                          }`}>
                            {phase.id}
                          </span>
                          <div className="flex-1">
                            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
                              {phase.title}
                            </div>
                            <div className="text-xs text-gray-500 dark:text-gray-400">
                              {phase.description}
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                    
                    <div className="mt-3 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                      <h5 className="text-sm font-medium text-blue-900 dark:text-blue-100 mb-1">
                        💡 Workflow Options
                      </h5>
                      <p className="text-xs text-blue-800 dark:text-blue-200 mb-2">
                        You can pass execution options directly in the API call or configure them in the UI (stored in database).
                      </p>
                      <div className="text-xs text-blue-800 dark:text-blue-200">
                        <strong>Available Options:</strong>
                        <ul className="mt-1 ml-4 space-y-1">
                          <li>• <strong>run_management:</strong> "create_new_runs_always", "use_same_run", "create_new_run_once_daily"</li>
                          <li>• <strong>execution_strategy:</strong> "sequential_execution", "parallel_execution"</li>
                        </ul>
                        <div className="mt-2 p-2 bg-blue-100 dark:bg-blue-800/30 rounded">
                          <strong>Default:</strong> "create_new_runs_always" + "sequential_execution"
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </div>

            {/* 2. Poll Events */}
            <div>
              <div className="flex items-center gap-2 mb-3">
                <ExternalLink className="w-4 h-4 text-blue-600" />
                <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                  2. Poll for Events
                </h3>
              </div>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
                Use the observer_id to get real-time events from the execution.
              </p>
              
              <div className="space-y-3">
                <div>
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Request:
                  </h4>
                  <CodeBlock>{pollEventsExample}</CodeBlock>
                </div>
                
                <div>
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Response:
                  </h4>
                  <CodeBlock language="json">{pollEventsResponse}</CodeBlock>
                </div>
              </div>
            </div>

            {/* 3. Cancel Execution */}
            <div>
              <div className="flex items-center gap-2 mb-3">
                <Square className="w-4 h-4 text-red-600" />
                <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                  3. Cancel Execution
                </h3>
              </div>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
                Gracefully cancel an ongoing execution using the session_id.
              </p>
              
              <div className="space-y-3">
                <div>
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Request:
                  </h4>
                  <CodeBlock>{cancelExecutionExample}</CodeBlock>
                </div>
                
                <div>
                  <h4 className="text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                    Response:
                  </h4>
                  <CodeBlock language="json">{cancelExecutionResponse}</CodeBlock>
                </div>
              </div>
            </div>

            {/* 4. Complete Workflow */}
            <div>
              <div className="flex items-center gap-2 mb-3">
                <Play className="w-4 h-4 text-purple-600" />
                <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                  4. Complete {getModeName(selectedModeCategory || 'chat')} Connection
                </h3>
              </div>
              <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
                A complete bash script showing the full {selectedModeCategory === 'workflow' ? 'workflow' : selectedModeCategory === 'deep-research' ? 'deep research' : 'chat'} connection from start to completion.
                {selectedModeCategory === 'workflow' && ' Uses default options: "Create New Runs Always" and "Sequential Execution".'}
              </p>
              
              <CodeBlock>{completeWorkflowExample}</CodeBlock>
            </div>

            {/* Key Points */}
            <div className="bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
              <h3 className="font-semibold text-gray-900 dark:text-gray-100 mb-3">
                🔑 Key Points
              </h3>
              <ul className="text-sm text-gray-600 dark:text-gray-400 space-y-1">
                <li>• <strong>session_id</strong>: Persistent identifier stored in database for chat history</li>
                <li>• <strong>observer_id</strong>: Temporary identifier for real-time event polling</li>
                {selectedModeCategory === 'workflow' && (
                  <li>• <strong>execution_phase</strong>: Required for workflow mode (pre-verification for planning, post-verification for execution)</li>
                )}
                {selectedModeCategory === 'workflow' && (
                  <li>• <strong>options</strong>: Pass run_management and execution_strategy in API call or use UI defaults</li>
                )}
                {selectedModeCategory === 'deep-research' && (
                  <li>• <strong>options</strong>: Pass execution_mode (sequential/parallel) in API call or use UI defaults</li>
                )}
                <li>• <strong>Events</strong>: Real-time updates including tool calls, LLM responses, and completion status</li>
                <li>• <strong>Cancellation</strong>: Graceful shutdown that saves current state</li>
                {activePreset && (
                  <li>• <strong>Preset ID</strong>: "{activePreset.id}" - Use this exact ID in your API calls</li>
                )}
              </ul>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-3 p-6 border-t border-gray-200 dark:border-gray-700">
          <Button
            onClick={onClose}
            variant="outline"
            className="px-4 py-2"
          >
            Close
          </Button>
        </div>
      </div>
    </div>
  )
}
