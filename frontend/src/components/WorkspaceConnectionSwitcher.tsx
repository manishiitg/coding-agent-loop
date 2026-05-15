import { useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { Check, ChevronDown, Globe2, Loader2, Monitor, Plus, Trash2, X } from 'lucide-react'
import { Button } from './ui/Button'
import { Input } from './ui/Input'
import { authApi } from '../services/api'
import { useWorkspaceConnectionStore } from '../stores/useWorkspaceConnectionStore'
import { useChatStore } from '../stores/useChatStore'
import { useRunningWorkflowsStore } from '../stores/useRunningWorkflowsStore'

type WorkspaceConnectionSwitcherProps = {
  placement?: 'sidebar' | 'sidebar-bottom' | 'sidebar-minimized' | 'auth'
}

function normalizeUrl(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

function defaultWorkspaceApiUrl(apiBaseUrl: string): string {
  const normalized = normalizeUrl(apiBaseUrl)
  return normalized ? `${normalized}/api/wp` : ''
}

function nextRemoteWorkspaceName(profiles: Array<{ name: string; type: string }>): string {
  const baseNames = ['X', 'Y', 'Z']
  const existingNames = new Set(profiles.filter(profile => profile.type === 'remote').map(profile => profile.name))

  for (const name of baseNames) {
    if (!existingNames.has(name)) return name
  }

  let suffix = 2
  while (true) {
    for (const name of baseNames) {
      const candidate = `${name} ${suffix}`
      if (!existingNames.has(candidate)) return candidate
    }
    suffix += 1
  }
}

function validateRemoteUrl(value: string): string | null {
  try {
    const parsed = new URL(value)
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') {
      return 'Use an http or https URL.'
    }
    return null
  } catch {
    return 'Enter a valid workspace URL, including https://.'
  }
}

function parseWorkspaceInput(value: string): { serverUrl: string; code?: string } {
  const trimmed = value.trim()
  if (!trimmed) return { serverUrl: '' }

  const parsed = new URL(trimmed)
  if (parsed.protocol === 'runloop:') {
    return {
      serverUrl: normalizeUrl(parsed.searchParams.get('server') || ''),
      code: parsed.searchParams.get('code') || undefined,
    }
  }

  if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
    const serverParam = parsed.searchParams.get('server')
    const code = parsed.searchParams.get('code') || undefined
    return {
      serverUrl: normalizeUrl(serverParam || parsed.origin),
      code,
    }
  }

  return { serverUrl: trimmed }
}

function detachAndReload(workspaceId: string) {
  const chatStore = useChatStore.getState()
  const runningStore = useRunningWorkflowsStore.getState()
  const hasRunningWorkflows = runningStore.runningWorkflows.some(workflow => workflow.status === 'running')
  const hasActiveTabs = Object.values(chatStore.chatTabs).some(tab => tab.isStreaming || tab.hasRunningBgAgents)

  if ((hasRunningWorkflows || hasActiveTabs) && typeof window !== 'undefined') {
    const shouldSwitch = window.confirm(
      'Switch workspace and detach this window from active runs? Backend runs will continue unless you stop them explicitly.'
    )
    if (!shouldSwitch) return
  }

  chatStore.disconnectAllSSE()
  chatStore.stopPolling()
  chatStore.stopActiveSessionsPolling()
  runningStore.stopRunningPolling()
  useWorkspaceConnectionStore.getState().switchWorkspace(workspaceId)
  window.location.reload()
}

