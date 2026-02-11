import React, { useState, useRef, useEffect, useCallback } from 'react'
import { Share2, Copy, Check, Loader2 } from 'lucide-react'
import { useModeStore } from '../stores/useModeStore'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { sessionShareApi } from '../services/api'

interface ChatHeaderProps {
  chatSessionTitle: string
  chatSessionId: string
  sessionState: 'active' | 'completed' | 'loading' | 'error' | 'not-found'
}

export const ChatHeader: React.FC<ChatHeaderProps> = ({
  chatSessionTitle,
  chatSessionId,
  sessionState
}) => {
  const { selectedModeCategory } = useModeStore()
  const { getActivePreset } = usePresetApplication()

  const [showSharePopover, setShowSharePopover] = useState(false)
  const [shareUrl, setShareUrl] = useState('')
  const [isCreatingShare, setIsCreatingShare] = useState(false)
  const [copied, setCopied] = useState(false)
  const popoverRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)

  // Close popover on outside click or Escape
  useEffect(() => {
    if (!showSharePopover) return
    const handleClick = (e: MouseEvent) => {
      if (
        popoverRef.current && !popoverRef.current.contains(e.target as Node) &&
        buttonRef.current && !buttonRef.current.contains(e.target as Node)
      ) {
        setShowSharePopover(false)
      }
    }
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setShowSharePopover(false)
    }
    document.addEventListener('mousedown', handleClick)
    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('mousedown', handleClick)
      document.removeEventListener('keydown', handleKey)
    }
  }, [showSharePopover])

  const handleShare = useCallback(async () => {
    if (showSharePopover) {
      setShowSharePopover(false)
      return
    }
    setIsCreatingShare(true)
    setShareUrl('')
    setCopied(false)
    setShowSharePopover(true)
    try {
      const res = await sessionShareApi.createShare(chatSessionId)
      setShareUrl(`${window.location.origin}/shared/${res.token}`)
    } catch {
      setShareUrl('')
      setShowSharePopover(false)
    } finally {
      setIsCreatingShare(false)
    }
  }, [chatSessionId, showSharePopover])

  const handleCopy = useCallback(async () => {
    if (!shareUrl) return
    await navigator.clipboard.writeText(shareUrl)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [shareUrl])

  // Get active preset for current mode
  const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')

  // Check if header has any content to display
  const hasTitle = chatSessionTitle && !(selectedModeCategory === 'workflow' && activePreset)
  const hasStatus = chatSessionId

  // Don't render if there's no content to show
  if (!hasTitle && !hasStatus) {
    return null
  }

  return (
    <div className="border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
      {/* Session Info Bar - Tier 2 only (Tier 1 moved to ModePresetBar at App level) */}
      <div className="px-4 py-1.5 bg-white dark:bg-gray-900">
            <div className="flex items-center gap-3">
              {/* Session Title - Hide in workflow mode when preset is active to avoid duplication */}
              {hasTitle && (
                <h2 className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">
                  {chatSessionTitle}
                </h2>
              )}

              {/* Session Status */}
              {hasStatus && (
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  {sessionState === 'active' ? 'Live' :
                   sessionState === 'completed' ? 'Historical' :
                   sessionState === 'loading' ? 'Loading...' :
                   sessionState === 'error' ? 'Error' :
                   'Not Found'}
                </span>
              )}

              {/* Spacer */}
              <div className="flex-1" />

              {/* Share Button */}
              {chatSessionId && (
                <div className="relative">
                  <button
                    ref={buttonRef}
                    onClick={handleShare}
                    className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                    title="Share session"
                  >
                    <Share2 size={14} />
                  </button>

                  {showSharePopover && (
                    <div
                      ref={popoverRef}
                      className="absolute right-0 top-full mt-1 z-50 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-3 w-80"
                    >
                      {isCreatingShare ? (
                        <div className="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
                          <Loader2 size={14} className="animate-spin" />
                          Creating share link...
                        </div>
                      ) : (
                        <div className="flex items-center gap-2">
                          <input
                            type="text"
                            readOnly
                            value={shareUrl}
                            className="flex-1 text-xs bg-gray-50 dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded px-2 py-1.5 text-gray-700 dark:text-gray-300 outline-none"
                          />
                          <button
                            onClick={handleCopy}
                            className="flex-shrink-0 p-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400 transition-colors"
                            title="Copy link"
                          >
                            {copied ? <Check size={14} className="text-green-500" /> : <Copy size={14} />}
                          </button>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
    </div>
  )
}
