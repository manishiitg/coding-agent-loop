import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type WorkspaceConnectionType = 'local' | 'remote'

export interface WorkspaceProfile {
  id: string
  name: string
  type: WorkspaceConnectionType
  apiBaseUrl: string
  workspaceApiBaseUrl: string
  token?: string
  lastUsedAt?: number
}

interface WorkspaceConnectionState {
  profiles: WorkspaceProfile[]
  activeWorkspaceId: string
  addProfile: (profile: Omit<WorkspaceProfile, 'id' | 'lastUsedAt'> & { id?: string }) => string
  updateProfile: (id: string, updates: Partial<Omit<WorkspaceProfile, 'id'>>) => void
  removeProfile: (id: string) => void
  switchWorkspace: (id: string) => void
  setActiveWorkspaceToken: (token: string | undefined) => void
}

const STORAGE_NAME = 'workspace-connection-store'
const LOCAL_WORKSPACE_ID = 'local'

const defaultLocalProfile: WorkspaceProfile = {
  id: LOCAL_WORKSPACE_ID,
  name: 'Local',
  type: 'local',
  apiBaseUrl: '',
  workspaceApiBaseUrl: '',
  lastUsedAt: Date.now(),
}

function normalizeUrl(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

function slugifyName(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

function uniqueProfileId(name: string, existing: WorkspaceProfile[]): string {
  const base = slugifyName(name) || `workspace-${Date.now()}`
  const existingIds = new Set(existing.map(profile => profile.id))
  if (!existingIds.has(base)) return base

  let suffix = 2
  while (existingIds.has(`${base}-${suffix}`)) suffix += 1
  return `${base}-${suffix}`
}

function ensureLocalProfile(profiles: WorkspaceProfile[]): WorkspaceProfile[] {
  const normalized = profiles.map(profile => ({
    ...profile,
    apiBaseUrl: normalizeUrl(profile.apiBaseUrl || ''),
    workspaceApiBaseUrl: normalizeUrl(profile.workspaceApiBaseUrl || ''),
  }))
  return normalized.some(profile => profile.id === LOCAL_WORKSPACE_ID)
    ? normalized
    : [defaultLocalProfile, ...normalized]
}

export function getRawActiveWorkspaceId(): string {
  if (typeof window === 'undefined') return LOCAL_WORKSPACE_ID
  try {
    const raw = window.localStorage.getItem(STORAGE_NAME)
    if (!raw) return LOCAL_WORKSPACE_ID
    const parsed = JSON.parse(raw) as { state?: { activeWorkspaceId?: string } }
    return parsed.state?.activeWorkspaceId || LOCAL_WORKSPACE_ID
  } catch {
    return LOCAL_WORKSPACE_ID
  }
}

export function getWorkspaceScopedStorageKey(key: string): string {
  const workspaceId = getRawActiveWorkspaceId()
  return workspaceId === LOCAL_WORKSPACE_ID ? key : `${key}:${workspaceId}`
}

export const useWorkspaceConnectionStore = create<WorkspaceConnectionState>()(
  persist(
    (set, get) => ({
      profiles: [defaultLocalProfile],
      activeWorkspaceId: LOCAL_WORKSPACE_ID,

      addProfile: (profile) => {
        const profiles = ensureLocalProfile(get().profiles)
        const id = profile.id || uniqueProfileId(profile.name, profiles)
        const nextProfile: WorkspaceProfile = {
          id,
          name: profile.name.trim() || 'Workspace',
          type: profile.type,
          apiBaseUrl: normalizeUrl(profile.apiBaseUrl),
          workspaceApiBaseUrl: normalizeUrl(profile.workspaceApiBaseUrl),
          token: profile.token,
          lastUsedAt: Date.now(),
        }

        set({
          profiles: [...profiles.filter(existing => existing.id !== id), nextProfile],
        })
        return id
      },

      updateProfile: (id, updates) => {
        set(state => ({
          profiles: ensureLocalProfile(state.profiles).map(profile => {
            if (profile.id !== id) return profile
            return {
              ...profile,
              ...updates,
              apiBaseUrl: updates.apiBaseUrl !== undefined ? normalizeUrl(updates.apiBaseUrl) : profile.apiBaseUrl,
              workspaceApiBaseUrl: updates.workspaceApiBaseUrl !== undefined
                ? normalizeUrl(updates.workspaceApiBaseUrl)
                : profile.workspaceApiBaseUrl,
            }
          }),
        }))
      },

      removeProfile: (id) => {
        if (id === LOCAL_WORKSPACE_ID) return
        set(state => {
          const profiles = ensureLocalProfile(state.profiles).filter(profile => profile.id !== id)
          return {
            profiles,
            activeWorkspaceId: state.activeWorkspaceId === id ? LOCAL_WORKSPACE_ID : state.activeWorkspaceId,
          }
        })
      },

      switchWorkspace: (id) => {
        set(state => {
          const profiles = ensureLocalProfile(state.profiles)
          if (!profiles.some(profile => profile.id === id)) return { profiles }
          return {
            profiles: profiles.map(profile => (
              profile.id === id ? { ...profile, lastUsedAt: Date.now() } : profile
            )),
            activeWorkspaceId: id,
          }
        })
      },

      setActiveWorkspaceToken: (token) => {
        set(state => ({
          profiles: ensureLocalProfile(state.profiles).map(profile => (
            profile.id === state.activeWorkspaceId ? { ...profile, token } : profile
          )),
        }))
      },
    }),
    {
      name: STORAGE_NAME,
      partialize: (state) => ({
        profiles: ensureLocalProfile(state.profiles),
        activeWorkspaceId: state.activeWorkspaceId,
      }),
      onRehydrateStorage: () => (state) => {
        if (!state) return
        const profiles = ensureLocalProfile(state.profiles)
        if (!profiles.some(profile => profile.id === state.activeWorkspaceId)) {
          state.activeWorkspaceId = LOCAL_WORKSPACE_ID
        }
        state.profiles = profiles
      },
    }
  )
)

export function getActiveWorkspaceProfile(): WorkspaceProfile {
  const state = useWorkspaceConnectionStore.getState()
  const profiles = ensureLocalProfile(state.profiles)
  return profiles.find(profile => profile.id === state.activeWorkspaceId) || defaultLocalProfile
}
