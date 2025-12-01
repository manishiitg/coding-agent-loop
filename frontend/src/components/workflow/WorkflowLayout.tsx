import React, { useMemo, useCallback, useRef, useEffect } from 'react'
import { WorkflowCanvas, type WorkflowCanvasRef } from './canvas'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'
import { useChatStore } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import ChatArea, { type ChatAreaRef } from '../ChatArea'
import { ChatHeader } from '../ChatHeader'
import { MessageSquare, X } from 'lucide-react'
import { agentApi } from '../../services/api'
import { type ExecutionOptions } from '../../services/api-types'

interface WorkflowLayoutProps {
  className?: string
  onCreatePlan?: () => void
  onNewChat: () => void
}

/**
 * Main layout component for workflow mode
 * Shows React Flow canvas as the main area with ChatArea appearing when a phase is started
 * Uses useWorkflowStore for activePhase and showChatArea state (single source of truth)
 */
export const WorkflowLayout: React.FC<WorkflowLayoutProps> = ({
  className = '',
  onCreatePlan,
  onNewChat
}) => {
  const { selectedModeCategory, setModeCategory } = useModeStore()
  const { currentWorkflowPhase, setCurrentWorkflowPhase, events } = useChatStore()
  
  // Use workflow store for UI state (single source of truth)
  const activePhase = useWorkflowStore(state => state.activePhase)
  const showChatArea = useWorkflowStore(state => state.showChatArea)
  const setActivePhase = useWorkflowStore(state => state.setActivePhase)
  const setShowChatArea = useWorkflowStore(state => state.setShowChatArea)
  
  // Ref for the ChatArea component
  const chatAreaRef = useRef<ChatAreaRef>(null)
  // Ref for the WorkflowCanvas component (for triggering refresh)
  const canvasRef = useRef<WorkflowCanvasRef>(null)
  // Track the last processed event index to avoid duplicate refreshes
  const lastProcessedEventIndexRef = useRef(-1)
  
  // Get active workflow preset
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  
  const activeWorkflowPreset = useMemo(() => {
    if (selectedModeCategory === 'workflow' && activePresetId) {
      const customPreset = customPresets.find(p => p.id === activePresetId)
      if (customPreset) return customPreset
      
      const predefinedPreset = predefinedPresets.find(p => p.id === activePresetId)
      if (predefinedPreset) return predefinedPreset
    }
    return null
  }, [selectedModeCategory, activePresetId, customPresets, predefinedPresets])

  // Get workspace path from active preset
  const workspacePath = useMemo(() => {
    if (activeWorkflowPreset?.selectedFolder?.filepath) {
      return activeWorkflowPreset.selectedFolder.filepath
    }
    return null
  }, [activeWorkflowPreset])

  // Listen for todo_steps_extracted events to auto-refresh the canvas
  useEffect(() => {
    if (events.length === 0) return
    
    console.log(`[WorkflowPlanUpdate] Processing events (total: ${events.length}, last processed: ${lastProcessedEventIndexRef.current})`)
    
    // Find new todo_steps_extracted events that we haven't processed yet
    for (let i = lastProcessedEventIndexRef.current + 1; i < events.length; i++) {
      const event = events[i]
      console.log(`[WorkflowPlanUpdate] Event ${i}: type=${event.type}, timestamp=${event.timestamp}`)
      
      if (event.type === 'todo_steps_extracted') {
        const eventData = event.data as { extracted_steps?: unknown[], total_steps_extracted?: number, plan_source?: string, extraction_method?: string, workspace_path?: string }
        const stepCount = (eventData?.extracted_steps?.length) || eventData?.total_steps_extracted || 0
        const planSource = eventData?.plan_source || 'unknown'
        const extractionMethod = eventData?.extraction_method || 'unknown'
        
        console.log(`[WorkflowPlanUpdate] Detected plan update event:`, {
          stepCount,
          planSource,
          extractionMethod,
          workspacePath: eventData?.workspace_path,
          eventIndex: i
        })
        
        // Trigger canvas refresh with change detection
        if (canvasRef.current) {
          console.log('[WorkflowPlanUpdate] Calling canvasRef.current.refresh()')
          canvasRef.current.refresh().then((changes) => {
            console.log('[WorkflowPlanUpdate] Canvas refresh completed, changes:', changes)
          }).catch((err) => {
            console.error('[WorkflowPlanUpdate] Canvas refresh failed:', err)
          })
        } else {
          console.warn('[WorkflowPlanUpdate] canvasRef.current is null, cannot refresh')
        }
        lastProcessedEventIndexRef.current = i
      }
    }
  }, [events])

  // Handle phase start from toolbar (now accepts execution options directly)
  const handleStartPhase = useCallback(async (phaseId: string, executionOptions?: ExecutionOptions) => {
    console.log('[WorkflowLayout] Starting phase:', phaseId, executionOptions ? 'with options' : '')
    
    if (!activePresetId) {
      console.error('[WorkflowLayout] No active preset ID, cannot start phase')
      return
    }
    
    // Reset previous events before starting new phase
    if (chatAreaRef?.current?.resetChatState) {
      console.log('[WorkflowLayout] Clearing previous events for new phase')
      chatAreaRef.current.resetChatState()
    }
    
    // Reset the event tracking index since we cleared events
    lastProcessedEventIndexRef.current = -1
    
    // Update workflow status in database BEFORE submitting query
    // The backend reads the phase from the database, not from the query string
    try {
      console.log('[WorkflowLayout] Updating workflow status to phase:', phaseId)
      await agentApi.updateWorkflow(activePresetId, phaseId, null, undefined)
      console.log('[WorkflowLayout] Workflow status updated successfully')
    } catch (error) {
      console.error('[WorkflowLayout] Failed to update workflow status:', error)
      // Continue anyway - the backend might still work with the query
    }
    
    setActivePhase(phaseId)
    setCurrentWorkflowPhase(phaseId)
    setShowChatArea(true) // Show ChatArea when starting a phase
    
    // Build query for the phase
    const query = `Execute workflow phase: ${phaseId}`
    
    // Submit query through ChatArea with execution options as separate parameter
    if (chatAreaRef?.current?.submitQuery) {
      if (executionOptions) {
        console.log('[WorkflowLayout] Execution options:', executionOptions)
      }
      await chatAreaRef.current.submitQuery(query, executionOptions)
    }
  }, [activePresetId, setCurrentWorkflowPhase, setActivePhase, setShowChatArea])

  // Handle closing ChatArea
  const handleCloseChatArea = useCallback(() => {
    setShowChatArea(false)
    setActivePhase(null)
  }, [setShowChatArea, setActivePhase])

  // No preset selected state
  if (!activeWorkflowPreset) {
    return (
      <div className={`flex flex-col h-full ${className}`}>
        {/* Header */}
        <ChatHeader
          chatSessionTitle=""
          chatSessionId=""
          sessionState="active"
          onModeSelect={(category) => setModeCategory(category)}
        />
        
        <div className="flex-1 flex items-center justify-center bg-gray-50 dark:bg-gray-900">
        <div className="flex flex-col items-center gap-4 text-center max-w-md">
            <div className="w-20 h-20 rounded-full bg-gray-200 dark:bg-gray-700 flex items-center justify-center">
            <span className="text-4xl">🚀</span>
          </div>
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">
              Select a Workflow
            </h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-2">
              Choose a workflow preset from the sidebar to get started. 
              The workflow canvas will visualize your plan and let you run it step by step.
            </p>
            </div>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`flex flex-col h-full ${className}`}>
      {/* Header */}
      <ChatHeader
        chatSessionTitle={activeWorkflowPreset?.label || ''}
        chatSessionId=""
        sessionState="active"
        onModeSelect={(category) => setModeCategory(category)}
      />

      {/* Main Content */}
      <div className="flex-1 flex min-h-0">
        {/* Canvas - main area, shrinks when ChatArea is shown */}
        <div className={`flex-1 min-w-0 transition-all duration-300 ${showChatArea ? 'w-1/2' : ''}`}>
          <WorkflowCanvas
            ref={canvasRef}
            workspacePath={workspacePath}
            presetQueryId={activePresetId}
            currentPhase={activePhase || currentWorkflowPhase}
            onStartPhase={handleStartPhase}
            onCreatePlan={onCreatePlan}
            className="h-full"
          />
        </div>

        {/* ChatArea Panel - single instance, show/hide via CSS */}
        <div className={`${showChatArea ? 'w-1/2' : 'w-0 overflow-hidden'} border-l border-gray-200 dark:border-gray-700 flex flex-col bg-white dark:bg-gray-900 relative transition-all duration-300`}>
          {/* Close button overlay - only visible when panel is open */}
          {showChatArea && (
            <button
              onClick={handleCloseChatArea}
              className="absolute top-2 right-2 z-10 p-1.5 rounded-full bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-500 dark:text-gray-400 shadow-sm"
              title="Close chat panel"
            >
              <X className="w-4 h-4" />
            </button>
          )}
          
          {/* Single ChatArea component - always mounted, visibility controlled by CSS */}
          <ChatArea
            ref={chatAreaRef}
            onNewChat={onNewChat}
            hideHeader
            hideInput
            compact
          />
        </div>

        {/* Toggle ChatArea Button (when hidden) */}
        {!showChatArea && (
          <button
            onClick={() => setShowChatArea(true)}
            className="fixed bottom-4 right-4 p-3 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-full shadow-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors z-40"
            title="Show Chat"
          >
            <MessageSquare className="w-5 h-5" />
          </button>
        )}
      </div>
    </div>
  )
}

export default WorkflowLayout
