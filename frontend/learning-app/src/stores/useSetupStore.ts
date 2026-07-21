import { create } from 'zustand'
import type { ApiEngine, Screen } from './types'
import { resolveSetState, type SetStateAction } from './storeUtils'

interface SetupState {
  screen: Screen
  setScreen: (v: SetStateAction<Screen>) => void
  engines: ApiEngine[]
  setEngines: (v: SetStateAction<ApiEngine[]>) => void
  enginesState: 'loading' | 'ready' | 'error'
  setEnginesState: (v: SetStateAction<'loading' | 'ready' | 'error'>) => void
  engine: string
  setEngine: (v: SetStateAction<string>) => void
  testState: 'idle' | 'testing' | 'valid' | 'invalid'
  setTestState: (v: SetStateAction<'idle' | 'testing' | 'valid' | 'invalid'>) => void
  testMessage: string
  setTestMessage: (v: SetStateAction<string>) => void
  booting: boolean
  setBooting: (v: SetStateAction<boolean>) => void
  bootError: boolean
  setBootError: (v: SetStateAction<boolean>) => void
  pin: string
  setPin: (v: SetStateAction<string>) => void
  pinConfirm: string
  setPinConfirm: (v: SetStateAction<string>) => void
  pinError: string
  setPinError: (v: SetStateAction<string>) => void
  saving: boolean
  setSaving: (v: SetStateAction<boolean>) => void
}

export const useSetupStore = create<SetupState>()((set) => ({
  screen: 'engine',
  setScreen: (v) => set((s) => ({ screen: resolveSetState(v, s.screen) })),
  engines: [],
  setEngines: (v) => set((s) => ({ engines: resolveSetState(v, s.engines) })),
  enginesState: 'loading',
  setEnginesState: (v) => set((s) => ({ enginesState: resolveSetState(v, s.enginesState) })),
  engine: '',
  setEngine: (v) => set((s) => ({ engine: resolveSetState(v, s.engine) })),
  testState: 'idle',
  setTestState: (v) => set((s) => ({ testState: resolveSetState(v, s.testState) })),
  testMessage: '',
  setTestMessage: (v) => set((s) => ({ testMessage: resolveSetState(v, s.testMessage) })),
  booting: true,
  setBooting: (v) => set((s) => ({ booting: resolveSetState(v, s.booting) })),
  bootError: false,
  setBootError: (v) => set((s) => ({ bootError: resolveSetState(v, s.bootError) })),
  pin: '',
  setPin: (v) => set((s) => ({ pin: resolveSetState(v, s.pin) })),
  pinConfirm: '',
  setPinConfirm: (v) => set((s) => ({ pinConfirm: resolveSetState(v, s.pinConfirm) })),
  pinError: '',
  setPinError: (v) => set((s) => ({ pinError: resolveSetState(v, s.pinError) })),
  saving: false,
  setSaving: (v) => set((s) => ({ saving: resolveSetState(v, s.saving) })),
}))
