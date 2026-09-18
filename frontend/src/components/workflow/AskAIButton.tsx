import { useEffect, useRef, useState } from 'react'
import { MessageCircle, Send, X } from 'lucide-react'
import { useChatStore } from '../../stores/useChatStore'
import { sendWorkspacePaneMessageToChat } from '../../utils/workspacePaneChat'
import ModalPortal from '../ui/ModalPortal'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'

/**
 * Every settings/config panel in the workflow builder (MCP, skills, secrets,
 * LLM, bots, browser, schedules, notify, backup, publish...) only ever shows
 * what's already set up on this deployment or in this workflow — none of
 * them can tell the user that chat itself can search the web, install
 * something new, or explain what's possible beyond that fixed list. This is
 * the fix for that, meant to be dropped into any such panel's header.
 *
 * Reuses sendWorkspacePaneMessageToChat, the single delivery path for every
 * right-pane action
 * uses for a Needs-your-decision card (utils/reportHumanInputChat.ts): it
 * finds or opens the right interactive tab for this workflow, queues
 * correctly behind a running turn, and switches focus to chat. A prefilled
 * textarea does none of that. Defaults to the formatted (not terminal) view,
 * since these are ordinary conversational asks, not something to watch a
 * CLI execute live.
 */
export function AskAIButton({
  workspacePath,
  message,
  onAsk,
  label = 'Ask AI',
  className,
  iconOnly = false,
}: {
  workspacePath: string | null
  message: string
  /** Product surfaces can deliver the message to their own chat lane. */
  onAsk?: (message: string) => void | Promise<void>
  label?: string
  className?: string
  /** Renders just the icon (for a tight icon-toolbar spot) instead of icon+label. */
  iconOnly?: boolean
}) {
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [sending, setSending] = useState(false)
  const [dropdownPosition, setDropdownPosition] = useState({ top: 0, left: 0 })
  const buttonRef = useRef<HTMLButtonElement | null>(null)
  const dropdownRef = useRef<HTMLDivElement | null>(null)
  const buttonClassName = className ?? (iconOnly
    ? 'inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary'
    : 'flex shrink-0 items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary')
  const disabled = !workspacePath && !onAsk

  const updateDropdownPosition = () => {
    const button = buttonRef.current
    if (!button) return
    const rect = button.getBoundingClientRect()
    const width = 320
    const margin = 8
    const left = Math.min(
      Math.max(margin, rect.right - width),
      Math.max(margin, window.innerWidth - width - margin),
    )
    setDropdownPosition({ top: rect.bottom + margin, left })
  }

  const openConfirm = () => {
    updateDropdownPosition()
    setConfirmOpen(true)
  }

  useEffect(() => {
    if (!confirmOpen) return

    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node | null
      if (!target) return
      if (buttonRef.current?.contains(target) || dropdownRef.current?.contains(target)) return
      setConfirmOpen(false)
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        setConfirmOpen(false)
      }
    }
    const handleViewportChange = () => updateDropdownPosition()

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    window.addEventListener('resize', handleViewportChange)
    window.addEventListener('scroll', handleViewportChange, true)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('resize', handleViewportChange)
      window.removeEventListener('scroll', handleViewportChange, true)
    }
  }, [confirmOpen])

  const handleSend = async () => {
    if (disabled || sending) return
    setSending(true)
    if (onAsk) {
      try {
        await onAsk(message)
        setConfirmOpen(false)
      } catch (err) {
        useChatStore.getState().addToast(err instanceof Error ? err.message : 'Failed to open chat.', 'error')
      } finally {
        setSending(false)
      }
      return
    }

    if (!workspacePath) {
      setSending(false)
      return
    }

    try {
      await sendWorkspacePaneMessageToChat({ workspacePath, message })
      setConfirmOpen(false)
    } catch (err) {
      useChatStore.getState().addToast(err instanceof Error ? err.message : 'Failed to open chat.', 'error')
    } finally {
      setSending(false)
    }
  }

  return (
    // A native `title` attribute is unreliable here: this button is disabled
    // when there's no workspacePath, and several browsers suppress the title
    // tooltip on disabled elements entirely. The app-wide Tooltip component
    // (backed by the TooltipProvider in App.tsx) doesn't have that problem.
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <button ref={buttonRef} type="button" onClick={openConfirm} aria-label={iconOnly ? label : undefined} className={buttonClassName} disabled={disabled}>
            <MessageCircle className="h-3.5 w-3.5" />
            {!iconOnly && label}
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">{label}</TooltipContent>
      </Tooltip>
      {confirmOpen && (
        <ModalPortal>
          <div
            ref={dropdownRef}
            role="dialog"
            aria-modal="false"
            aria-labelledby="ask-ai-preview-title"
            className="fixed z-[10000] w-80 rounded-lg border border-border bg-background shadow-xl"
            style={{ top: dropdownPosition.top, left: dropdownPosition.left }}
          >
            <button
              type="button"
              onClick={() => setConfirmOpen(false)}
              disabled={sending}
              aria-label="Close Ask AI confirmation"
              className="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-60"
            >
              <X className="h-4 w-4" />
            </button>
            <div className="px-4 pt-4 pr-10">
              <h2 id="ask-ai-preview-title" className="text-sm font-semibold text-foreground">
                Ask AI about this view
              </h2>
            </div>
            <div className="flex justify-end px-4 py-4">
              <button
                type="button"
                onClick={() => { void handleSend() }}
                disabled={sending}
                className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-60"
              >
                <Send className="h-3.5 w-3.5" />
                {sending ? 'Sending...' : 'Send to chat'}
              </button>
            </div>
          </div>
        </ModalPortal>
      )}
    </>
  )
}
