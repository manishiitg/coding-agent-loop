import { describe, expect, it, vi } from 'vitest'

vi.mock('../services/api', () => ({
  getApiBaseUrl: () => 'http://localhost:8080',
  getAuthToken: () => null,
}))

import { secretsApi } from './secrets'

// Only two secret types exist: scoped (workflow / project / product box) and
// global. The per-user ("yours") store and its client methods are gone; this
// guards the client surface so they cannot creep back.
describe('secretsApi two-type contract', () => {
  it('exposes scoped and global secret methods', () => {
    for (const method of [
      'storeWorkflowSecret',
      'deleteWorkflowSecret',
      'listWorkflowSecrets',
      'getGlobalSecrets',
      'saveGlobalSecret',
      'deleteGlobalSecret',
      'revealGlobalSecret',
      'promoteWorkflowSecret',
      'encrypt',
      'decrypt',
    ] as const) {
      expect(typeof secretsApi[method], method).toBe('function')
    }
  })

  it('exposes no per-user secret methods', () => {
    const client = secretsApi as unknown as Record<string, unknown>
    expect(client.storeSecret).toBeUndefined()
    expect(client.deleteStoredSecret).toBeUndefined()
    expect(client.listStoredSecrets).toBeUndefined()
  })
})
