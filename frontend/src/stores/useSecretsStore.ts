import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { secretsApi } from '../api/secrets'

export interface StoredSecret {
  id: string
  name: string
  encryptedValue: string
  createdAt: number
  updatedAt: number
}

export interface GlobalSecret {
  name: string
}

export interface WorkflowSecret {
  name: string
}

export interface StoredUserSecret {
  name: string
}

interface SecretsState {
  secrets: StoredSecret[]
  globalSecrets: GlobalSecret[]
  storedUserSecrets: StoredUserSecret[]
  workflowSecretsByPath: Record<string, WorkflowSecret[]>
  // null = all global secrets selected (default), string[] = only these names selected
  selectedGlobalSecretNames: string[] | null
  botEnabledNames: Set<string>
  addSecret: (secret: Omit<StoredSecret, 'id' | 'createdAt' | 'updatedAt'>) => StoredSecret
  updateSecret: (id: string, updates: Partial<Pick<StoredSecret, 'name' | 'encryptedValue'>>) => void
  removeSecret: (id: string) => void
  getSecret: (id: string) => StoredSecret | undefined
  getSecretByName: (name: string) => StoredSecret | undefined
  fetchGlobalSecrets: () => Promise<void>
  fetchStoredUserSecrets: () => Promise<void>
  fetchWorkflowSecrets: (workspacePath: string) => Promise<void>
  addWorkflowSecret: (workspacePath: string, name: string, encryptedValue: string) => Promise<void>
  removeWorkflowSecret: (workspacePath: string, name: string) => Promise<void>
  setSelectedGlobalSecretNames: (names: string[] | null) => void
  fetchBotSecrets: () => Promise<void>
  toggleBotAccess: (id: string) => Promise<void>
}

