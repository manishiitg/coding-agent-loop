import React from 'react'

interface Preset {
  id: string
  name: string
  description: string
}

interface WorkflowPresetSelectorProps {
  selectedPresetId: string
  availablePresets: Preset[]
  isCreatingWorkflow?: boolean
}

export const WorkflowPresetSelector: React.FC<WorkflowPresetSelectorProps> = ({
  selectedPresetId,
  availablePresets,
  isCreatingWorkflow = false
}) => {
  const selectedPreset = availablePresets.find(p => p.id === selectedPresetId)

  // Don't render if no preset is selected or found
  if (!selectedPresetId || !selectedPreset) {
    return null
  }

  return (
    <div className="mb-4 p-3 bg-card border border-border rounded-lg">
      {/* Compact preset display */}
      <div className="flex items-center gap-2">
        <div className="w-2 h-2 bg-success rounded-full"></div>
        <span className="text-sm font-medium text-foreground">
          {selectedPreset.name}
        </span>
      </div>

      {/* Loading indicator */}
      {isCreatingWorkflow && (
        <div className="flex items-center gap-2 text-xs text-muted-foreground mt-2">
          <div className="w-3 h-3 border-2 border-muted-foreground border-t-primary rounded-full animate-spin"></div>
          Generating automation plan...
        </div>
      )}
    </div>
  )
}
