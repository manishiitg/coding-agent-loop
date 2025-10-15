import React, { forwardRef, useImperativeHandle, useState, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/Card'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Info, Zap, Clock, CheckCircle } from 'lucide-react'
import { useAppStore } from '@/stores'

export interface OrchestratorModeHandlerRef {
  getSelectedExecutionMode: () => OrchestratorExecutionMode
  resetSelection: () => void
}

export interface OrchestratorModeHandlerProps {
  onExecutionModeChange?: (mode: OrchestratorExecutionMode) => void
  children?: React.ReactNode
}

export type OrchestratorExecutionMode = 'sequential_execution' | 'parallel_execution'

export const OrchestratorModeHandler = forwardRef<OrchestratorModeHandlerRef, OrchestratorModeHandlerProps>(({
  onExecutionModeChange,
  children
}, ref) => {
  const { agentMode } = useAppStore()
  const [selectedMode, setSelectedMode] = useState<OrchestratorExecutionMode>('sequential_execution')

  const handleModeChange = useCallback((mode: OrchestratorExecutionMode) => {
    setSelectedMode(mode)
    onExecutionModeChange?.(mode)
  }, [onExecutionModeChange])

  const getSelectedExecutionMode = useCallback(() => {
    return selectedMode
  }, [selectedMode])

  const resetSelection = useCallback(() => {
    setSelectedMode('sequential_execution')
    onExecutionModeChange?.('sequential_execution')
  }, [onExecutionModeChange])

  // Expose methods through ref
  useImperativeHandle(ref, () => ({
    getSelectedExecutionMode,
    resetSelection
  }), [getSelectedExecutionMode, resetSelection])

  // Show orchestrator components when in orchestrator mode
  if (agentMode === 'orchestrator') {
    return (
      <div className="space-y-4">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Zap className="h-5 w-5" />
              Orchestrator Execution Mode
            </CardTitle>
            <CardDescription>
              Choose how the orchestrator should execute your tasks
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {/* Sequential Execution Option */}
              <div 
                className={`flex items-start space-x-3 p-4 border rounded-lg transition-colors cursor-pointer ${
                  selectedMode === 'sequential_execution' 
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-950/20' 
                    : 'hover:bg-muted/50'
                }`}
                onClick={() => handleModeChange('sequential_execution')}
              >
                <input 
                  type="radio" 
                  id="sequential" 
                  name="executionMode"
                  value="sequential_execution"
                  checked={selectedMode === 'sequential_execution'}
                  onChange={(e) => handleModeChange(e.target.value as OrchestratorExecutionMode)}
                  className="mt-1"
                />
                <div className="flex-1 space-y-2">
                  <Label htmlFor="sequential" className="flex items-center gap-2 cursor-pointer">
                    <Clock className="h-4 w-4" />
                    Sequential Execution
                    <Badge variant="secondary" className="text-xs">
                      Default
                    </Badge>
                  </Label>
                  <p className="text-sm text-muted-foreground ml-6">
                    Execute tasks one by one in order. More reliable and easier to debug.
                    Best for complex workflows with dependencies.
                  </p>
                  <div className="ml-6 space-y-1">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <CheckCircle className="h-3 w-3" />
                      Step-by-step execution
                    </div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <CheckCircle className="h-3 w-3" />
                      Better error handling
                    </div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <CheckCircle className="h-3 w-3" />
                      Easier debugging
                    </div>
                  </div>
                </div>
              </div>

              {/* Parallel Execution Option */}
              <div 
                className={`flex items-start space-x-3 p-4 border rounded-lg transition-colors cursor-pointer ${
                  selectedMode === 'parallel_execution' 
                    ? 'border-blue-500 bg-blue-50 dark:bg-blue-950/20' 
                    : 'hover:bg-muted/50'
                }`}
                onClick={() => handleModeChange('parallel_execution')}
              >
                <input 
                  type="radio" 
                  id="parallel" 
                  name="executionMode"
                  value="parallel_execution"
                  checked={selectedMode === 'parallel_execution'}
                  onChange={(e) => handleModeChange(e.target.value as OrchestratorExecutionMode)}
                  className="mt-1"
                />
                <div className="flex-1 space-y-2">
                  <Label htmlFor="parallel" className="flex items-center gap-2 cursor-pointer">
                    <Zap className="h-4 w-4" />
                    Parallel Execution
                    <Badge variant="outline" className="text-xs">
                      Experimental
                    </Badge>
                  </Label>
                  <p className="text-sm text-muted-foreground ml-6">
                    Execute independent tasks simultaneously for faster completion.
                    Uses AI to identify tasks that can run in parallel.
                  </p>
                  <div className="ml-6 space-y-1">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <CheckCircle className="h-3 w-3" />
                      Faster execution
                    </div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <CheckCircle className="h-3 w-3" />
                      AI-powered dependency analysis
                    </div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <CheckCircle className="h-3 w-3" />
                      Up to 3 parallel tasks
                    </div>
                  </div>
                </div>
              </div>
            </div>

            {/* Info Section */}
            <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-950/20 rounded-lg border border-blue-200 dark:border-blue-800">
              <div className="flex items-start gap-2">
                <Info className="h-4 w-4 text-blue-600 dark:text-blue-400 mt-0.5" />
                <div className="text-sm text-blue-800 dark:text-blue-200">
                  <p className="font-medium">How it works:</p>
                  <ul className="mt-1 space-y-1 text-xs">
                    <li>• <strong>Sequential:</strong> Tasks run one after another, ensuring proper order</li>
                    <li>• <strong>Parallel:</strong> AI analyzes your request and runs independent tasks simultaneously</li>
                    <li>• You can switch modes anytime before starting execution</li>
                  </ul>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        {children}
      </div>
    )
  }

  // For non-orchestrator modes, just render children
  return <>{children}</>
})

OrchestratorModeHandler.displayName = 'OrchestratorModeHandler'