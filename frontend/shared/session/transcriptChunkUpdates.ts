import type { PollingEvent } from './types'

// A coding CLI (codex, claude-code) narrates while it works: "I'll separate
// original posts from replies and count both by day", then tool calls, then
// the answer. The backend carries each of those narrations as a whole-message
// streaming_chunk (source "transcript", is_delta false). Streaming packets only
// feed the transient live buffer, so the moment the next chunk or the
// completion replaced it, that sentence was gone from the chat -- while the
// tmux pane still showed it. The durable restore already turns the same
// messages into intermediate llm_generation_end rows (restore.ts, "preserve
// every readable update"); this makes the live path produce the identical row
// so a turn reads the same whether you watched it or reloaded it. The final
// chunk repeats the answer and is dropped against the completion card by
// dropAnswersRepeatedByCompletionCard.
export function intermediateUpdateFromTranscriptChunk(event: PollingEvent): PollingEvent | null {
  if (event.type !== 'streaming_chunk' || !event.id) return null
  const outer = (event.data && typeof event.data === 'object' ? event.data : {}) as Record<string, unknown>
  const inner = (outer.data && typeof outer.data === 'object' ? outer.data : outer) as Record<string, unknown>
  const metadata = (inner.metadata && typeof inner.metadata === 'object' ? inner.metadata : {}) as Record<string, unknown>

  const source = String(inner.source ?? outer.source ?? metadata.source ?? '').trim().toLowerCase()
  if (source !== 'transcript') return null
  if (inner.is_delta === true || outer.is_delta === true) return null
  if (metadata.kind === 'terminal' || metadata.replace === true) return null
  if (inner.is_tool_call === true) return null
  const content = typeof inner.content === 'string' ? inner.content.trim() : ''
  if (!content) return null

  const timestamp = event.timestamp || (typeof inner.timestamp === 'string' ? inner.timestamp : new Date().toISOString())
  return {
    ...event,
    id: `${event.id}-update`,
    type: 'llm_generation_end',
    timestamp,
    data: {
      ...outer,
      type: 'llm_generation_end',
      data: {
        ...inner,
        content,
        result: content,
        status: 'completed',
        restored_intermediate_update: true,
      },
    } as PollingEvent['data'],
  }
}
