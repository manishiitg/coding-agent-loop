import { create } from 'zustand'
import { resolveSetState, type SetStateAction } from './storeUtils'

// State for the "Connectors" modal (WhatsApp real pairing, Gmail).
interface WhatsAppState {
  waOpen: boolean
  setWaOpen: (v: SetStateAction<boolean>) => void
}

export const useWhatsAppStore = create<WhatsAppState>()((set) => ({
  waOpen: false,
  setWaOpen: (v) => set((s) => ({ waOpen: resolveSetState(v, s.waOpen) })),
}))
