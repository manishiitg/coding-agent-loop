import React, { useMemo, useState } from 'react'
import * as Select from '@radix-ui/react-select'
import { Check, ChevronDown, Calendar, DollarSign, Box, Search, X } from 'lucide-react'
import { cn } from '../../lib/utils' // Assuming this exists given 'clsx' and 'tailwind-merge' are in package.json
import type { ModelMetadata } from '../../services/llm-config-api'

// Helper to extract date from model ID for sorting
// Matches YYYYMMDD or YYYY-MM-DD patterns common in Anthropic/OpenAI/Vertext IDs
const extractDateFromModelID = (modelID: string): number => {
  // Try YYYYMMDD
  const matchCompact = modelID.match(/20\d{6}/)
  if (matchCompact) return parseInt(matchCompact[0])

  // Try YYYY-MM-DD
  const matchHyphen = modelID.match(/20\d{2}-\d{2}-\d{2}/)
  if (matchHyphen) return parseInt(matchHyphen[0].replace(/-/g, ''))

  return 0
}

interface ModelSelectorProps {
  value: string
  onChange: (value: string) => void
  models: string[] // List of model IDs
  metadata: ModelMetadata[]
  placeholder?: string
  className?: string
  disabled?: boolean
}

export function ModelSelector({
  value,
  onChange,
  models,
  metadata,
  placeholder = "Select a model...",
  className,
  disabled = false
}: ModelSelectorProps) {
  const [searchQuery, setSearchQuery] = useState('')
  const [open, setOpen] = useState(false)
  
  // Filter and sort models based on search query
  const filteredAndSortedModels = useMemo(() => {
    let filtered = [...models]
    
    // Filter by search query (model name or model ID)
    if (searchQuery.trim()) {
      const query = searchQuery.toLowerCase().trim()
      filtered = models.filter(modelId => {
        const meta = metadata.find(m => m.model_id === modelId)
        const modelName = meta?.model_name?.toLowerCase() || ''
        const modelIdLower = modelId.toLowerCase()
        
        return modelName.includes(query) || modelIdLower.includes(query)
      })
    }
    
    // Sort: Latest date first, then alphabetical
    return filtered.sort((a, b) => {
      const dateA = extractDateFromModelID(a)
      const dateB = extractDateFromModelID(b)

      if (dateA !== dateB) {
        return dateB - dateA // Descending (latest first)
      }
      return a.localeCompare(b)
    })
  }, [models, metadata, searchQuery])

  const getModelDetails = (modelId: string) => {
    return metadata.find(m => m.model_id === modelId)
  }

  // Format context window (e.g. 200000 -> 200k)
  const formatContext = (ctx: number) => {
    if (ctx >= 1000000) return `${(ctx / 1000000).toFixed(1)}M`
    return `${(ctx / 1000).toFixed(0)}k`
  }

  const handleValueChange = (newValue: string) => {
    onChange(newValue)
    setSearchQuery('') // Reset search when a model is selected
    setOpen(false)
  }

  const handleOpenChange = (isOpen: boolean) => {
    setOpen(isOpen)
    if (!isOpen) {
      setSearchQuery('') // Reset search when dropdown closes
    }
  }

  return (
    <Select.Root value={value} onValueChange={handleValueChange} disabled={disabled} open={open} onOpenChange={handleOpenChange}>
      <Select.Trigger 
        className={cn(
          "flex w-full items-center justify-between rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
      >
        <Select.Value placeholder={placeholder}>
          {value ? (
            <div className="flex flex-col items-start text-left w-full">
              <span className="font-medium">
                {getModelDetails(value)?.model_name || value}
              </span>
              {getModelDetails(value) && (
                <span className="text-xs text-muted-foreground flex items-center gap-2 flex-wrap">
                  <span className="flex items-center gap-1">
                    <Box className="w-3 h-3" />
                    {formatContext(getModelDetails(value)!.context_window)} ctx
                  </span>
                  <span>•</span>
                  <span className="flex items-center gap-1">
                    <DollarSign className="w-3 h-3" />
                    <span>${getModelDetails(value)!.input_cost_per_1m.toFixed(2)}/1M in</span>
                  </span>
                  <span>•</span>
                  <span className="flex items-center gap-1">
                    <span>${getModelDetails(value)!.output_cost_per_1m.toFixed(2)}/1M out</span>
                  </span>
                </span>
              )}
            </div>
          ) : (
            <span className="text-muted-foreground">{placeholder}</span>
          )}
        </Select.Value>
        <Select.Icon>
          <ChevronDown className="h-4 w-4 opacity-50" />
        </Select.Icon>
      </Select.Trigger>

      <Select.Portal>
        <Select.Content 
          className="relative z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md animate-in fade-in-80"
          position="popper"
          sideOffset={5}
        >
          {/* Search Input */}
          <div className="p-2 border-b border-border">
            <div className="relative flex items-center">
              <Search className="absolute left-2 w-4 h-4 text-muted-foreground pointer-events-none" />
              <input
                type="text"
                placeholder="Search models..."
                value={searchQuery}
                onChange={(e) => {
                  e.stopPropagation()
                  setSearchQuery(e.target.value)
                }}
                onClick={(e) => e.stopPropagation()}
                onKeyDown={(e) => {
                  e.stopPropagation()
                  // Prevent closing dropdown on Escape if there's search text
                  if (e.key === 'Escape' && searchQuery) {
                    e.preventDefault()
                    setSearchQuery('')
                  }
                }}
                className="w-full h-8 pl-8 pr-8 text-sm bg-background border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-0"
                autoFocus
              />
              {searchQuery && (
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    setSearchQuery('')
                  }}
                  className="absolute right-2 w-4 h-4 text-muted-foreground hover:text-foreground"
                >
                  <X className="w-4 h-4" />
                </button>
              )}
            </div>
          </div>
          
          <Select.Viewport className="p-1 max-h-[300px] overflow-y-auto">
            {filteredAndSortedModels.length === 0 ? (
              <div className="py-6 text-center text-sm text-muted-foreground">
                No models found matching "{searchQuery}"
              </div>
            ) : (
              filteredAndSortedModels.map((modelId) => {
              const meta = getModelDetails(modelId)
              const date = extractDateFromModelID(modelId)
              
              return (
                <Select.Item
                  key={modelId}
                  value={modelId}
                  className="relative flex w-full cursor-default select-none items-center rounded-sm py-2 pl-2 pr-8 text-sm outline-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
                >
                  <div className="flex flex-col w-full gap-0.5">
                    <div className="flex items-center justify-between w-full">
                      <span className="font-medium">{meta?.model_name || modelId}</span>
                      {date > 0 && (
                        <span className="text-[10px] text-muted-foreground bg-secondary/50 px-1.5 rounded flex items-center gap-0.5">
                          <Calendar className="w-3 h-3" />
                          {date.toString().slice(0, 4)}-{date.toString().slice(4, 6)}
                        </span>
                      )}
                    </div>
                    
                    {meta && (
                      <div className="flex items-center gap-3 text-xs text-muted-foreground flex-wrap">
                        <span className="flex items-center gap-1" title="Context Window">
                          <Box className="w-3 h-3" />
                          {formatContext(meta.context_window)}
                        </span>
                        <span className="flex items-center gap-1" title="Input Cost per 1M tokens">
                          <DollarSign className="w-3 h-3" />
                          <span className="font-medium">${meta.input_cost_per_1m.toFixed(2)}</span>
                          <span className="opacity-70">/1M in</span>
                        </span>
                        <span className="flex items-center gap-1" title="Output Cost per 1M tokens">
                          <DollarSign className="w-3 h-3 opacity-70" />
                          <span className="font-medium">${meta.output_cost_per_1m.toFixed(2)}</span>
                          <span className="opacity-70">/1M out</span>
                        </span>
                      </div>
                    )}
                  </div>

                  <span className="absolute right-2 flex h-3.5 w-3.5 items-center justify-center">
                    <Select.ItemIndicator>
                      <Check className="h-4 w-4" />
                    </Select.ItemIndicator>
                  </span>
                </Select.Item>
              )
            })
            )}
          </Select.Viewport>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  )
}
