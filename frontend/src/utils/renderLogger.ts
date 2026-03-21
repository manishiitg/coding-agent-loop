// Render/memo loggers — disabled (were flooding console).
// Call sites kept intact so they can be re-enabled by uncommenting the bodies.

// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function useRenderLogger(_name: string, _deps: Record<string, unknown> = {}) {
  // no-op
}

// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function useMemoLogger(_label: string, _value: unknown, _summary?: string | number) {
  // no-op
}
