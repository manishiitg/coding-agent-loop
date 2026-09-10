import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type ProductSurface = 'agentworks' | 'video-studio' | 'dominion' | 'sparkquill'

interface ProductSurfaceState {
  productSurface: ProductSurface
  lastVideoProjectId: string | null
  setProductSurface: (surface: ProductSurface) => void
  setLastVideoProjectId: (projectId: string | null) => void
}

export const useProductSurfaceStore = create<ProductSurfaceState>()(
  persist(
    (set) => ({
      productSurface: 'agentworks',
      lastVideoProjectId: null,
      setProductSurface: (productSurface) => set({ productSurface }),
      setLastVideoProjectId: (lastVideoProjectId) => set({ lastVideoProjectId }),
    }),
    {
      name: 'agentworks-product-surface',
      version: 4,
      migrate: (persisted) => {
        const state = persisted as Partial<ProductSurfaceState> | undefined
        const surface = state?.productSurface
        const validSurface = surface === 'agentworks' ||
          surface === 'video-studio' ||
          surface === 'dominion' ||
          surface === 'sparkquill'
        return {
          ...state,
          productSurface: validSurface ? surface : 'agentworks',
        } as ProductSurfaceState
      },
    },
  ),
)
