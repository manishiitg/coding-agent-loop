import { MessageCircle } from 'lucide-react'
import { useChatStore } from '../../stores/useChatStore'
import { sendWorkflowMessageToChat } from '../../utils/reportHumanInputChat'

/**
 * Every settings/config panel in the workflow builder (MCP, skills, secrets,
 * LLM, bots, browser, schedules, notify, backup, publish...) only ever shows
 * what's already set up on this deployment or in this workflow — none of
 * them can tell the user that chat itself can search the web, install
 * something new, or explain what's possible beyond that fixed list. This is
 * the fix for that, meant to be dropped into any such panel's header.
 *
 * Reuses sendWorkflowMessageToChat, the same delivery Pulse's "Ask in chat"
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
  label = 'Ask AI',
  title = "This only shows what's already set up. Ask in chat to search for, install, or explain something that isn't here.",
  className = 'flex shrink-0 items-center gap-1.5 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary',
  iconOnly = false,
}: {
  workspacePath: string | null
  message: string
  label?: string
  title?: string
  className?: string
  /** Renders just the icon (for a tight icon-toolbar spot) instead of icon+label. */
  iconOnly?: boolean
}) {
  const handleClick = () => {
    if (!workspacePath) return
    void sendWorkflowMessageToChat({ workspacePath, message, viewMode: 'formatted' }).catch(err => {
      useChatStore.getState().addToast(err instanceof Error ? err.message : 'Failed to open chat.', 'error')
    })
  }

  return (
    <button type="button" onClick={handleClick} title={title} aria-label={iconOnly ? label : undefined} className={className} disabled={!workspacePath}>
      <MessageCircle className="h-3.5 w-3.5" />
      {!iconOnly && label}
    </button>
  )
}
