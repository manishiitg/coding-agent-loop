import { useState, useRef } from 'react'
import { Download, Upload, Loader2 } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { agentApi } from '../../services/api'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useModeStore } from '../../stores/useModeStore'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'

interface WorkflowBackupSectionProps {
  minimized?: boolean
}

export default function WorkflowBackupSection({
  minimized = false,
}: WorkflowBackupSectionProps) {
  const { selectedModeCategory } = useModeStore()
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)
  const { fetchFiles } = useWorkspaceStore()
  
  const [isExporting, setIsExporting] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [exportError, setExportError] = useState<string | null>(null)
  const [importError, setImportError] = useState<string | null>(null)
  const [importSuccess, setImportSuccess] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // Get active workflow preset
  const activeWorkflowPreset = customPresets.find(p => p.id === activePresetId) ||
    predefinedPresets.find(p => p.id === activePresetId)

  // Only show in workflow mode with an active preset
  if (selectedModeCategory !== 'workflow' || !activeWorkflowPreset?.selectedFolder?.filepath) {
    return null
  }

  const workspacePath = activeWorkflowPreset.selectedFolder.filepath

  const handleExport = async () => {
    setIsExporting(true)
    setExportError(null)

    try {
      const blob = await agentApi.exportWorkflowBackup(workspacePath)
      
      // Create download link
      const url = window.URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      
      // Generate filename
      const workspaceName = workspacePath.split('/').pop() || 'workspace'
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, -5)
      link.download = `${workspaceName}-backup-${timestamp}.zip`
      
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      window.URL.revokeObjectURL(url)
    } catch (error) {
      console.error('Export failed:', error)
      setExportError(error instanceof Error ? error.message : 'Failed to export backup')
    } finally {
      setIsExporting(false)
    }
  }

  const handleImportClick = () => {
    fileInputRef.current?.click()
  }

  const handleImport = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    if (!file) return

    // Validate file type
    if (!file.name.endsWith('.zip')) {
      setImportError('Please select a ZIP file')
      return
    }

    setIsImporting(true)
    setImportError(null)
    setImportSuccess(null)

    try {
      // Ask for confirmation
      const overwrite = window.confirm(
        'This will restore the workspace from the backup. Existing files may be overwritten. Continue?'
      )

      const result = await agentApi.importWorkflowBackup(workspacePath, file, overwrite)
      
      if (result.success) {
        setImportSuccess(`Successfully imported ${result.data?.files_extracted || 0} files`)
        
        // Refresh workspace files
        setTimeout(() => {
          fetchFiles().catch(console.error)
        }, 500)
      } else {
        setImportError(result.message || 'Import failed')
      }
    } catch (error) {
      console.error('Import failed:', error)
      setImportError(error instanceof Error ? error.message : 'Failed to import backup')
    } finally {
      setIsImporting(false)
      // Reset file input
      if (fileInputRef.current) {
        fileInputRef.current.value = ''
      }
    }
  }

  if (minimized) {
    return (
      <TooltipProvider>
        <div className="flex flex-col gap-2">
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  handleExport()
                }}
                disabled={isExporting}
                className="p-2 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                title="Export Backup"
              >
                {isExporting ? (
                  <Loader2 className="w-5 h-5 animate-spin" />
                ) : (
                  <Download className="w-5 h-5" />
                )}
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Export Workspace Backup</p>
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  handleImportClick()
                }}
                disabled={isImporting}
                className="p-2 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                title="Import Backup"
              >
                {isImporting ? (
                  <Loader2 className="w-5 h-5 animate-spin" />
                ) : (
                  <Upload className="w-5 h-5" />
                )}
              </button>
            </TooltipTrigger>
            <TooltipContent>
              <p>Import Workspace Backup</p>
            </TooltipContent>
          </Tooltip>
        </div>
      </TooltipProvider>
    )
  }

  return (
    <TooltipProvider>
      <div className="space-y-2">
        {/* Header */}
        <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
          <Download className="w-4 h-4" />
          Workspace Backup
        </h3>

        {/* Content */}
        <div className="space-y-2">
          {/* Export Button */}
          <button
            onClick={handleExport}
            disabled={isExporting}
            className="w-full flex items-center justify-center gap-2 px-3 py-2 text-sm bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {isExporting ? (
              <>
                <Loader2 className="w-4 h-4 animate-spin" />
                Exporting...
              </>
            ) : (
              <>
                <Download className="w-4 h-4" />
                Export Backup
              </>
            )}
          </button>

          {/* Import Button */}
          <div>
            <input
              ref={fileInputRef}
              type="file"
              accept=".zip"
              onChange={handleImport}
              className="hidden"
            />
            <button
              onClick={handleImportClick}
              disabled={isImporting}
              className="w-full flex items-center justify-center gap-2 px-3 py-2 text-sm bg-secondary text-secondary-foreground rounded-md hover:bg-secondary/90 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isImporting ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Importing...
                </>
              ) : (
                <>
                  <Upload className="w-4 h-4" />
                  Import Backup
                </>
              )}
            </button>
          </div>

          {/* Error Messages */}
          {exportError && (
            <div className="text-xs text-destructive bg-destructive/10 p-2 rounded">
              Export Error: {exportError}
            </div>
          )}

          {importError && (
            <div className="text-xs text-destructive bg-destructive/10 p-2 rounded">
              Import Error: {importError}
            </div>
          )}

          {importSuccess && (
            <div className="text-xs text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/20 p-2 rounded">
              {importSuccess}
            </div>
          )}
        </div>
      </div>
    </TooltipProvider>
  )
}



