import { create } from 'zustand'
import type { ConvMeta, ParentMsg } from './types'
import { resolveSetState, type SetStateAction } from './storeUtils'

interface ParentChatState {
  focusInput: string
  setFocusInput: (v: SetStateAction<string>) => void
  parentMessages: ParentMsg[]
  setParentMessages: (v: SetStateAction<ParentMsg[]>) => void
  sending: boolean
  setSending: (v: SetStateAction<boolean>) => void
  liveStatus: string
  setLiveStatus: (v: SetStateAction<string>) => void
  // Real content streamed in as the model generates its reply (see
  // status_stream.go's "delta" events) — a live-updating preview shown while
  // `sending` is true, replaced by the actual persisted message once the
  // turn's blocking fetch resolves. '' when nothing has streamed in yet.
  streamingReply: string
  setStreamingReply: (v: SetStateAction<string>) => void
  suggestions: { label: string; message: string }[]
  setSuggestions: (v: SetStateAction<{ label: string; message: string }[]>) => void
  menuOpen: boolean
  setMenuOpen: (v: SetStateAction<boolean>) => void
  conversations: ConvMeta[]
  setConversations: (v: SetStateAction<ConvMeta[]>) => void
  childSessionsList: ConvMeta[]
  setChildSessionsList: (v: SetStateAction<ConvMeta[]>) => void
  railOpen: boolean
  setRailOpen: (v: SetStateAction<boolean>) => void
}

export const useParentChatStore = create<ParentChatState>()((set) => ({
  focusInput: '',
  setFocusInput: (v) => set((s) => ({ focusInput: resolveSetState(v, s.focusInput) })),
  parentMessages: [],
  setParentMessages: (v) => set((s) => ({ parentMessages: resolveSetState(v, s.parentMessages) })),
  sending: false,
  setSending: (v) => set((s) => ({ sending: resolveSetState(v, s.sending) })),
  liveStatus: '',
  setLiveStatus: (v) => set((s) => ({ liveStatus: resolveSetState(v, s.liveStatus) })),
  streamingReply: '',
  setStreamingReply: (v) => set((s) => ({ streamingReply: resolveSetState(v, s.streamingReply) })),
  suggestions: [],
  setSuggestions: (v) => set((s) => ({ suggestions: resolveSetState(v, s.suggestions) })),
  menuOpen: false,
  setMenuOpen: (v) => set((s) => ({ menuOpen: resolveSetState(v, s.menuOpen) })),
  conversations: [],
  setConversations: (v) => set((s) => ({ conversations: resolveSetState(v, s.conversations) })),
  childSessionsList: [],
  setChildSessionsList: (v) => set((s) => ({ childSessionsList: resolveSetState(v, s.childSessionsList) })),
  railOpen: false,
  setRailOpen: (v) => set((s) => ({ railOpen: resolveSetState(v, s.railOpen) })),
}))
