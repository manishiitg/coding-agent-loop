import { create } from 'zustand'
import { resolveSetState, type SetStateAction } from './storeUtils'

// Child→Parent is gated by the parent PIN so a child can't reach answer keys.
interface PinGateState {
  pinGate: boolean
  setPinGate: (v: SetStateAction<boolean>) => void
  gateValue: string
  setGateValue: (v: SetStateAction<string>) => void
  gateError: string
  setGateError: (v: SetStateAction<string>) => void
}

export const usePinGateStore = create<PinGateState>()((set) => ({
  pinGate: false,
  setPinGate: (v) => set((s) => ({ pinGate: resolveSetState(v, s.pinGate) })),
  gateValue: '',
  setGateValue: (v) => set((s) => ({ gateValue: resolveSetState(v, s.gateValue) })),
  gateError: '',
  setGateError: (v) => set((s) => ({ gateError: resolveSetState(v, s.gateError) })),
}))
