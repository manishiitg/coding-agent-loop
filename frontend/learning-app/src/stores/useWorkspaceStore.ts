import { create } from 'zustand'
import type { DrawerTab, TreeNode, WsFile } from './types'
import { resolveSetState, type SetStateAction } from './storeUtils'

// Parent-side workspace browsing: drawer tabs, the file tree/viewer, and the
// generated HTML documents (academic map, progress report).
interface WorkspaceState {
  drawerTab: DrawerTab
  setDrawerTab: (v: SetStateAction<DrawerTab>) => void
  filesView: 'subjects' | 'uploaded' | 'reference' | 'advanced'
  setFilesView: (v: SetStateAction<'subjects' | 'uploaded' | 'reference' | 'advanced'>) => void
  prefsContent: string | null
  setPrefsContent: (v: SetStateAction<string | null>) => void
  treeNodes: TreeNode[]
  setTreeNodes: (v: SetStateAction<TreeNode[]>) => void
  wsFiles: WsFile[]
  setWsFiles: (v: SetStateAction<WsFile[]>) => void
  allFiles: string[]
  setAllFiles: (v: SetStateAction<string[]>) => void
  viewerPath: string | null
  setViewerPath: (v: SetStateAction<string | null>) => void
  // Bumped whenever open_file fires, even for the SAME path — Quill may edit a
  // file and re-open it, and setting viewerPath to an unchanged string wouldn't
  // otherwise trigger a refetch.
  viewerRefreshKey: number
  setViewerRefreshKey: (v: SetStateAction<number>) => void
  viewerImageList: string[]
  setViewerImageList: (v: SetStateAction<string[]>) => void
  viewerContent: { isText: boolean; content: string } | null
  setViewerContent: (v: SetStateAction<{ isText: boolean; content: string } | null>) => void
  mapHtml: string | null
  setMapHtml: (v: SetStateAction<string | null>) => void
  mapRefreshKey: number
  setMapRefreshKey: (v: SetStateAction<number>) => void
  progressHtml: string | null
  setProgressHtml: (v: SetStateAction<string | null>) => void
  wsRefreshKey: number
  setWsRefreshKey: (v: SetStateAction<number>) => void
  filesSubjectFilter: string
  setFilesSubjectFilter: (v: SetStateAction<string>) => void
}

export const useWorkspaceStore = create<WorkspaceState>()((set) => ({
  drawerTab: 'map',
  setDrawerTab: (v) => set((s) => ({ drawerTab: resolveSetState(v, s.drawerTab) })),
  filesView: 'subjects',
  setFilesView: (v) => set((s) => ({ filesView: resolveSetState(v, s.filesView) })),
  prefsContent: null,
  setPrefsContent: (v) => set((s) => ({ prefsContent: resolveSetState(v, s.prefsContent) })),
  treeNodes: [],
  setTreeNodes: (v) => set((s) => ({ treeNodes: resolveSetState(v, s.treeNodes) })),
  wsFiles: [],
  setWsFiles: (v) => set((s) => ({ wsFiles: resolveSetState(v, s.wsFiles) })),
  allFiles: [],
  setAllFiles: (v) => set((s) => ({ allFiles: resolveSetState(v, s.allFiles) })),
  viewerPath: null,
  setViewerPath: (v) => set((s) => ({ viewerPath: resolveSetState(v, s.viewerPath) })),
  viewerRefreshKey: 0,
  setViewerRefreshKey: (v) => set((s) => ({ viewerRefreshKey: resolveSetState(v, s.viewerRefreshKey) })),
  viewerImageList: [],
  setViewerImageList: (v) => set((s) => ({ viewerImageList: resolveSetState(v, s.viewerImageList) })),
  viewerContent: null,
  setViewerContent: (v) => set((s) => ({ viewerContent: resolveSetState(v, s.viewerContent) })),
  mapHtml: null,
  setMapHtml: (v) => set((s) => ({ mapHtml: resolveSetState(v, s.mapHtml) })),
  mapRefreshKey: 0,
  setMapRefreshKey: (v) => set((s) => ({ mapRefreshKey: resolveSetState(v, s.mapRefreshKey) })),
  progressHtml: null,
  setProgressHtml: (v) => set((s) => ({ progressHtml: resolveSetState(v, s.progressHtml) })),
  wsRefreshKey: 0,
  setWsRefreshKey: (v) => set((s) => ({ wsRefreshKey: resolveSetState(v, s.wsRefreshKey) })),
  filesSubjectFilter: '',
  setFilesSubjectFilter: (v) => set((s) => ({ filesSubjectFilter: resolveSetState(v, s.filesSubjectFilter) })),
}))
