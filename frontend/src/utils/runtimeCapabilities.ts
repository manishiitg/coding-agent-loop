type RuntimeCapabilityWindow = Window & {
  __APP_RUNTIME_CONFIG__?: {
    cdpEnabled?: boolean | string
  }
}

// Missing means enabled so older desktop/local runtime configs preserve their
// behavior. Remote deployments always publish an explicit false value.
export function isBrowserCDPEnabled(): boolean {
  if (typeof window === 'undefined') return true
  const value = (window as RuntimeCapabilityWindow).__APP_RUNTIME_CONFIG__?.cdpEnabled
  if (value === undefined) return true
  if (typeof value === 'boolean') return value
  return value.toLowerCase() === 'true'
}
