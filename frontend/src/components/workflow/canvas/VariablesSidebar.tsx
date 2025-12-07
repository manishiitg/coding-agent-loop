import React, { useState, useEffect, useCallback } from 'react'
import { X, Plus, Trash2, Save, Check, Circle, Loader2, AlertCircle } from 'lucide-react'
import { agentApi } from '../../../services/api'
import type { VariablesManifest, VariableGroup } from '../../../services/api-types'

interface VariablesSidebarProps {
  workspacePath: string | null
  onClose: () => void
  onUpdate?: (manifest: VariablesManifest) => void
}

export const VariablesSidebar: React.FC<VariablesSidebarProps> = ({
  workspacePath,
  onClose,
  onUpdate
}) => {
  const [manifest, setManifest] = useState<VariablesManifest | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [hasChanges, setHasChanges] = useState(false)

  // Load variable groups
  useEffect(() => {
    if (!workspacePath) {
      setIsLoading(false)
      return
    }

    const loadVariables = async () => {
      setIsLoading(true)
      setError(null)
      try {
        const response = await agentApi.getVariableGroups(workspacePath)
        if (response.success && response.manifest) {
          setManifest(response.manifest)
        } else {
          setManifest(null)
        }
      } catch (err) {
        console.error('[VariablesSidebar] Failed to load variables:', err)
        setError('Failed to load variables')
      } finally {
        setIsLoading(false)
      }
    }

    loadVariables()
  }, [workspacePath])

  // Get groups (handling single-group backward compatibility)
  const getGroups = useCallback((): VariableGroup[] => {
    if (!manifest) return []
    // If groups exist, use them directly
    if (manifest.groups && manifest.groups.length > 0) {
      return manifest.groups
    }
    // Single group mode - create virtual group from variables
    // Only create virtual group if we have variables but no groups
    if (manifest.variables && manifest.variables.length > 0) {
      const values: Record<string, string> = {}
      manifest.variables.forEach(v => {
        values[v.name] = v.value || ''
      })
      return [{
        group_id: 'group-1',
        values,
        enabled: true
      }]
    }
    return []
  }, [manifest])

  const groups = getGroups()

  // Add new group
  const handleAddGroup = useCallback(() => {
    if (!manifest) return
    
    // If this is the first group being added, migrate existing values to group-1
    const isFirstGroup = !manifest.groups || manifest.groups.length === 0
    
    let newGroupId: string
    let updatedGroups: VariableGroup[]
    
    if (isFirstGroup) {
      // Migrate existing values to group-1, new group will be group-2
      const existingValues: Record<string, string> = {}
      manifest.variables.forEach(v => {
        existingValues[v.name] = v.value || ''
      })
      
      const newValues: Record<string, string> = {}
      manifest.variables.forEach(v => {
        newValues[v.name] = '' // Empty values for new group
      })
      
      newGroupId = 'group-2'
      updatedGroups = [
        { group_id: 'group-1', values: existingValues, enabled: true },
        { group_id: newGroupId, values: newValues, enabled: true }
      ]
    } else {
      // Add to existing groups
      const existingGroups = manifest.groups || []
      newGroupId = `group-${existingGroups.length + 1}`
      const newValues: Record<string, string> = {}
      manifest.variables.forEach(v => {
        newValues[v.name] = '' // Empty values for new group
      })
      
      updatedGroups = [
        ...existingGroups,
        { group_id: newGroupId, values: newValues, enabled: true }
      ]
    }
    
    const updatedManifest: VariablesManifest = {
      ...manifest,
      groups: updatedGroups
    }
    
    setManifest(updatedManifest)
    setHasChanges(true)
    // Update parent immediately so node UI reflects the change
    if (onUpdate) {
      onUpdate(updatedManifest)
    }
  }, [manifest, onUpdate])

  // Toggle group enabled/disabled
  const handleToggleGroup = useCallback((groupId: string) => {
    if (!manifest?.groups) return
    
    const updatedGroups = manifest.groups.map(g => 
      g.group_id === groupId ? { ...g, enabled: !g.enabled } : g
    )
    
    const updatedManifest = { ...manifest, groups: updatedGroups }
    setManifest(updatedManifest)
    setHasChanges(true)
    // Update parent immediately so node UI reflects the change
    if (onUpdate) {
      onUpdate(updatedManifest)
    }
  }, [manifest, onUpdate])

  // Delete group
  const handleDeleteGroup = useCallback((groupId: string) => {
    if (!manifest?.groups || manifest.groups.length <= 1) return
    
    const updatedGroups = manifest.groups.filter(g => g.group_id !== groupId)
    const updatedManifest = { ...manifest, groups: updatedGroups }
    setManifest(updatedManifest)
    setHasChanges(true)
    // Update parent immediately so node UI reflects the change
    if (onUpdate) {
      onUpdate(updatedManifest)
    }
  }, [manifest, onUpdate])

  // Update variable value in single-group mode
  const handleUpdateValue = useCallback((variableName: string, newValue: string) => {
    if (!manifest) return
    
    // Single-group mode - update Variables[].value directly
    const updatedVariables = manifest.variables.map(v => 
      v.name === variableName ? { ...v, value: newValue } : v
    )
    const updatedManifest = { ...manifest, variables: updatedVariables }
    setManifest(updatedManifest)
    setHasChanges(true)
    if (onUpdate) {
      onUpdate(updatedManifest)
    }
  }, [manifest, onUpdate])

  // Save changes
  const handleSave = useCallback(async () => {
    if (!manifest || !workspacePath || !hasChanges) return
    
    setIsSaving(true)
    setError(null)
    try {
      const response = await agentApi.updateVariableGroups(workspacePath, manifest)
      if (!response.success) {
        throw new Error(response.message || 'Failed to save changes')
      }
      setHasChanges(false)
      if (onUpdate) {
        onUpdate(manifest)
      }
    } catch (err) {
      console.error('[VariablesSidebar] Failed to save:', err)
      let errorMessage = 'Failed to save changes'
      if (err instanceof Error) {
        errorMessage = err.message
      } else if (typeof err === 'object' && err !== null && 'response' in err) {
        const axiosError = err as { response?: { data?: { message?: string } } }
        errorMessage = axiosError.response?.data?.message || errorMessage
      }
      setError(errorMessage)
    } finally {
      setIsSaving(false)
    }
  }, [manifest, workspacePath, hasChanges, onUpdate])


  // Count enabled groups
  const enabledCount = groups.filter(g => g.enabled).length
  const isMultiGroup = groups.length > 1

  return (
    <div className="absolute right-0 top-0 bottom-0 w-[450px] bg-background border-l border-border flex flex-col shadow-lg z-50">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-background">
        <div className="flex items-center gap-2">
          <h2 className="text-base font-semibold text-foreground">Variables</h2>
          {isMultiGroup && (
            <span className="text-xs text-muted-foreground bg-muted px-2 py-0.5 rounded">
              {enabledCount}/{groups.length} groups enabled
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {hasChanges && (
            <button
              onClick={handleSave}
              disabled={isSaving}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded
                       bg-purple-600 dark:bg-purple-500 text-white hover:bg-purple-700 dark:hover:bg-purple-600 
                       disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
            >
              {isSaving ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Save className="w-3.5 h-3.5" />
              )}
              Save
            </button>
          )}
          <button
            onClick={onClose}
            className="p-1.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Error display */}
      {error && (
        <div className="mx-4 mt-3 p-3 rounded-md bg-destructive/10 border border-destructive/20 text-destructive dark:text-destructive text-sm flex items-center gap-2">
          <AlertCircle className="w-4 h-4 flex-shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Loading state */}
      {isLoading ? (
        <div className="flex-1 flex items-center justify-center">
          <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
        </div>
      ) : !manifest || manifest.variables.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center text-center p-4">
          <p className="text-muted-foreground mb-2">No variables extracted yet</p>
          <p className="text-xs text-muted-foreground">
            Run the Variable Extraction phase first
          </p>
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto flex flex-col">
          {/* Add group button */}
          <div className="px-4 py-2 border-b border-border flex-shrink-0">
            <button
              onClick={handleAddGroup}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium
                       bg-muted text-muted-foreground hover:bg-muted/80 transition-colors w-full justify-center"
            >
              <Plus className="w-3.5 h-3.5" />
              {isMultiGroup ? 'Add Group' : 'Add Variable Group'}
            </button>
            {!isMultiGroup && (
              <p className="text-xs text-muted-foreground mt-1.5 text-center">
                Add multiple groups to run workflow with different values
              </p>
            )}
          </div>

          {/* All groups list */}
          <div className="p-4 space-y-4">
            {groups.map((group, index) => (
              <div
                key={`${group.group_id}-${index}`}
                className={`border rounded-lg p-3 space-y-3 ${
                  !group.enabled ? 'opacity-60 border-border/50' : 'border-border'
                }`}
              >
                {/* Group header */}
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <button
                      onClick={() => handleToggleGroup(group.group_id)}
                      className="flex items-center gap-1.5 hover:scale-110 transition-transform"
                      title={group.enabled ? 'Disable group' : 'Enable group'}
                    >
                      {group.enabled ? (
                        <Check className="w-4 h-4 text-green-600 dark:text-green-400" />
                      ) : (
                        <Circle className="w-4 h-4 text-gray-400 dark:text-gray-500" />
                      )}
                    </button>
                    <span className="text-sm font-semibold text-foreground font-mono">
                      {group.group_id.toUpperCase()}
                    </span>
                    {!group.enabled && (
                      <span className="text-xs text-muted-foreground">(Disabled)</span>
                    )}
                  </div>
                  {groups.length > 1 && (
                    <button
                      onClick={() => handleDeleteGroup(group.group_id)}
                      className="p-1 hover:text-red-500 transition-colors"
                      title="Delete group"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  )}
                </div>

                {/* Variables for this group */}
                <div className="space-y-2">
                  {manifest.variables.map((variable) => (
                    <div key={variable.name} className="space-y-1">
                      <label className="flex items-center gap-2 text-xs font-medium text-foreground">
                        <span className="font-mono text-purple-600 dark:text-purple-400">
                          {variable.name}
                        </span>
                        {variable.description && (
                          <span className="text-xs text-muted-foreground font-normal">
                            ({variable.description})
                          </span>
                        )}
                      </label>
                      <input
                        type="text"
                        value={group.values[variable.name] || ''}
                        onChange={(e) => {
                          // Update value for this specific group
                          if (manifest.groups && manifest.groups.length > 0) {
                            const updatedGroups = manifest.groups.map(g => {
                              if (g.group_id === group.group_id) {
                                return { ...g, values: { ...g.values, [variable.name]: e.target.value } }
                              }
                              return g
                            })
                            const updatedManifest = { ...manifest, groups: updatedGroups }
                            setManifest(updatedManifest)
                            setHasChanges(true)
                            if (onUpdate) {
                              onUpdate(updatedManifest)
                            }
                          } else {
                            // Single group mode
                            handleUpdateValue(variable.name, e.target.value)
                          }
                        }}
                        disabled={!group.enabled}
                        className="w-full px-3 py-2 rounded border border-border bg-muted/30 text-foreground text-sm
                                 focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary focus:bg-background
                                 font-mono placeholder:text-muted-foreground/50 disabled:opacity-50 disabled:cursor-not-allowed"
                        placeholder={`Enter value for ${variable.name}`}
                      />
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>

          {/* Objective preview */}
          {manifest.objective && (
            <div className="px-4 py-3 border-t border-border bg-background">
              <div className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1.5">
                Templated Objective
              </div>
              <p className="text-xs text-foreground bg-muted/50 p-2 rounded font-mono whitespace-pre-wrap">
                {manifest.objective}
              </p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

