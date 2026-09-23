// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest'
import { consumeMcpConsentReturnPath, mcpConsentReturnPath, rememberMcpConsentReturnPath } from './mcpOAuthReturn'

const path = `/oauth/consent?request=mcp_req_${'a'.repeat(64)}`

let values: Map<string, string>
beforeEach(() => {
  values = new Map()
  Object.defineProperty(globalThis, 'localStorage', {
    configurable: true,
    value: {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => { values.set(key, value) },
      removeItem: (key: string) => { values.delete(key) },
    },
  })
})

describe('MCP consent return', () => {
  it('only accepts one local consent request', () => {
    expect(mcpConsentReturnPath(path)).toBe(path)
    expect(mcpConsentReturnPath(`https://evil.example${path}`)).toBeNull()
    expect(mcpConsentReturnPath(`//evil.example${path}`)).toBeNull()
    expect(mcpConsentReturnPath(path + '&next=https://evil.example')).toBeNull()
    expect(mcpConsentReturnPath('/oauth/consent?request=wrong')).toBeNull()
  })

  it('binds the return path to the sign-in state and consumes it once', () => {
    rememberMcpConsentReturnPath(path, 'state-1')
    expect(consumeMcpConsentReturnPath('state-2')).toBeNull()
    expect(consumeMcpConsentReturnPath('state-1')).toBe(path)
    expect(consumeMcpConsentReturnPath('state-1')).toBeNull()
  })
})
