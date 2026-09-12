// Old browser/workspace JSON may still carry fields from the removed feature.
// Strip them at config boundaries so later saves cannot restore fallback chains.
export function stripRetiredLLMFallbacks<T>(value: T): T {
  if (Array.isArray(value)) return value.map(stripRetiredLLMFallbacks) as T
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(Object.entries(value).flatMap(([key, entry]) => {
    if (key === 'fallbacks' || key === 'fallback_models' || key === 'cross_provider_fallback') return []
    return [[key, key === 'options' ? entry : stripRetiredLLMFallbacks(entry)]]
  })) as T
}
