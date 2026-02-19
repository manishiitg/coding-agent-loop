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

interface SecretsState {
  secrets: StoredSecret[]
  globalSecrets: GlobalSecret[]
  // null = all global secrets selected (default), string[] = only these names selected
  selectedGlobalSecretNames: string[] | null
  botEnabledNames: Set<string>
  addSecret: (secret: Omit<StoredSecret, 'id' | 'createdAt' | 'updatedAt'>) => StoredSecret
  updateSecret: (id: string, updates: Partial<Pick<StoredSecret, 'name' | 'encryptedValue'>>) => void
  removeSecret: (id: string) => void
  getSecret: (id: string) => StoredSecret | undefined
  getSecretByName: (name: string) => StoredSecret | undefined
  fetchGlobalSecrets: () => Promise<void>
  setSelectedGlobalSecretNames: (names: string[] | null) => void
  fetchBotSecrets: () => Promise<void>
  toggleBotAccess: (id: string) => Promise<void>
}

export const useSecretsStore = create<SecretsState>()(
  persist(
    (set, get) => ({
      secrets: [],
      globalSecrets: [],
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

      setSelectedGlobalSecretNames: (names) => {
        set({ selectedGlobalSecretNames: names })
      },

      fetchBotSecrets: async () => {
        try {
          const result = await secretsApi.listStoredSecrets()
          set({ botEnabledNames: new Set(result.map((s) => s.name)) })
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
      partialize: (state) => ({ secrets: state.secrets }),
    }
  )
)
