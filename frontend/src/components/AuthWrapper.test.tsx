// @vitest-environment happy-dom
import React, { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
vi.mock('../stores/useAuthStore', () => ({ useAuthStore: vi.fn() }))
vi.mock('../pages/Login', () => ({ Login: () => <div>App sign in</div> }))
vi.mock('../pages/AuthCallback', () => ({ AuthCallback: () => <div>OAuth</div> }))
vi.mock('../pages/SharedFile', () => ({ SharedFile: ({ encodedPath }: { encodedPath: string }) => <div>Shared asset {encodedPath}</div> }))
vi.mock('../pages/SharedFolder', () => ({ SharedFolder: () => <div>Shared folder</div> }))
vi.mock('../pages/ReportPage', () => ({ ReportPage: () => <div>Report</div> }))
vi.mock('./WorkspaceConnectionSwitcher', () => ({ WorkspaceConnectionSwitcher: () => null }))
vi.mock('./DesktopAppOnlyGate', () => ({ DesktopAppOnlyGate: () => null }))
vi.mock('../services/api', () => ({ isDesktopAppOnlyMode: () => false }))
import { useAuthStore } from '../stores/useAuthStore'
import { AuthWrapper } from './AuthWrapper'
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
describe('Shared asset sign in', () => {
  it.each([true, false])('waits for authentication and keeps the file with multi-user mode %s', async (multi) => {
    window.history.replaceState({}, '', '/file?path=YWJj')
    const state = { isMultiUserMode: multi, isMultiUserModeChecked: true, isAuthenticated: false, isLoading: !multi, checkAuth: vi.fn(), checkAuthMode: vi.fn(), login: vi.fn().mockResolvedValue(undefined) }
    vi.mocked(useAuthStore).mockImplementation(() => state)
    const host = document.createElement('div');document.body.append(host);const root = createRoot(host)
    try {
      await act(async () => root.render(<AuthWrapper>Home</AuthWrapper>))
      expect(host.textContent).not.toContain('Shared asset')
      expect(host.textContent).toContain(multi ? 'App sign in' : 'Loading')
      state.isAuthenticated = true;state.isLoading = false
      await act(async () => root.render(<AuthWrapper>Home</AuthWrapper>))
      expect(host.textContent).toContain('Shared asset YWJj')
      expect(window.location.pathname).toBe('/file')
      window.history.replaceState({}, '', '/file?path=ZGVm')
      await act(async () => window.dispatchEvent(new PopStateEvent('popstate')))
      expect(host.textContent).toContain('Shared asset ZGVm')
    } finally { await act(async () => root.unmount());host.remove();window.history.replaceState({}, '', '/') }
  })
})
