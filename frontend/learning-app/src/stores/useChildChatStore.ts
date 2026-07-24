import { create } from 'zustand'
import type { Activity, ChildSuggestion, ParentMsg } from './types'
import { resolveSetState, type SetStateAction } from './storeUtils'

interface ChildChatState {
  childMessages: ParentMsg[]
  setChildMessages: (v: SetStateAction<ParentMsg[]>) => void
  childSending: boolean
  setChildSending: (v: SetStateAction<boolean>) => void
  childInput: string
  setChildInput: (v: SetStateAction<string>) => void
  childSuggestions: ChildSuggestion[]
  setChildSuggestions: (v: SetStateAction<ChildSuggestion[]>) => void
  childLiveStatus: string
  setChildLiveStatus: (v: SetStateAction<string>) => void
  childStreamingReply: string
  setChildStreamingReply: (v: SetStateAction<string>) => void
  // The ONE activity the child is currently bound to (/api/child/activity) —
  // replaces the old scoped-tree scan + package-manifest lookup entirely.
  childActivity: Activity | null
  setChildActivity: (v: SetStateAction<Activity | null>) => void
  childViewerPath: string | null
  setChildViewerPath: (v: SetStateAction<string | null>) => void
  childViewerContent: { isText: boolean; content: string } | null
  setChildViewerContent: (v: SetStateAction<{ isText: boolean; content: string } | null>) => void
  childTreeRefreshKey: number
  setChildTreeRefreshKey: (v: SetStateAction<number>) => void
}

export const useChildChatStore = create<ChildChatState>()((set) => ({
  childMessages: [],
  setChildMessages: (v) => set((s) => ({ childMessages: resolveSetState(v, s.childMessages) })),
  childSending: false,
  setChildSending: (v) => set((s) => ({ childSending: resolveSetState(v, s.childSending) })),
  childInput: '',
  setChildInput: (v) => set((s) => ({ childInput: resolveSetState(v, s.childInput) })),
  childSuggestions: [],
  setChildSuggestions: (v) => set((s) => ({ childSuggestions: resolveSetState(v, s.childSuggestions) })),
  childLiveStatus: '',
  setChildLiveStatus: (v) => set((s) => ({ childLiveStatus: resolveSetState(v, s.childLiveStatus) })),
  childStreamingReply: '',
  setChildStreamingReply: (v) => set((s) => ({ childStreamingReply: resolveSetState(v, s.childStreamingReply) })),
  childActivity: null,
  setChildActivity: (v) => set((s) => ({ childActivity: resolveSetState(v, s.childActivity) })),
  childViewerPath: null,
  setChildViewerPath: (v) => set((s) => ({ childViewerPath: resolveSetState(v, s.childViewerPath) })),
  childViewerContent: null,
  setChildViewerContent: (v) => set((s) => ({ childViewerContent: resolveSetState(v, s.childViewerContent) })),
  childTreeRefreshKey: 0,
  setChildTreeRefreshKey: (v) => set((s) => ({ childTreeRefreshKey: resolveSetState(v, s.childTreeRefreshKey) })),
}))
