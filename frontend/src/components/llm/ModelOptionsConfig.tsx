import type { ModelMetadata } from '../../services/llm-config-api'

interface ModelOptionsConfigProps {
  metadata?: ModelMetadata
  options: Record<string, unknown>
  temperature?: number
  onChange: (opts: Record<string, unknown>, temp?: number) => void
  disabled?: boolean
}

export function ModelOptionsConfig({ metadata, options, temperature, onChange, disabled }: ModelOptionsConfigProps) {
  const updateOption = (key: string, value: string | number) => {
    onChange({ ...options, [key]: value }, temperature)
  }

  const updateTemperature = (val: string) => {
    onChange(options, parseFloat(val))
  }

  // Generate temperature options: 0.0, 0.1, 0.2, ..., 1.0
  const temperatureOptions = Array.from({ length: 11 }, (_, i) => i * 0.1)

  // Default to 0.0 if temperature is undefined
  const currentTemp = temperature !== undefined ? temperature : 0

  return (
    <div className="space-y-3 mt-4 border-t border-border pt-4">
      <h5 className="text-sm font-medium text-foreground">Model Options</h5>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        {/* Common Option: Temperature */}
        <div>
          <label className="block text-xs font-medium text-muted-foreground mb-1">Temperature</label>
          <select 
            value={currentTemp.toString()}
            onChange={(e) => updateTemperature(e.target.value)}
            disabled={disabled}
            className="w-full px-2 py-1.5 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary focus:border-primary disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {temperatureOptions.map(temp => (
              <option key={temp} value={temp.toString()}>
                {temp.toFixed(1)}
              </option>
            ))}
          </select>
        </div>

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
