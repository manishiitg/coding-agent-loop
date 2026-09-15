import React from 'react'
import {
  Loader2,
  AlertCircle,
  FileText,
  Route as RouteIcon,
  ArrowLeft,
} from 'lucide-react'
import type { RunFolderInfo, StepExecutionLogs } from '../../services/api-types'
import InspectorShell from './InspectorShell'
import { PulseReviewsPanel } from './executionLogs/LogPrimitives'
import { LogsHeader } from './executionLogs/LogsHeader'
import { StepContent } from './executionLogs/StepContent'
import { StepList } from './executionLogs/StepList'
import { useExecutionLogsData } from './executionLogs/useExecutionLogsData'

const headerRowClass = (embedded: boolean) =>
  `flex items-center justify-between gap-3 border-b border-border ${embedded ? 'px-3 py-2' : 'px-4 py-3 sm:px-6 sm:py-4'}`

interface ExecutionLogsPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  runFolder: string | null
  runFolders: string[] // Available run folders (iterations and groups)
  runFolderInfos?: RunFolderInfo[] // Lifecycle timestamps for dropdown labels
  startedAt?: string | null
  embedded?: boolean
  // Refreshes the run_folder LIST itself (a new folder appearing after a
  // standalone execute_step run, e.g.), as opposed to the panel's own
  // refresh, which only re-fetches logs for the already-selected folder.
  // Without this, a folder that didn't exist when runFolders was last loaded
  // stays invisible in the dropdown no matter how many times the panel's own
  // refresh is clicked. Optional: the standalone (non-embedded) popup has no
  // parent-owned folder list to refresh.
  onRefreshRunFolders?: () => void | Promise<void>
}

