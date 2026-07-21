import { create } from 'zustand'
import { resolveSetState, type SetStateAction } from './storeUtils'

// State for the "SparkQuill on WhatsApp" preview modal.
interface WhatsAppState {
  waOpen: boolean
  setWaOpen: (v: SetStateAction<boolean>) => void
  waMessages: { role: 'user' | 'assistant'; text: string }[]
  setWaMessages: (v: SetStateAction<{ role: 'user' | 'assistant'; text: string }[]>) => void
  waInput: string
  setWaInput: (v: SetStateAction<string>) => void
  waSending: boolean
  setWaSending: (v: SetStateAction<boolean>) => void
}

export const useWhatsAppStore = create<WhatsAppState>()((set) => ({
  waOpen: false,
  setWaOpen: (v) => set((s) => ({ waOpen: resolveSetState(v, s.waOpen) })),
  waMessages: [],
  setWaMessages: (v) => set((s) => ({ waMessages: resolveSetState(v, s.waMessages) })),
  waInput: '',
  setWaInput: (v) => set((s) => ({ waInput: resolveSetState(v, s.waInput) })),
  waSending: false,
  setWaSending: (v) => set((s) => ({ waSending: resolveSetState(v, s.waSending) })),
}))
