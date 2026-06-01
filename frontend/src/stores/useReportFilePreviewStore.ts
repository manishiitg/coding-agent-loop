import { create } from 'zustand'

// Drives the in-report file preview modal. Report widgets (file-list rows,
// table/cards file links) call `show({ path })` with the ABSOLUTE workspace
// path of the file to preview. A single <FilePreviewModal/> mounted inside the
// report subtree subscribes to this store and renders the file inline (PDF
// iframe, image, text, etc.). This is intentionally self-contained: the report
// lives in the workflow layout, a different subtree from the chat workspace's
// file-content overlay, so it cannot rely on useWorkspaceStore's viewer.
interface ReportFilePreviewState {
  path: string | null
  name: string | null
  show: (file: { path: string; name?: string }) => void
  close: () => void
}

export const useReportFilePreviewStore = create<ReportFilePreviewState>((set) => ({
  path: null,
  name: null,
  show: ({ path, name }) =>
    set({ path, name: name ?? path.split('/').filter(Boolean).pop() ?? path }),
  close: () => set({ path: null, name: null }),
}))