const ExecutionLogsPopup: React.FC<ExecutionLogsPopupProps> = ({
  isOpen,
  onClose,
  workspacePath,
  runFolder: initialRunFolder,
  runFolders,
  runFolderInfos = [],
  startedAt,
  embedded = false,
  onRefreshRunFolders
}) => {
  const {
    runFolderOptions,
    loading,
    logs,
    error,
    expandedSteps,
    expandedValidations,
    expandedExecutions,
    expandedArchived,
    selectedRunFolder,
    setSelectedRunFolder,
    stepSearchQueries,
    setStepSearchQueries,
    routeFilterKey,
    setRouteFilterKey,
    routingRouteGroups,
    expandedFiles,
    fileContents,
    loadingFiles,
    focusedStepId,
    stepDetailScrolled,
    handleStepDetailScroll,
    loadLogs,
    toggleStep,
    toggleValidation,
    toggleExecution,
    toggleArchived,
    toggleFileExpansion,
  } = useExecutionLogsData({ isOpen, workspacePath, initialRunFolder, runFolders })

  // Recursive render function for step content
  const renderStepContent = (stepId: string, stepLogs: StepExecutionLogs) => (
    <StepContent
      stepId={stepId}
      stepLogs={stepLogs}
      logs={logs}
      stepSearchQueries={stepSearchQueries}
      setStepSearchQueries={setStepSearchQueries}
      expandedValidations={expandedValidations}
      toggleValidation={toggleValidation}
      expandedExecutions={expandedExecutions}
      toggleExecution={toggleExecution}
      expandedArchived={expandedArchived}
      toggleArchived={toggleArchived}
      expandedFiles={expandedFiles}
      fileContents={fileContents}
      loadingFiles={loadingFiles}
      toggleFileExpansion={toggleFileExpansion}
    />
  )

  return (
    <InspectorShell
      embedded={embedded}
      isOpen={isOpen}
      onClose={onClose}
      embeddedClassName="bg-background flex flex-col border border-border relative h-full min-h-0 rounded-none border-0"
      modalClassName="bg-background flex flex-col border border-border relative rounded-lg shadow-xl w-full max-w-[calc(100vw-1rem)] sm:max-w-[90vw] h-[calc(100dvh-1rem)] sm:h-[95vh]"
      headerClassName={headerRowClass(embedded)}
      header={
        <LogsHeader
          embedded={embedded}
          startedAt={startedAt}
          runFolderOptions={runFolderOptions}
          runFolderInfos={runFolderInfos}
          selectedRunFolder={selectedRunFolder}
          setSelectedRunFolder={setSelectedRunFolder}
          loading={loading}
          loadLogs={loadLogs}
          onRefreshRunFolders={onRefreshRunFolders}
        />
      }
    >
        {/* Content */}
        <div
          className={`flex-1 overflow-y-auto bg-background ${embedded ? 'p-4' : 'p-6'}`}
          onScroll={focusedStepId ? handleStepDetailScroll : undefined}
        >
          {loading ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <Loader2 className="w-8 h-8 animate-spin mb-3 text-primary" />
              <p>Loading execution logs...</p>
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center py-12 text-destructive">
              <AlertCircle className="w-12 h-12 mb-3" />
              <p>{error}</p>
              <button 
                onClick={() => loadLogs()}
                className="mt-4 px-4 py-2 bg-destructive/10 text-destructive rounded-md hover:bg-destructive/20 transition-colors text-sm font-medium"
              >
                Retry
              </button>
            </div>
          ) : !selectedRunFolder ? (
            <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
              <FileText className="w-12 h-12 mb-3 opacity-50" />
              <p className="text-sm font-medium">Select an iteration or group to view logs</p>
              <p className="text-xs mt-2 opacity-70">
                {runFolderOptions.length > 0 
                  ? `Choose from ${runFolderOptions.length} available ${runFolderOptions.length === 1 ? 'run' : 'runs'} above.`
                  : 'No run folders available. Execute an automation to generate logs.'}
              </p>
            </div>
          ) : (
            <div className="space-y-4">
              {focusedStepId && (
                <div
                  className={`sticky top-0 z-20 -mx-1 flex items-center border-b border-border/80 bg-background/95 px-1 backdrop-blur-sm transition-[padding] duration-150 ${
                    stepDetailScrolled ? 'py-1' : 'pb-3 pt-1'
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => toggleStep(focusedStepId)}
                    className={`inline-flex items-center gap-2 rounded-md border border-border bg-card font-medium text-foreground shadow-sm transition-all duration-150 hover:bg-accent ${
                      stepDetailScrolled ? 'px-2 py-1 text-xs' : 'px-3 py-2 text-sm'
                    }`}
                  >
                    <ArrowLeft className={stepDetailScrolled ? 'h-3 w-3' : 'h-4 w-4'} />
                    Back to all steps
                  </button>
                </div>
              )}

              {/* Message when no step logs found */}
              {logs && Object.keys(logs.steps).length === 0 && (logs.pulse_reviews?.length || 0) === 0 && (
                <div className="flex flex-col items-center justify-center py-8 text-muted-foreground border border-dashed border-border rounded-lg">
                  <FileText className="w-10 h-10 mb-2 opacity-50" />
                  <p className="text-sm">No step execution logs found for <span className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">{selectedRunFolder}</span>.</p>
                  {runFolders.length > 1 && (
                    <p className="text-xs mt-2 opacity-70">
                      Try selecting a different iteration or group from the dropdown above.
                    </p>
                  )}
                </div>
              )}

              {!focusedStepId && <PulseReviewsPanel reviews={logs?.pulse_reviews || []} />}

              {!focusedStepId && routingRouteGroups.length > 0 && (
                <div className="flex flex-wrap items-center gap-1.5 pb-1">
                  <button
                    type="button"
                    onClick={() => setRouteFilterKey(null)}
                    className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium border transition-colors ${
                      routeFilterKey === null
                        ? 'bg-primary text-primary-foreground border-primary'
                        : 'bg-muted text-muted-foreground border-border hover:bg-accent'
                    }`}
                  >
                    All steps
                  </button>
                  {routingRouteGroups.map(group => (
                    <button
                      key={group.key}
                      type="button"
                      onClick={() => setRouteFilterKey(group.key)}
                      title={`Route "${group.routeName}" -- selected by ${group.routeStepTitle}`}
                      className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-xs font-medium border transition-colors ${
                        routeFilterKey === group.key
                          ? 'bg-teal-600 text-white border-teal-600'
                          : 'bg-muted text-muted-foreground border-border hover:bg-accent'
                      }`}
                    >
                      <RouteIcon className="h-3 w-3" />
                      {group.routeName}
                    </button>
                  ))}
                </div>
              )}

              <StepList
                logs={logs}
                focusedStepId={focusedStepId}
                routeFilterKey={routeFilterKey}
                expandedSteps={expandedSteps}
                toggleStep={toggleStep}
                renderStepContent={renderStepContent}
              />
            </div>
          )}
        </div>
    </InspectorShell>
  )
}

export default ExecutionLogsPopup