export function WorkspaceConnectionSwitcher({ placement = 'sidebar' }: WorkspaceConnectionSwitcherProps) {
  const profiles = useWorkspaceConnectionStore(state => state.profiles)
  const activeWorkspaceId = useWorkspaceConnectionStore(state => state.activeWorkspaceId)
  const addProfile = useWorkspaceConnectionStore(state => state.addProfile)
  const removeProfile = useWorkspaceConnectionStore(state => state.removeProfile)
  const isElectron = typeof window !== 'undefined' && Boolean((window as any).electronAPI)
  const [open, setOpen] = useState(false)
  const [adding, setAdding] = useState(false)
  const [apiBaseUrl, setApiBaseUrl] = useState('')
  const [testState, setTestState] = useState<'idle' | 'testing' | 'ok' | 'error'>('idle')
  const [message, setMessage] = useState('')

  const activeProfile = useMemo(
    () => profiles.find(profile => profile.id === activeWorkspaceId) || profiles[0],
    [activeWorkspaceId, profiles]
  )

  const sortedProfiles = useMemo(
    () => [...profiles].sort((a, b) => {
      if (a.id === 'local') return -1
      if (b.id === 'local') return 1
      return (b.lastUsedAt || 0) - (a.lastUsedAt || 0)
    }),
    [profiles]
  )

  if (!isElectron) return null

  const resetForm = () => {
    setApiBaseUrl('')
    setTestState('idle')
    setMessage('')
  }

  const handleApiUrlChange = (value: string) => {
    setApiBaseUrl(value)
    setTestState('idle')
    setMessage('')
  }

  const handleTest = async () => {
    let parsedInput: { serverUrl: string; code?: string }
    try {
      parsedInput = parseWorkspaceInput(apiBaseUrl)
    } catch {
      setTestState('error')
      setMessage('Enter a valid server or desktop connect URL.')
      return
    }
    const target = normalizeUrl(parsedInput.serverUrl)
    if (!target) {
      setTestState('error')
      setMessage('Enter a server URL first.')
      return
    }
    const validationError = validateRemoteUrl(target)
    if (validationError) {
      setTestState('error')
      setMessage(validationError)
      return
    }

    setTestState('testing')
    setMessage('')
    try {
      const response = await fetch(`${target}/api/auth/mode`, { cache: 'no-store' })
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`)
      }
      setTestState('ok')
      setMessage(parsedInput.code ? 'Connection looks good. Connect code detected.' : 'Connection looks good.')
    } catch (error) {
      setTestState('error')
      setMessage(error instanceof Error ? error.message : 'Could not reach workspace.')
    }
  }

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    let parsedInput: { serverUrl: string; code?: string }
    try {
      parsedInput = parseWorkspaceInput(apiBaseUrl)
    } catch {
      setTestState('error')
      setMessage('Enter a valid server or desktop connect URL.')
      return
    }
    const normalizedApi = normalizeUrl(parsedInput.serverUrl)
    if (!normalizedApi) {
      setTestState('error')
      setMessage('Server URL is required.')
      return
    }
    const validationError = validateRemoteUrl(normalizedApi)
    if (validationError) {
      setTestState('error')
      setMessage(validationError)
      return
    }
    const normalizedWorkspaceApi = defaultWorkspaceApiUrl(normalizedApi)
    const workspaceValidationError = validateRemoteUrl(normalizedWorkspaceApi)
    if (workspaceValidationError) {
      setTestState('error')
      setMessage(`Workspace API URL: ${workspaceValidationError}`)
      return
    }
    setTestState('testing')
    setMessage(parsedInput.code ? 'Connecting desktop app...' : '')
    let token: string | undefined
    try {
      if (parsedInput.code) {
        const response = await authApi.exchangeDesktopConnect(normalizedApi, parsedInput.code)
        token = response.token
      }
    } catch (error) {
      setTestState('error')
      setMessage(error instanceof Error ? error.message : 'Could not connect desktop app.')
      return
    }
    const id = addProfile({
      name: nextRemoteWorkspaceName(profiles),
      type: 'remote',
      apiBaseUrl: normalizedApi,
      workspaceApiBaseUrl: normalizedWorkspaceApi,
      token,
    })
    resetForm()
    setAdding(false)
    setOpen(false)
    detachAndReload(id)
  }

  const handleRemove = (id: string) => {
    if (id === 'local') return
    const shouldRemove = window.confirm('Remove this workspace profile from this device?')
    if (!shouldRemove) return
    removeProfile(id)
    if (id === activeWorkspaceId) {
      detachAndReload('local')
    }
  }

  const wrapperClass = placement === 'auth'
    ? 'fixed left-4 top-4 z-50'
    : 'relative'

  const buttonClass = placement === 'auth'
    ? 'h-9 rounded-md border border-border bg-background px-3 text-sm shadow-sm'
    : placement === 'sidebar-bottom'
      ? 'h-9 w-full justify-between rounded-md border border-border bg-background px-3 text-sm'
      : placement === 'sidebar-minimized'
        ? 'h-9 w-9 justify-center rounded-md border border-border bg-background'
      : 'h-8 rounded-md border border-border bg-background px-2 text-xs'

  const menuClass = placement === 'sidebar-bottom'
    ? 'absolute bottom-full left-0 z-50 mb-2 w-80 rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-lg'
    : placement === 'sidebar-minimized'
      ? 'absolute bottom-0 left-full z-50 ml-2 w-80 rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-lg'
    : 'absolute left-0 top-full z-50 mt-2 w-80 rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-lg'

  const compactIconOnly = placement === 'sidebar-minimized'

  return (
    <div
      className={wrapperClass}
      onClick={compactIconOnly ? event => event.stopPropagation() : undefined}
    >
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        className={`inline-flex items-center gap-2 text-foreground hover:bg-muted ${buttonClass}`}
        title="Switch workspace"
      >
        {activeProfile?.type === 'remote' ? (
          <Globe2 className="h-4 w-4 text-blue-500" />
        ) : (
          <Monitor className="h-4 w-4 text-emerald-500" />
        )}
        {!compactIconOnly && (
          <>
            <span className="max-w-[130px] truncate font-medium">{activeProfile?.name || 'Local'}</span>
            <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
          </>
        )}
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className={menuClass}>
            {!adding ? (
              <div className="space-y-2">
                <div className="px-2 py-1 text-xs font-medium uppercase text-muted-foreground">Workspaces</div>
                <div className="max-h-64 overflow-y-auto">
                  {sortedProfiles.map(profile => (
                    <div
                      key={profile.id}
                      className="group flex items-center gap-2 rounded-md px-2 py-2 hover:bg-muted"
                    >
                      <button
                        type="button"
                        onClick={() => {
                          setOpen(false)
                          if (profile.id !== activeWorkspaceId) detachAndReload(profile.id)
                        }}
                        className="flex min-w-0 flex-1 items-center gap-2 text-left"
                      >
                        {profile.type === 'remote' ? (
                          <Globe2 className="h-4 w-4 shrink-0 text-blue-500" />
                        ) : (
                          <Monitor className="h-4 w-4 shrink-0 text-emerald-500" />
                        )}
                        <span className="min-w-0 flex-1">
                          <span className="block truncate text-sm font-medium">{profile.name}</span>
                          <span className="block truncate text-xs text-muted-foreground">
                            {profile.type === 'local' ? 'Local app' : profile.apiBaseUrl}
                          </span>
                        </span>
                        {profile.id === activeWorkspaceId && <Check className="h-4 w-4 text-primary" />}
                      </button>
                      {profile.id !== 'local' && (
                        <button
                          type="button"
                          onClick={() => handleRemove(profile.id)}
                          className="hidden rounded p-1 text-muted-foreground hover:bg-background hover:text-destructive group-hover:block"
                          title="Remove workspace"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  ))}
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="w-full justify-center gap-2"
                  onClick={() => {
                    setAdding(true)
                    resetForm()
                  }}
                >
                  <Plus className="h-4 w-4" />
                  Add workspace
                </Button>
              </div>
            ) : (
              <form className="space-y-3 p-1" onSubmit={handleSubmit}>
                <div className="flex items-center justify-between">
                  <div className="text-sm font-medium">Add workspace</div>
                  <button
                    type="button"
                    className="rounded p-1 text-muted-foreground hover:bg-muted"
                    onClick={() => setAdding(false)}
                  >
                    <X className="h-4 w-4" />
                  </button>
                </div>
                <div className="space-y-1">
                  <label className="text-xs font-medium text-muted-foreground">Server URL</label>
                  <Input
                    value={apiBaseUrl}
                    onChange={event => handleApiUrlChange(event.target.value)}
                    placeholder="https://runloop.example.com"
                  />
                </div>
                {message && (
                  <div className={`text-xs ${testState === 'ok' ? 'text-emerald-600' : 'text-destructive'}`}>
                    {message}
                  </div>
                )}
                <div className="flex gap-2">
                  <Button type="button" variant="outline" size="sm" className="flex-1" onClick={handleTest} disabled={testState === 'testing'}>
                    {testState === 'testing' && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
                    Test
                  </Button>
                  <Button type="submit" size="sm" className="flex-1" disabled={testState === 'testing'}>
                    {testState === 'testing' ? 'Saving...' : 'Save'}
                  </Button>
                </div>
              </form>
            )}
          </div>
        </>
      )}
    </div>
  )
}
