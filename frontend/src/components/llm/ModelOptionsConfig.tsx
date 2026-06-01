import type { ModelMetadata } from '../../services/llm-config-api'

interface ModelOptionsConfigProps {
  metadata?: ModelMetadata
  options: Record<string, unknown>
  onChange: (opts: Record<string, unknown>) => void
  disabled?: boolean
}

export function ModelOptionsConfig({ metadata, options, onChange, disabled }: ModelOptionsConfigProps) {
  const hasModelOptions = !!(
    metadata?.supports_reasoning_effort ||
    metadata?.supports_thinking_level ||
    metadata?.supports_thinking_budget
  )

  const updateOption = (key: string, value: string | number) => {
    onChange({ ...options, [key]: value })
  }

  if (!hasModelOptions) {
    return null
  }

  return (
    <div className="space-y-3 mt-4 border-t border-border pt-4">
      <h5 className="text-sm font-medium text-foreground">Model Options</h5>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        {metadata && metadata.supports_reasoning_effort && (
          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1">Reasoning Effort</label>
            <select 
              value={(options.reasoning_effort as string) || 'medium'} 
              onChange={(e) => updateOption('reasoning_effort', e.target.value)}
              disabled={disabled}
              className="w-full px-2 py-1.5 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary focus:border-primary disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {(metadata.reasoning_effort_levels || ['low', 'medium', 'high']).map(level => (
                <option key={level} value={level}>{level.charAt(0).toUpperCase() + level.slice(1)}</option>
              ))}
            </select>
          </div>
        )}
        
        {metadata?.supports_thinking_level && (
          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1">Thinking Level</label>
            <select 
              value={(options.thinking_level as string) || 'high'} 
              onChange={(e) => updateOption('thinking_level', e.target.value)}
              disabled={disabled}
              className="w-full px-2 py-1.5 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary focus:border-primary disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {(metadata.thinking_levels || ['low', 'high']).map(level => (
                <option key={level} value={level}>{level.charAt(0).toUpperCase() + level.slice(1)}</option>
              ))}
            </select>
          </div>
        )}

        {metadata?.supports_thinking_budget && (
          <div>
            <label className="block text-xs font-medium text-muted-foreground mb-1">Thinking Budget (Tokens)</label>
            <input 
              type="number"
              value={(options.thinking_budget as number) || 1024} 
              onChange={(e) => updateOption('thinking_budget', parseInt(e.target.value))}
              disabled={disabled}
              className="w-full px-2 py-1.5 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary focus:border-primary disabled:opacity-50 disabled:cursor-not-allowed"
              min={1024}
              step={1024}
            />
          </div>
        )}
      </div>
    </div>
  )
}