export const useSecretsStore = create<SecretsState>()(
  persist(
    (set, get) => ({
      secrets: [],
      globalSecrets: [],
      storedUserSecrets: [],
      workflowSecretsByPath: {},
      selectedGlobalSecretNames: null,
      botEnabledNames: new Set<string>(),

      addSecret: (secret) => {
        const now = Date.now()
        const newSecret: StoredSecret = {
          id: crypto.randomUUID(),
          name: secret.name,
          encryptedValue: secret.encryptedValue,
          createdAt: now,
          updatedAt: now,
        }
        set((state) => ({
          secrets: [...state.secrets, newSecret],
          storedUserSecrets: state.storedUserSecrets.some((s) => s.name === secret.name)
            ? state.storedUserSecrets
            : [...state.storedUserSecrets, { name: secret.name }].sort((a, b) => a.name.localeCompare(b.name)),
          botEnabledNames: new Set([...state.botEnabledNames, secret.name]),
        }))
        // Fire-and-forget sync to server for bot session access
        secretsApi.storeSecret(secret.name, secret.encryptedValue).catch(() => {})
        return newSecret
      },

      updateSecret: (id, updates) => {
        const oldSecret = get().secrets.find((s) => s.id === id)
        const wasEnabled = oldSecret && get().botEnabledNames.has(oldSecret.name)

        set((state) => ({
          secrets: state.secrets.map((s) =>
            s.id === id
              ? { ...s, ...updates, updatedAt: Date.now() }
              : s
          ),
          storedUserSecrets: updates.name && oldSecret
            ? [
                ...state.storedUserSecrets.filter((s) => s.name !== oldSecret.name && s.name !== updates.name),
                { name: updates.name },
              ].sort((a, b) => a.name.localeCompare(b.name))
            : state.storedUserSecrets,
        }))

        if (wasEnabled && oldSecret) {
          const updated = get().secrets.find((s) => s.id === id)
          if (updated) {
            // If name changed, delete old name from server first
            if (updates.name && updates.name !== oldSecret.name) {
              secretsApi.deleteStoredSecret(oldSecret.name).catch(() => {})
              set((state) => {
                const next = new Set(state.botEnabledNames)
                next.delete(oldSecret.name)
                next.add(updated.name)
                return { botEnabledNames: next }
              })
            }
            // Re-sync updated value to server
            secretsApi.storeSecret(updated.name, updated.encryptedValue).catch(() => {})
          }
        }
      },

      removeSecret: (id) => {
        const secret = get().secrets.find((s) => s.id === id)
        const wasEnabled = secret && get().botEnabledNames.has(secret.name)

        set((state) => ({
          secrets: state.secrets.filter((s) => s.id !== id),
          storedUserSecrets: secret
            ? state.storedUserSecrets.filter((s) => s.name !== secret.name)
            : state.storedUserSecrets,
        }))

        // Only delete from server if bot access was enabled
        if (secret && wasEnabled) {
          secretsApi.deleteStoredSecret(secret.name).catch(() => {})
          set((state) => {
            const next = new Set(state.botEnabledNames)
            next.delete(secret.name)
            return { botEnabledNames: next }
          })
        }
      },

      getSecret: (id) => {
        return get().secrets.find((s) => s.id === id)
      },

      getSecretByName: (name) => {
        return get().secrets.find((s) => s.name === name)
      },

      fetchGlobalSecrets: async () => {
        try {
          const result = await secretsApi.getGlobalSecrets()
          set({ globalSecrets: result })
        } catch {
          // Silently fail — global secrets are optional
        }
      },

      fetchStoredUserSecrets: async () => {
        try {
          const result = await secretsApi.listStoredSecrets()
          set({
            storedUserSecrets: result,
            botEnabledNames: new Set(result.map((s) => s.name)),
          })
        } catch {
          set({ storedUserSecrets: [] })
        }
      },

      fetchWorkflowSecrets: async (workspacePath) => {
        const trimmed = workspacePath.trim()
        if (!trimmed) return
        try {
          const result = await secretsApi.listWorkflowSecrets(trimmed)
          set((state) => ({
            workflowSecretsByPath: {
              ...state.workflowSecretsByPath,
              [trimmed]: result,
            },
          }))
        } catch {
          set((state) => ({
            workflowSecretsByPath: {
              ...state.workflowSecretsByPath,
              [trimmed]: [],
            },
          }))
        }
      },

      addWorkflowSecret: async (workspacePath, name, encryptedValue) => {
        const trimmed = workspacePath.trim()
        if (!trimmed) return
        await secretsApi.storeWorkflowSecret(trimmed, name, encryptedValue)
        set((state) => {
          const existing = state.workflowSecretsByPath[trimmed] || []
          const next = existing.some((s) => s.name === name)
            ? existing
            : [...existing, { name }].sort((a, b) => a.name.localeCompare(b.name))
          return {
            workflowSecretsByPath: {
              ...state.workflowSecretsByPath,
              [trimmed]: next,
            },
          }
        })
      },

      removeWorkflowSecret: async (workspacePath, name) => {
        const trimmed = workspacePath.trim()
        if (!trimmed) return
        await secretsApi.deleteWorkflowSecret(trimmed, name)
        set((state) => ({
          workflowSecretsByPath: {
            ...state.workflowSecretsByPath,
            [trimmed]: (state.workflowSecretsByPath[trimmed] || []).filter((s) => s.name !== name),
          },
        }))
      },

      setSelectedGlobalSecretNames: (names) => {
        set({ selectedGlobalSecretNames: names })
      },

      fetchBotSecrets: async () => {
        try {
          const result = await secretsApi.listStoredSecrets()
          set({
            storedUserSecrets: result,
            botEnabledNames: new Set(result.map((s) => s.name)),
          })
        } catch {
          // Silently fail
        }
      },

      toggleBotAccess: async (id) => {
        const secret = get().secrets.find((s) => s.id === id)
        if (!secret) return

        const isEnabled = get().botEnabledNames.has(secret.name)

        if (isEnabled) {
          // Optimistically update UI
          set((state) => {
            const next = new Set(state.botEnabledNames)
            next.delete(secret.name)
            return { botEnabledNames: next }
          })
          try {
            await secretsApi.deleteStoredSecret(secret.name)
          } catch {
            // Revert on failure
            set((state) => ({
              botEnabledNames: new Set([...state.botEnabledNames, secret.name]),
            }))
          }
        } else {
          // Optimistically update UI
          set((state) => ({
            botEnabledNames: new Set([...state.botEnabledNames, secret.name]),
          }))
          try {
            await secretsApi.storeSecret(secret.name, secret.encryptedValue)
          } catch {
            // Revert on failure
            set((state) => {
              const next = new Set(state.botEnabledNames)
              next.delete(secret.name)
              return { botEnabledNames: next }
            })
          }
        }
      },
    }),
    {
      name: 'secrets-store',
      partialize: (state) => ({
        secrets: state.secrets,
        selectedGlobalSecretNames: state.selectedGlobalSecretNames,
      }),
    }
  )
)
