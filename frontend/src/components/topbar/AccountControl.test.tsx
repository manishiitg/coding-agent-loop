// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
vi.mock('../../stores/useAuthStore', () => ({ useAuthStore: vi.fn() }))
vi.mock('./AccessTokensDialog', () => ({ default: () => <div role="dialog">Manage access tokens</div> }))
vi.mock('./ChangePasswordDialog', () => ({ default: () => null }))
import { useAuthStore } from '../../stores/useAuthStore'
import { TooltipProvider } from '../ui/tooltip'
import AccountControl from './AccountControl'

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
describe('Account menu availability', () => {
  it.each([false, true])('exposes token management with multi-user mode %s', async (isMultiUserMode) => {
    vi.mocked(useAuthStore).mockReturnValue({ user: { id: 'user', username: 'Alex' }, isMultiUserMode, logout: vi.fn() })
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host)
    try {
      await act(async () => root.render(<TooltipProvider><AccountControl /></TooltipProvider>))
      const trigger = host.querySelector('button')!
      expect(trigger.getAttribute('aria-label')).toBe(isMultiUserMode ? 'Account: Alex' : 'Account: Local account')
      await act(async () => trigger.click())
      expect(host.textContent).toContain('Access tokens')
      expect(host.textContent?.includes('Change password')).toBe(isMultiUserMode)
      expect(host.textContent?.includes('Sign out')).toBe(isMultiUserMode)
      await act(async () => Array.from(host.querySelectorAll('button')).find(b => b.textContent?.includes('Access tokens'))!.click())
      expect(host.querySelector('[role="dialog"]')?.textContent).toBe('Manage access tokens')
    } finally { await act(async () => root.unmount()); host.remove() }
  })
  it('waits for the local account session to initialize', async () => {
    vi.mocked(useAuthStore).mockReturnValue({ user: null, isMultiUserMode: false, logout: vi.fn() })
    const host = document.createElement('div'); const root = createRoot(host)
    try {
      await act(async () => root.render(<TooltipProvider><AccountControl /></TooltipProvider>))
      expect(host.querySelector('button')).toBeNull()
    } finally { await act(async () => root.unmount()) }
  })
})
