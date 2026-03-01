import React, { useEffect, useRef, useState, useCallback } from 'react'
import ChatArea from '../ChatArea'
import { useChatStore } from '../../stores/useChatStore'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { CodePrototypeHeader } from './CodePrototypeHeader'
import { DeployDrawer } from './DeployDrawer'
import { NewProjectWizard } from './NewProjectWizard'
import { PreviewPanel } from './PreviewPanel'
import { LogsPanel } from './LogsPanel'
import { useAppStore } from '../../stores/useAppStore'
import { useAuthStore } from '../../stores/useAuthStore'

const MOBILE_WIDTH = 390 // default preview width (iPhone 14 Pro width)

const CodeChatArea: React.FC<{ onNewChat: () => void }> = ({ onNewChat }) => {
  const activeTabId = useChatStore(s => s.activeTabId)
  return (
    <ChatArea
      onNewChat={onNewChat}
      tabId={activeTabId || undefined}
      hideHeader={true}
    />
  )
}

interface Props {
  onNewChat: () => void
}

export const CodePrototypeLayout: React.FC<Props> = ({ onNewChat }) => {
  const { currentProject, showPreview, showLogs } = useCodePrototypeStore()
  const { setWorkspaceMinimized } = useAppStore()
  const authUser = useAuthStore(s => s.user)

  console.log('[PROTOTYPE] CodePrototypeLayout mount — project:', currentProject?.name ?? 'none', 'showPreview:', showPreview)

  const [showWizard, setShowWizard] = useState(!currentProject)

  // Right-column width (px) — defaults to mobile width
  const [rightWidth, setRightWidth] = useState(MOBILE_WIDTH)
  const containerRef = useRef<HTMLDivElement>(null)
  const widthDragging = useRef(false)

  // Logs split ratio (fraction of right column given to logs, 0.2–0.8)
  const [logsHeightPct, setLogsHeightPct] = useState(0.4)
  const rightColRef = useRef<HTMLDivElement>(null)
  const heightDragging = useRef(false)

  // Close workspace whenever preview or logs opens
  useEffect(() => {
    if (showPreview || showLogs) setWorkspaceMinimized(true)
  }, [showPreview, showLogs, setWorkspaceMinimized])

  // Horizontal drag — resize right column width
  const onWidthDragStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    widthDragging.current = true
    const startX = e.clientX
    const startWidth = rightWidth

    const onMove = (ev: MouseEvent) => {
      if (!widthDragging.current || !containerRef.current) return
      const containerW = containerRef.current.getBoundingClientRect().width
      const delta = startX - ev.clientX
      const next = Math.min(containerW * 0.8, Math.max(280, startWidth + delta))
      setRightWidth(next)
    }
    const onUp = () => {
      widthDragging.current = false
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }, [rightWidth])

  // Vertical drag — resize logs height
  const onHeightDragStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    heightDragging.current = true

    const onMove = (ev: MouseEvent) => {
      if (!heightDragging.current || !rightColRef.current) return
      const rect = rightColRef.current.getBoundingClientRect()
      const pct = (ev.clientY - rect.top) / rect.height
      setLogsHeightPct(1 - Math.min(0.8, Math.max(0.2, pct)))
    }
    const onUp = () => {
      heightDragging.current = false
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }, [])

  useEffect(() => {
    if (currentProject) {
      console.log('[PROTOTYPE] project restored from localStorage:', currentProject.name)
      setShowWizard(false)
    }
  }, [currentProject])

  useEffect(() => {
    if (!currentProject || !authUser) return
    const activeTabId = useChatStore.getState().activeTabId
    if (!activeTabId) return
    if (currentProject.config) {
      console.log('[PROTOTYPE] applying project config to tab', activeTabId, '— project:', currentProject.name, currentProject.config)
      useChatStore.getState().setTabConfig(activeTabId, {
        selectedServers: currentProject.config.selected_servers ?? [],
        selectedSecrets: currentProject.config.selected_secrets ?? [],
        selectedSkills: currentProject.config.selected_skills ?? [],
        selectedSubAgents: currentProject.config.selected_subagents ?? [],
      })
    }
  }, [currentProject?.name, authUser])

  const handleNewChat = () => {
    const activeTabId = useChatStore.getState().activeTabId
    if (activeTabId) {
      useChatStore.getState().resetTabChat(activeTabId)
    }
  }

  const showRight = showPreview || showLogs
  const bothVisible = showPreview && showLogs

  return (
    <div ref={containerRef} className="h-full flex flex-col relative">
      <CodePrototypeHeader onNewChat={handleNewChat} />

      <div className="flex-1 min-h-0 overflow-hidden flex">
        {/* Left: chat — fills remaining space */}
        <div className="flex-1 min-w-0">
          <CodeChatArea onNewChat={onNewChat} />
        </div>

        {showRight && (
          <>
            {/* Vertical drag handle — resize right column width */}
            <div
              onMouseDown={onWidthDragStart}
              className="w-2 flex-shrink-0 cursor-col-resize flex items-center justify-center bg-gray-100 dark:bg-gray-800 hover:bg-emerald-100 dark:hover:bg-emerald-900/40 border-x border-gray-200 dark:border-gray-700 group transition-colors"
              title="Drag to resize"
            >
              <div className="flex flex-col gap-0.5 pointer-events-none">
                <div className="w-0.5 h-0.5 rounded-full bg-gray-400 dark:bg-gray-500 group-hover:bg-emerald-500" />
                <div className="w-0.5 h-0.5 rounded-full bg-gray-400 dark:bg-gray-500 group-hover:bg-emerald-500" />
                <div className="w-0.5 h-0.5 rounded-full bg-gray-400 dark:bg-gray-500 group-hover:bg-emerald-500" />
              </div>
            </div>

            {/* Right column — preview + logs */}
            <div
              ref={rightColRef}
              className="flex-shrink-0 flex flex-col min-w-0"
              style={{ width: rightWidth }}
            >
              {showPreview && (
                <div
                  className="min-h-0 flex-shrink-0"
                  style={bothVisible ? { height: `${(1 - logsHeightPct) * 100}%` } : { flex: 1 }}
                >
                  <PreviewPanel />
                </div>
              )}
              {bothVisible && (
                <div
                  onMouseDown={onHeightDragStart}
                  className="h-1 flex-shrink-0 cursor-row-resize bg-gray-200 dark:bg-gray-700 hover:bg-emerald-400 dark:hover:bg-emerald-600 transition-colors"
                  title="Drag to resize"
                />
              )}
              {showLogs && (
                <div
                  className="min-h-0"
                  style={bothVisible ? { height: `${logsHeightPct * 100}%` } : { flex: 1 }}
                >
                  <LogsPanel />
                </div>
              )}
            </div>
          </>
        )}
      </div>

      <DeployDrawer />

      {showWizard && (
        <NewProjectWizard onClose={() => setShowWizard(false)} />
      )}
    </div>
  )
}
