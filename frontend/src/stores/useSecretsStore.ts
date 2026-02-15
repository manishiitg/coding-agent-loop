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
  addSecret: (secret: Omit<StoredSecret, 'id' | 'createdAt' | 'updatedAt'>) => StoredSecret
  updateSecret: (id: string, updates: Partial<Pick<StoredSecret, 'name' | 'encryptedValue'>>) => void
  removeSecret: (id: string) => void
  getSecret: (id: string) => StoredSecret | undefined
  getSecretByName: (name: string) => StoredSecret | undefined
  fetchGlobalSecrets: () => Promise<void>
  setSelectedGlobalSecretNames: (names: string[] | null) => void
}

export const useSecretsStore = create<SecretsState>()(
  persist(
    (set, get) => ({
      secrets: [],
      globalSecrets: [],
      selectedGlobalSecretNames: null,

      addSecret: (secret) => {
        const now = Date.now()
        const newSecret: StoredSecret = {
          id: crypto.randomUUID(),
          name: secret.name,
          encryptedValue: secret.encryptedValue,
          createdAt: now,
          updatedAt: now,
        }
        set((state) => ({ secrets: [...state.secrets, newSecret] }))
        return newSecret
      },

      updateSecret: (id, updates) => {
        set((state) => ({
          secrets: state.secrets.map((s) =>
            s.id === id
              ? { ...s, ...updates, updatedAt: Date.now() }
              : s
          ),
        }))
      },

      removeSecret: (id) => {
        set((state) => ({
          secrets: state.secrets.filter((s) => s.id !== id),
        }))
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
    }),
    {
      name: 'secrets-store',
      partialize: (state) => ({ secrets: state.secrets }),
    }
  )
)
