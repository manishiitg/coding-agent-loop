import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export interface ImageGenConfig {
  provider: string  // 'vertex' (only option now; extensible for future providers)
  modelId: string   // e.g. 'imagen-4.0-generate-001'
  apiKey: string    // GEMINI_API_KEY value
}

interface ImageGenStore {
  config: ImageGenConfig
  setConfig: (config: Partial<ImageGenConfig>) => void
}

export const useImageGenStore = create<ImageGenStore>()(
  persist(
    (set) => ({
      config: {
        provider: 'vertex',
        modelId: 'gemini-2.5-flash-image',
        apiKey: '',
      },
      setConfig: (config) =>
        set((state) => ({
          config: { ...state.config, ...config },
        })),
    }),
    {
      name: 'image-gen-store',
    }
  )
)
