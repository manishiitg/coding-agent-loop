import { Suspense, lazy, useCallback, useEffect, useRef, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { Download, Edit, Github, Link, Loader2, MoreHorizontal, Save, X } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import { MarkdownRenderer, MermaidDiagram } from './ui/MarkdownRenderer'
import { CsvRenderer } from './ui/CsvRenderer'
import { HtmlRenderer } from './ui/HtmlRenderer'
import { ConversationRenderer, isConversationJSON } from './ui/ConversationRenderer'
import { DiffRenderer } from './ui/DiffRenderer'
import { RenderedContentSearchBar, RenderedContentSearchButton, useRenderedContentSearch } from './ui/RenderedContentSearch'
import LazyModalFallback from './ui/LazyModalFallback'
import { useWorkspaceStore } from '../stores'
import { useAuthStore } from '../stores/useAuthStore'
import { agentApi } from '../services/api'
import type { FileVersion } from '../services/api-types'
import { isValidJSON } from '../utils/event-helpers'
import { prepareDomForPdfExport } from '../utils/pdfExport'
import { convertToSlackMarkdown } from '../utils/slackMarkdown'
import { isDiffFilePath, looksLikeDiffContent } from '../utils/diff'
import { copyToClipboard } from '../utils/textUtils'
import { isCodeFile } from '../utils/codeFileLanguage'

const FileEditor = lazy(() => import('./workspace/FileEditor'))
const FileRevisionsModal = lazy(() => import('./workspace/FileRevisionsModal'))
const PushToGistDialog = lazy(() => import('./workspace/PushToGistDialog'))
const XlsxRenderer = lazy(() => import('./ui/XlsxRenderer').then(module => ({ default: module.XlsxRenderer })))
const DocxRenderer = lazy(() => import('./ui/DocxRenderer').then(module => ({ default: module.DocxRenderer })))
const PdfRenderer = lazy(() => import('./ui/PdfRenderer').then(module => ({ default: module.PdfRenderer })))

const FileSurfaceFallback = () => (
  <div className="flex h-full min-h-40 items-center justify-center text-muted-foreground">
    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
    Loading viewer...
  </div>
)

const ICON_BUTTON_CLASS =
  'flex items-center p-1.5 text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors'

const CopyIcon = () => (
  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <rect x="9" y="9" width="13" height="13" rx="2" ry="2" strokeWidth={2} />
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1" />
  </svg>
)

const SlackIcon = () => (
  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
    <path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zm1.271 0a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zm0 1.271a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zM18.956 8.834a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zm-1.27 0a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.163 0a2.528 2.528 0 0 1 2.523 2.522v6.312zM15.163 18.956a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.163 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zm0-1.27a2.527 2.527 0 0 1-2.52-2.523 2.527 2.527 0 0 1 2.52-2.52h6.315A2.528 2.528 0 0 1 24 15.163a2.528 2.528 0 0 1-2.522 2.523h-6.315z"/>
  </svg>
)

const HistoryIcon = () => (
  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
  </svg>
)

const PdfIcon = () => (
  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M7 21h10a2 2 0 002-2V9l-5-5H7a2 2 0 00-2 2v13a2 2 0 002 2z" />
    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M14 4v5h5" />
    <text x="7" y="18" fontSize="6" fontWeight="bold" fill="currentColor" stroke="none">PDF</text>
  </svg>
)

type PaneAction = {
  key: string
  label: string
  icon: React.ReactNode
  onSelect: () => void
  disabled?: boolean
}

/** The secondary header actions collapsed into one menu, so the header fits a
 * 240px pane without wrapping. Closes on select, outside click, and Escape. */
function PaneActionsMenu({ actions }: { actions: PaneAction[] }) {
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onMouseDown = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onMouseDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onMouseDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen(prev => !prev)}
        aria-label="More actions"
        aria-expanded={open}
        title="More actions"
        className={`${ICON_BUTTON_CLASS} ${open ? 'bg-gray-100 text-gray-900 dark:bg-gray-700 dark:text-gray-100' : ''}`}
      >
        <MoreHorizontal className="w-4 h-4" />
      </button>
      {open && (
        <div role="menu" className="absolute right-0 top-full z-50 mt-1 w-52 rounded-lg border border-gray-200 bg-white p-1 shadow-xl dark:border-gray-700 dark:bg-gray-800">
          {actions.map(action => (
            <button
              key={action.key}
              type="button"
              role="menuitem"
              disabled={action.disabled}
              onClick={() => {
                setOpen(false)
                action.onSelect()
              }}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm text-gray-700 hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50 dark:text-gray-200 dark:hover:bg-gray-700"
            >
              {action.icon}
              <span>{action.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/**
 * File viewer/editor body: images, video, audio, PDF/XLSX/DOCX, CSV, HTML,
 * Mermaid, conversation logs, JSON, diffs, and markdown (with inline editing,
 * save+commit, revision history, PDF export, and Gist push). Reads everything
 * from `useWorkspaceStore` (selectedFile, fileContent, showFileContent, ...).
 *
 * It fills whatever box it's given. Two shells mount it:
 *   - `pane`: the workspace pane's Files view swaps the file tree for this
 *     body while a file is open. The header collapses its secondary actions
 *     into a menu so it fits a narrow pane, tall surfaces (PDF, video, HTML)
 *     size to the pane instead of the viewport, and keyboard shortcuts only
 *     fire while focus is inside the viewer so Ctrl+E / Ctrl+S / Esc typed in
 *     the chat next to it are left alone.
 *   - `overlay`: `FileContentViewerOverlay` below, a full-viewport shell for
 *     surfaces with no workspace pane (Video Studio). Behaves as the original
 *     full-screen viewer did, shortcuts included.
 */
export function FileContentViewerBody({ variant }: { variant: 'pane' | 'overlay' }) {
  const rootRef = useRef<HTMLDivElement>(null)
  const {
    selectedFile,
    fileContent,
    loadingFileContent,
    showFileContent,
    setShowFileContent,
    setFileContent,
    setLoadingFileContent,
    showRevisionsModal,
    setShowRevisionsModal,
    isEditMode,
    setIsEditMode,
    editedContent,
    setEditedContent,
    isSaving,
    getHasUnsavedChanges,
    saveFile,
    binaryFileData,
  } = useWorkspaceStore(useShallow(state => ({
    selectedFile: state.selectedFile,
    fileContent: state.fileContent,
    loadingFileContent: state.loadingFileContent,
    showFileContent: state.showFileContent,
    setShowFileContent: state.setShowFileContent,
    setFileContent: state.setFileContent,
    setLoadingFileContent: state.setLoadingFileContent,
    showRevisionsModal: state.showRevisionsModal,
    setShowRevisionsModal: state.setShowRevisionsModal,
    isEditMode: state.isEditMode,
    setIsEditMode: state.setIsEditMode,
    editedContent: state.editedContent,
    setEditedContent: state.setEditedContent,
    isSaving: state.isSaving,
    getHasUnsavedChanges: state.getHasUnsavedChanges,
    saveFile: state.saveFile,
    binaryFileData: state.binaryFileData,
  })))

  const [videoObjectUrl, setVideoObjectUrl] = useState<string | null>(null)
  const [audioObjectUrl, setAudioObjectUrl] = useState<string | null>(null)

  useEffect(() => {
    const filePath = selectedFile?.path?.toLowerCase() || ''
    const isVideoFile = filePath.endsWith('.webm') || filePath.endsWith('.mp4') || filePath.endsWith('.mov')

    if (!isVideoFile || !binaryFileData) {
      setVideoObjectUrl((current) => {
        if (current) {
          URL.revokeObjectURL(current)
        }
        return null
      })
      return
    }

    const mimeType = filePath.endsWith('.webm')
      ? 'video/webm'
      : filePath.endsWith('.mov')
        ? 'video/quicktime'
        : 'video/mp4'
    const blob = new Blob([binaryFileData], { type: mimeType })
    const nextUrl = URL.createObjectURL(blob)

    setVideoObjectUrl((current) => {
      if (current) {
        URL.revokeObjectURL(current)
      }
      return nextUrl
    })

    return () => {
      URL.revokeObjectURL(nextUrl)
    }
  }, [binaryFileData, selectedFile?.path])

  useEffect(() => {
    const filePath = selectedFile?.path?.toLowerCase() || ''
    const isAudioFile = ['.mp3', '.wav', '.m4a', '.aac', '.ogg', '.oga', '.flac', '.opus'].some(ext => filePath.endsWith(ext))

    if (!isAudioFile || !binaryFileData) {
      setAudioObjectUrl((current) => {
        if (current) {
          URL.revokeObjectURL(current)
        }
        return null
      })
      return
    }

    const mimeType = filePath.endsWith('.wav')
      ? 'audio/wav'
      : filePath.endsWith('.m4a')
        ? 'audio/mp4'
        : filePath.endsWith('.aac')
          ? 'audio/aac'
          : filePath.endsWith('.ogg') || filePath.endsWith('.oga')
            ? 'audio/ogg'
            : filePath.endsWith('.flac')
              ? 'audio/flac'
              : filePath.endsWith('.opus')
                ? 'audio/opus'
                : 'audio/mpeg'
    const blob = new Blob([binaryFileData], { type: mimeType })
    const nextUrl = URL.createObjectURL(blob)

    setAudioObjectUrl((current) => {
      if (current) {
        URL.revokeObjectURL(current)
      }
      return nextUrl
    })

    return () => {
      URL.revokeObjectURL(nextUrl)
    }
  }, [binaryFileData, selectedFile?.path])

  const [commitMessage, setCommitMessage] = useState('')
  const [showCommitDialog, setShowCommitDialog] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [isRestoring, setIsRestoring] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)
  const [isExportingPdf, setIsExportingPdf] = useState(false)
  const [showPushToGistDialog, setShowPushToGistDialog] = useState(false)
  const [exportProgress, setExportProgress] = useState<string | null>(null)
  const [shareCopied, setShareCopied] = useState(false)
  const [contentCopied, setContentCopied] = useState(false)
  const [slackCopied, setSlackCopied] = useState(false)
  const markdownContentRef = useRef<HTMLDivElement>(null)
  const selectedFilePathLower = selectedFile?.path?.toLowerCase() || ''
  const isRenderedMarkdownSearchAvailable = (
    showFileContent &&
    !loadingFileContent &&
    !isEditMode &&
    !!fileContent &&
    !fileContent.startsWith('data:image/') &&
    !isCodeFile(selectedFile?.path || '') &&
    !binaryFileData &&
    !selectedFilePathLower.endsWith('.csv') &&
    !selectedFilePathLower.endsWith('.html') &&
    !selectedFilePathLower.endsWith('.htm') &&
    !selectedFilePathLower.endsWith('.mmd') &&
    !selectedFilePathLower.endsWith('.mermaid') &&
    !isValidJSON(fileContent) &&
    !looksLikeDiffContent(fileContent)
  )
  const renderedContentSearch = useRenderedContentSearch({
    targetRef: markdownContentRef,
    contentKey: `${selectedFile?.path || ''}:${fileContent.length}`,
    enabled: isRenderedMarkdownSearchAvailable,
  })

  // Initialize editedContent when entering edit mode
  useEffect(() => {
    if (isEditMode && editedContent === '' && fileContent) {
      setEditedContent(fileContent)
    }
  }, [isEditMode, fileContent, editedContent, setEditedContent])

  // Handle edit mode toggle
  const handleEdit = useCallback(() => {
    setEditedContent(fileContent)
    setIsEditMode(true)
    setSaveError(null)
  }, [fileContent, setEditedContent, setIsEditMode])

  // Handle download
  const handleDownload = useCallback(() => {
    if (!selectedFile || !fileContent) return

    const blob = new Blob([fileContent], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = selectedFile.path.split('/').pop() || 'download'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }, [selectedFile, fileContent])

  // Handle cancel edit
  const handleCancelEdit = useCallback(() => {
    if (getHasUnsavedChanges()) {
      if (window.confirm('You have unsaved changes. Are you sure you want to cancel?')) {
        setEditedContent('')
        setIsEditMode(false)
        setSaveError(null)
      }
    } else {
      setEditedContent('')
      setIsEditMode(false)
      setSaveError(null)
    }
  }, [getHasUnsavedChanges, setEditedContent, setIsEditMode])

  // Handle save
  const handleSave = useCallback(async () => {
    // Validate JSON if it's a JSON file
    if (selectedFile?.path?.toLowerCase().endsWith('.json') || isValidJSON(editedContent)) {
      try {
        JSON.parse(editedContent)
      } catch {
        setSaveError('Invalid JSON. Please fix the syntax errors before saving.')
        return
      }
    }

    // Check file size (warn if > 1MB)
    if (editedContent.length > 1024 * 1024) {
      if (!window.confirm('File is larger than 1MB. Continue saving?')) {
        return
      }
    }

    setShowCommitDialog(true)
  }, [selectedFile?.path, editedContent])

  // Handle save with commit message
  const handleSaveWithCommit = async () => {
    setSaveError(null)
    const result = await saveFile(commitMessage || undefined)
    if (result.success) {
      setShowCommitDialog(false)
      setCommitMessage('')
      setSaveError(null)
    } else {
      setSaveError(result.error || 'Failed to save file')
      // Keep dialog open on error
    }
  }

  // Handle restore version
  const handleRestoreVersion = useCallback(async (version: FileVersion) => {
    if (!selectedFile) {
      setRestoreError('No file selected')
      return
    }

    setIsRestoring(true)
    setRestoreError(null)

    try {
      // Call restore API
      const response = await agentApi.restoreFileVersion(
        selectedFile.path,
        version.commit_hash,
        `Restore to version ${version.commit_hash.substring(0, 8)}: ${version.commit_message}`
      )

      if (response.success) {
        // Reload file content after successful restore
        setLoadingFileContent(true)
        try {
          const contentResponse = await agentApi.getPlannerFileContent(selectedFile.path)
          if (contentResponse.success && contentResponse.data) {
            let processedContent = contentResponse.data.content

            // Process the content to convert escaped newlines to actual newlines
            processedContent = processedContent
              .replace(/\\n/g, '\n')
              .replace(/\\t/g, '\t')
              .replace(/\\r/g, '\r')

            // Check if this is a JSON file
            const extensionIsJson = selectedFile.path.toLowerCase().endsWith('.json')
            const contentIsJson = isValidJSON(processedContent)

            if (extensionIsJson || contentIsJson) {
              try {
                const parsed = JSON.parse(processedContent)
                processedContent = JSON.stringify(parsed, null, 2)
              } catch {
                // Keep original content if JSON parsing fails
              }
            }

            setFileContent(processedContent)

            // Exit edit mode if we were in it
            if (isEditMode) {
              setIsEditMode(false)
              setEditedContent('')
            }
          }
        } catch (err) {
          console.error('Failed to reload file content after restore:', err)
          // Still close modal even if reload fails
        } finally {
          setLoadingFileContent(false)
        }

        // Close modal on success
        setShowRevisionsModal(false)
      } else {
        setRestoreError(response.message || 'Failed to restore file version')
      }
    } catch (error) {
      console.error('Failed to restore file version:', error)
      setRestoreError(error instanceof Error ? error.message : 'Failed to restore file version')
    } finally {
      setIsRestoring(false)
    }
  }, [selectedFile, isEditMode, setIsEditMode, setEditedContent, setFileContent, setLoadingFileContent, setShowRevisionsModal])

  // Handle export to PDF
  const handleExportPdf = useCallback(async () => {
    if (!markdownContentRef.current || !selectedFile) return
    setIsExportingPdf(true)
    setExportProgress(null)
    const filename = (selectedFile.name || selectedFile.path?.split('/').pop() || 'document')
      .replace(/\.[^.]+$/, '') + '.pdf'
    const isElectron = !!window.electronAPI?.printToPDF

    try {
      const { restore } = await prepareDomForPdfExport(markdownContentRef.current)
      try {
        if (isElectron) {
          // Electron: printToPDF via IPC → direct file save
          await window.electronAPI?.printToPDF?.(filename)
        } else {
          // Web: clone content into a top-level wrapper for clean full-page printing
          const printTarget = markdownContentRef.current
          const clone = printTarget.cloneNode(true) as HTMLElement
          const wrapper = document.createElement('div')
          wrapper.id = 'pdf-print-wrapper'
          wrapper.style.cssText = 'position:absolute;top:0;left:0;width:100%;background:white;padding:40px;z-index:99999;'
          wrapper.appendChild(clone)
          // Set document title to filename for the PDF name
          const prevTitle = document.title
          document.title = filename.replace(/\.pdf$/, '')
          const style = document.createElement('style')
          style.textContent = `@media print {
            body > *:not(#pdf-print-wrapper) { display: none !important; }
            #pdf-print-wrapper { position: static !important; width: 100% !important; }
            #pdf-print-wrapper * { max-width: 100% !important; }
            html, body { overflow: visible !important; height: auto !important; }
          }`
          document.head.appendChild(style)
          document.body.appendChild(wrapper)
          await new Promise<void>((resolve) => {
            window.addEventListener('afterprint', () => resolve(), { once: true })
            window.print()
          })
          document.body.removeChild(wrapper)
          document.head.removeChild(style)
          document.title = prevTitle
        }
      } finally {
        restore()
      }
    } catch (err) {
      console.error('PDF export failed:', err)
    } finally {
      setIsExportingPdf(false)
      setExportProgress(null)
    }
  }, [selectedFile])

  // Keyboard shortcuts
  useEffect(() => {
    if (!showFileContent) return

    const handleKeyDown = (e: KeyboardEvent) => {
      // In the pane the viewer sits next to the chat; only act on shortcuts
      // typed while focus is inside the viewer. The overlay covers the
      // viewport, so there it keeps the original global behavior.
      if (variant === 'pane') {
        const active = document.activeElement
        if (!(active instanceof Node) || !rootRef.current?.contains(active)) return
      }
      // Ctrl+S or Cmd+S: Save
      if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault()
        if (isEditMode && getHasUnsavedChanges()) {
          handleSave()
        }
      }
      // Ctrl+E or Cmd+E: Toggle edit mode
      if ((e.ctrlKey || e.metaKey) && e.key === 'e') {
        e.preventDefault()
        if (isEditMode) {
          handleCancelEdit()
        } else {
          handleEdit()
        }
      }
      // Esc: Cancel edit mode
      if (e.key === 'Escape' && isEditMode) {
        if (!getHasUnsavedChanges()) {
          handleCancelEdit()
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [showFileContent, variant, isEditMode, getHasUnsavedChanges, handleSave, handleEdit, handleCancelEdit])

  const copyContent = useCallback(async () => {
    if (!fileContent) return
    await navigator.clipboard.writeText(fileContent)
    setContentCopied(true)
    setTimeout(() => setContentCopied(false), 2000)
  }, [fileContent])

  const copyAsSlack = useCallback(async () => {
    if (!fileContent) return
    const slack = convertToSlackMarkdown(fileContent)
    await navigator.clipboard.writeText(slack)
    setSlackCopied(true)
    setTimeout(() => setSlackCopied(false), 2000)
  }, [fileContent])

  const copyShareLink = useCallback(() => {
    if (!selectedFile?.path) return
    const encoded = btoa(unescape(encodeURIComponent(selectedFile.path)))
    const uid = useAuthStore.getState().user?.id || ''
    const shareUrl = `${window.location.origin}/file?path=${encoded}${uid ? `&uid=${encodeURIComponent(uid)}` : ''}`
    copyToClipboard(shareUrl).then((ok) => {
      if (ok) {
        setShareCopied(true)
        setTimeout(() => setShareCopied(false), 2000)
      }
    })
  }, [selectedFile?.path])

  const isOfficeOrPdf = ['.xls', '.xlsx', '.docx', '.pdf'].some(ext => selectedFilePathLower.endsWith(ext))
  // Edit is hidden for binary documents and for code files (code is written by the agent).
  const canEdit = !isOfficeOrPdf && !isCodeFile(selectedFile?.path || '')
  const canShowRevisions = !isOfficeOrPdf
  const isMarkdownFile = selectedFilePathLower.endsWith('.md') || selectedFilePathLower.endsWith('.markdown')
  // PDF, video, and HTML surfaces fill the viewport in the overlay and the
  // pane in the pane.
  const tallSurfaceClass = variant === 'pane' ? 'h-full min-h-[320px]' : 'h-[calc(100vh-120px)]'

  const paneActions: PaneAction[] = [
    { key: 'copy', label: contentCopied ? 'Copied!' : 'Copy content', icon: <CopyIcon />, onSelect: () => { void copyContent() } },
    { key: 'slack', label: slackCopied ? 'Copied!' : 'Copy as Slack format', icon: <SlackIcon />, onSelect: () => { void copyAsSlack() } },
    { key: 'share', label: shareCopied ? 'Copied!' : 'Copy share link', icon: <Link className="w-4 h-4" />, onSelect: copyShareLink },
    ...(canShowRevisions ? [{ key: 'revisions', label: 'File revisions', icon: <HistoryIcon />, onSelect: () => setShowRevisionsModal(true) }] : []),
    ...(isMarkdownFile ? [
      { key: 'pdf', label: isExportingPdf ? (exportProgress || 'Exporting…') : 'Export as PDF', icon: isExportingPdf ? <Loader2 className="w-4 h-4 animate-spin" /> : <PdfIcon />, onSelect: () => { void handleExportPdf() }, disabled: isExportingPdf },
      { key: 'gist', label: 'Push to GitHub Gist', icon: <Github className="w-4 h-4" />, onSelect: () => setShowPushToGistDialog(true) },
    ] : []),
  ]

  // Prevent navigation with unsaved changes
  useEffect(() => {
    if (!showFileContent || !getHasUnsavedChanges()) return

    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => window.removeEventListener('beforeunload', handleBeforeUnload)
  }, [showFileContent, getHasUnsavedChanges])

  return (
    <>
      <div
        ref={rootRef}
        className="flex h-full min-h-0 flex-col bg-white dark:bg-gray-900"
        data-ui-file-path={selectedFile?.path || undefined}
        data-ui-file-ready={showFileContent && !loadingFileContent ? 'true' : 'false'}
      >
        {/* Fixed Header */}
        <div className={`flex items-center justify-between ${variant === 'pane' ? 'px-3' : 'px-4'} py-2 border-b border-gray-200 dark:border-gray-700 flex-shrink-0`}>
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <button
              onClick={() => {
                if (getHasUnsavedChanges()) {
                  if (window.confirm('You have unsaved changes. Are you sure you want to close?')) {
                    setEditedContent('')
                    setIsEditMode(false)
                    setShowFileContent(false)
                  }
                } else {
                  setShowFileContent(false)
                }
              }}
              className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 flex-shrink-0"
            >
              ← Back
            </button>
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="flex flex-col min-w-0 cursor-help gap-0.5">
                    {selectedFile?.path && (
                      <>
                        <div className="flex items-center gap-2">
                          <h2 className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">
                            {selectedFile.path.split('/').pop() || selectedFile.path}
                          </h2>
                          {getHasUnsavedChanges() && (
                            <span className="text-[10px] text-orange-500">●</span>
                          )}
                        </div>
                        <p className="text-[10px] text-gray-500 dark:text-gray-400 truncate" title={selectedFile.path}>
                          {selectedFile.path}
                        </p>
                      </>
                    )}
                  </div>
                </TooltipTrigger>
                {selectedFile?.path && (
                  <TooltipContent>
                    <p className="max-w-md break-all">{selectedFile.path}</p>
                  </TooltipContent>
                )}
              </Tooltip>
            </TooltipProvider>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {!isEditMode ? (
              <div className="flex items-center gap-0.5">
                {canEdit && (
                  <button onClick={handleEdit} className={ICON_BUTTON_CLASS} title="Edit file (Ctrl+E)">
                    <Edit className="w-4 h-4" />
                  </button>
                )}
                <button onClick={handleDownload} className={ICON_BUTTON_CLASS} title="Download file">
                  <Download className="w-4 h-4" />
                </button>
                {isRenderedMarkdownSearchAvailable && (
                  <RenderedContentSearchButton search={renderedContentSearch} className={ICON_BUTTON_CLASS} />
                )}
                {variant === 'pane' ? (
                  <PaneActionsMenu actions={paneActions} />
                ) : (
                  <>
                    <button onClick={() => { void copyContent() }} className={`${ICON_BUTTON_CLASS} gap-1`} title="Copy formatted content">
                      <CopyIcon />
                      {contentCopied && <span className="text-xs text-green-600 dark:text-green-400">Copied!</span>}
                    </button>
                    <button onClick={() => { void copyAsSlack() }} className={`${ICON_BUTTON_CLASS} gap-1`} title="Copy as Slack format">
                      <SlackIcon />
                      {slackCopied && <span className="text-xs text-green-600 dark:text-green-400">Copied!</span>}
                    </button>
                    <button onClick={copyShareLink} className={`${ICON_BUTTON_CLASS} gap-1`} title="Copy public share link">
                      <Link className="w-4 h-4" />
                      {shareCopied && <span className="text-xs text-green-600 dark:text-green-400">Copied!</span>}
                    </button>
                    {canShowRevisions && (
                      <button onClick={() => setShowRevisionsModal(true)} className={ICON_BUTTON_CLASS} title="View file revisions">
                        <HistoryIcon />
                      </button>
                    )}
                    {isMarkdownFile && (
                      <button
                        onClick={handleExportPdf}
                        disabled={isExportingPdf}
                        className={`${ICON_BUTTON_CLASS} disabled:opacity-50 disabled:cursor-not-allowed`}
                        title="Export as PDF"
                      >
                        {isExportingPdf ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin" />
                            {exportProgress && (
                              <span className="ml-1 text-xs text-gray-500 dark:text-gray-400">{exportProgress}</span>
                            )}
                          </>
                        ) : (
                          <PdfIcon />
                        )}
                      </button>
                    )}
                    {isMarkdownFile && (
                      <button
                        onClick={() => setShowPushToGistDialog(true)}
                        className={`${ICON_BUTTON_CLASS} disabled:opacity-50 disabled:cursor-not-allowed`}
                        title="Push to GitHub Gist"
                      >
                        <Github className="w-4 h-4" />
                      </button>
                    )}
                  </>
                )}
              </div>
            ) : (
              <>
                <button
                  onClick={handleCancelEdit}
                  className="flex items-center gap-1 px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors"
                  title="Cancel edit (Esc)"
                  disabled={isSaving}
                >
                  <X className="w-4 h-4" />
                  Cancel
                </button>
                {getHasUnsavedChanges() && (
                  <button
                    onClick={handleSave}
                    disabled={isSaving}
                    className="flex items-center gap-1 px-3 py-1.5 text-sm text-white bg-blue-500 hover:bg-blue-600 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    title="Save file (Ctrl+S)"
                  >
                    {isSaving ? (
                      <Loader2 className="w-4 h-4 animate-spin" />
                    ) : (
                      <Save className="w-4 h-4" />
                    )}
                    Save
                  </button>
                )}
              </>
            )}
          </div>
        </div>

        {isRenderedMarkdownSearchAvailable && (
          <RenderedContentSearchBar search={renderedContentSearch} />
        )}

        {/* Save Error Message */}
        {saveError && (
          <div className="px-4 py-2 bg-red-50 dark:bg-red-900/20 border-b border-red-200 dark:border-red-800">
            <p className="text-sm text-red-600 dark:text-red-400">{saveError}</p>
          </div>
        )}

        {/* Commit Message Dialog */}
        {showCommitDialog && (
          <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 w-full max-w-md">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-4">
                Save File
              </h3>
              <div className="mb-4">
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Commit Message (optional)
                </label>
                <input
                  type="text"
                  value={commitMessage}
                  onChange={(e) => setCommitMessage(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault()
                      handleSaveWithCommit()
                    } else if (e.key === 'Escape') {
                      setShowCommitDialog(false)
                      setCommitMessage('')
                    }
                  }}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="Enter commit message..."
                  autoFocus
                />
              </div>
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => {
                    setShowCommitDialog(false)
                    setCommitMessage('')
                  }}
                  className="px-4 py-2 text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-100"
                >
                  Cancel
                </button>
                <button
                  onClick={handleSaveWithCommit}
                  disabled={isSaving}
                  className="px-4 py-2 text-sm text-white bg-blue-500 hover:bg-blue-600 rounded-md disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isSaving ? 'Saving...' : 'Save'}
                </button>
              </div>
            </div>
          </div>
        )}

        {/* Scrollable Content */}
        <div className="flex-1 overflow-y-auto">
          {loadingFileContent ? (
            <div className="flex items-center justify-center h-full">
              <div className="text-center">
                <div className="w-8 h-8 border-4 border-gray-300 border-t-blue-500 rounded-full animate-spin mx-auto mb-4"></div>
                <p className="text-gray-500">Loading file content...</p>
              </div>
            </div>
          ) : (
            <>
              {fileContent.startsWith('data:image/') ? (
                <div className="flex flex-col items-center justify-center h-full p-4">
                  <img
                    src={fileContent}
                    alt="File content"
                    className="max-w-full max-h-full object-contain rounded-lg shadow-lg"
                    onError={(e) => console.error('❌ Image failed to load:', e)}
                  />
                  <p className="text-sm text-gray-500 mt-2">Image file</p>
                </div>
              ) : isEditMode ? (
                <div className="h-full overflow-hidden">
                  <Suspense fallback={<FileSurfaceFallback />}>
                    <FileEditor
                      value={editedContent}
                      filepath={selectedFile?.path || ''}
                      readOnly={false}
                      onChange={(value) => setEditedContent(value || '')}
                      height="100%"
                    />
                  </Suspense>
                </div>
              ) : (selectedFile?.path && isCodeFile(selectedFile.path) && !/\.html?$/i.test(selectedFile.path)) ? (
                <div className="h-full overflow-hidden">
                  <Suspense fallback={<FileSurfaceFallback />}>
                    <FileEditor
                      value={fileContent}
                      filepath={selectedFile.path}
                      readOnly={true}
                      height="100%"
                    />
                  </Suspense>
                </div>
              ) : (
                <div className={(selectedFile?.path?.toLowerCase().endsWith('.pdf') || selectedFile?.path?.toLowerCase().endsWith('.html') || selectedFile?.path?.toLowerCase().endsWith('.htm')) ? "" : "p-6"}>
                  {(() => {
                    const filePath = selectedFile?.path?.toLowerCase() || ''

                    // CSV files
                    if (filePath.endsWith('.csv')) {
                      return <CsvRenderer content={fileContent} />
                    }

                    // Excel files (binary)
                    if ((filePath.endsWith('.xlsx') || filePath.endsWith('.xls')) && binaryFileData) {
                      return (
                        <Suspense fallback={<FileSurfaceFallback />}>
                          <XlsxRenderer data={binaryFileData} />
                        </Suspense>
                      )
                    }

                    // DOCX files (binary)
                    if (filePath.endsWith('.docx') && binaryFileData) {
                      return (
                        <Suspense fallback={<FileSurfaceFallback />}>
                          <DocxRenderer data={binaryFileData} />
                        </Suspense>
                      )
                    }

                    // PDF files (binary)
                    if (filePath.endsWith('.pdf') && binaryFileData) {
                      return (
                        <div className={`${tallSurfaceClass} w-full`}>
                          <Suspense fallback={<FileSurfaceFallback />}>
                            <PdfRenderer data={binaryFileData} />
                          </Suspense>
                        </div>
                      )
                    }

                    // Video files
                    if ((filePath.endsWith('.webm') || filePath.endsWith('.mp4') || filePath.endsWith('.mov')) && videoObjectUrl) {
                      return (
                        <div className={`${tallSurfaceClass} w-full flex items-center justify-center bg-black rounded-lg`}>
                          <video
                            controls
                            autoPlay
                            className="max-h-full max-w-full"
                            src={videoObjectUrl}
                          />
                        </div>
                      )
                    }

                    // Audio files
                    if ((filePath.endsWith('.mp3') || filePath.endsWith('.wav') || filePath.endsWith('.m4a') || filePath.endsWith('.aac') || filePath.endsWith('.ogg') || filePath.endsWith('.oga') || filePath.endsWith('.flac') || filePath.endsWith('.opus')) && audioObjectUrl) {
                      return (
                        <div className="min-h-[260px] w-full flex items-center justify-center rounded-lg border border-gray-200 bg-gray-50 p-8 dark:border-gray-700 dark:bg-gray-900">
                          <audio
                            controls
                            autoPlay
                            className="w-full max-w-3xl"
                            src={audioObjectUrl}
                          />
                        </div>
                      )
                    }

                    // HTML files
                    if (filePath.endsWith('.html') || filePath.endsWith('.htm')) {
                      return (
                        <div className={`${tallSurfaceClass} w-full`}>
                          <HtmlRenderer content={fileContent} />
                        </div>
                      )
                    }

                    // Mermaid diagram files (.mmd, .mermaid)
                    if (filePath.endsWith('.mmd') || filePath.endsWith('.mermaid')) {
                      return (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                            <span className="font-medium">Mermaid Diagram</span>
                            <span className="text-xs bg-purple-100 dark:bg-purple-900 text-purple-800 dark:text-purple-200 px-2 py-1 rounded font-mono">
                              {selectedFile?.path?.split('.').pop()}
                            </span>
                          </div>
                          <MermaidDiagram content={fileContent} />
                        </div>
                      )
                    }

                    // Conversation log files (-conversation.json)
                    if (selectedFile?.path && isValidJSON(fileContent)) {
                      try {
                        const parsed = JSON.parse(fileContent)
                        if (isConversationJSON(selectedFile.path, parsed)) {
                          return <ConversationRenderer content={fileContent} />
                        }
                      } catch { /* fall through to generic JSON */ }
                    }

                    // Check for JSON files
                    if (selectedFile?.path?.toLowerCase().endsWith('.json') || isValidJSON(fileContent)) {
                      // Check if content looks like formatted JSON (has proper indentation)
                      const isFormattedJson = fileContent.includes('{\n  ') || fileContent.includes('[\n  ')

                      return (
                        <div className="space-y-2">
                          <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
                            <span className="font-medium">📄 JSON File</span>
                            {isFormattedJson && (
                              <span className="text-xs bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-200 px-2 py-1 rounded">
                                Formatted
                              </span>
                            )}
                          </div>
                          <div className="bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded-lg p-4">
                            <pre className="text-xs font-mono text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap break-words leading-relaxed">
                              {fileContent}
                            </pre>
                          </div>
                        </div>
                      )
                    }

                    if ((selectedFile?.path && isDiffFilePath(selectedFile.path)) || looksLikeDiffContent(fileContent)) {
                      return <DiffRenderer content={fileContent} />
                    }

                    // Default: render as markdown
                    return (
                      <div ref={markdownContentRef} className="max-w-4xl mx-auto">
                        <div className="prose prose-sm max-w-none dark:prose-invert prose-headings:font-semibold prose-headings:text-gray-900 dark:prose-headings:text-gray-100 prose-p:text-gray-700 dark:prose-p:text-gray-300 prose-a:text-blue-600 dark:prose-a:text-blue-400 prose-a:no-underline hover:prose-a:underline prose-strong:text-gray-900 dark:prose-strong:text-gray-100 prose-code:text-blue-600 dark:prose-code:text-blue-400 prose-pre:bg-gray-50 dark:prose-pre:bg-gray-900 prose-blockquote:border-l-blue-500 prose-blockquote:text-gray-700 dark:prose-blockquote:text-gray-300">
                          <MarkdownRenderer
                            content={fileContent}
                            className="max-w-none"
                            showScrollbar={true}
                            basePath={selectedFile?.path}
                          />
                        </div>
                      </div>
                    )
                  })()}
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {/* Push to Gist Dialog */}
      {showPushToGistDialog && (
        <Suspense fallback={<LazyModalFallback label="Loading GitHub sharing..." />}>
          <PushToGistDialog
            isOpen
            onClose={() => setShowPushToGistDialog(false)}
            fileContent={fileContent}
            fileName={selectedFile?.name || selectedFile?.path?.split('/').pop() || 'document.md'}
          />
        </Suspense>
      )}

      {/* File Revisions Modal */}
      {showRevisionsModal && (
        <Suspense fallback={<LazyModalFallback label="Loading file history..." />}>
          <FileRevisionsModal
            isOpen
            onClose={() => {
              setShowRevisionsModal(false)
              setRestoreError(null)
            }}
            filepath={selectedFile?.path || ''}
            onRestoreVersion={handleRestoreVersion}
          />
        </Suspense>
      )}

      {/* Restore Error Toast */}
      {restoreError && (
        <div className="fixed bottom-4 right-4 bg-red-500 text-white px-4 py-3 rounded-lg shadow-lg z-50 flex items-center gap-3 max-w-md">
          <div className="flex-1">
            <p className="font-medium">Restore Failed</p>
            <p className="text-sm text-red-100">{restoreError}</p>
          </div>
          <button
            onClick={() => setRestoreError(null)}
            className="text-white hover:text-red-100"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
      )}

      {/* Restore Loading Overlay */}
      {isRestoring && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl p-6 flex flex-col items-center gap-4">
            <Loader2 className="w-8 h-8 animate-spin text-blue-500" />
            <p className="text-gray-900 dark:text-gray-100">Restoring file version...</p>
          </div>
        </div>
      )}
    </>
  )
}

/**
 * Full-viewport shell for surfaces that have no workspace pane to host the
 * viewer in (Video Studio). `fixed inset-0` rather than `absolute` so it
 * needs no positioned ancestor, same as the body's own nested dialogs.
 */
export function FileContentViewerOverlay() {
  const showFileContent = useWorkspaceStore(state => state.showFileContent)
  if (!showFileContent) return null
  return (
    <div className="fixed inset-0 bg-white dark:bg-gray-900 z-40 flex flex-col">
      <FileContentViewerBody variant="overlay" />
    </div>
  )
}
