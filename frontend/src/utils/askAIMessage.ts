// Ask AI messages carry builder-only instructions (which reference doc to
// load, which evidence to use) alongside the user's plain request. The full
// text is what Builder receives, but the chat surface must show only the
// plain request: a small-business owner should never see tool-call syntax,
// skill paths, or config keys in their own chat.
//
// Format (queue-safe: a single string, one block per Ask AI action):
//   [ASK-AI view="Dashboard"]
//   <user-facing request, plain words>
//
//   <builder-only instructions>
//
// Display surfaces collapse each block to `Ask AI · <view> — <request>`
// via askAIDisplayText. This follows the [AUTO-NOTIFICATION] precedent:
// a machine-readable marker with a friendly rendering.

const MARKER_LINE = /^\[ASK-AI view="([^"]+)"\]\s*$/

export type AskAIMessageBlock = {
  view: string
  summary: string
  instructions: string
}

export function buildAskAIMessage(block: { view: string; summary: string; instructions: string }): string {
  return `[ASK-AI view="${block.view}"]\n${block.summary.trim()}\n\n${block.instructions.trim()}`
}

export function hasAskAIMessage(content: string): boolean {
  return content.split('\n').some(line => MARKER_LINE.test(line))
}

/** Split content into Ask AI blocks. Non-marked text is ignored. */
export function parseAskAIMessages(content: string): AskAIMessageBlock[] {
  const blocks: AskAIMessageBlock[] = []
  const lines = content.split('\n')
  let index = 0
  while (index < lines.length) {
    const match = lines[index].match(MARKER_LINE)
    if (!match) {
      index++
      continue
    }
    const view = match[1]
    index++
    const body: string[] = []
    while (index < lines.length && !MARKER_LINE.test(lines[index])) {
      body.push(lines[index])
      index++
    }
    const paragraphs = body.join('\n').split(/\n\s*\n/)
    const summary = (paragraphs[0] ?? '').trim().replace(/\s+/g, ' ')
    const instructions = paragraphs.slice(1).join('\n\n').trim()
    blocks.push({ view, summary, instructions })
  }
  return blocks
}

/**
 * Collapse every Ask AI block to its friendly one-liner, preserving any
 * surrounding human text. Queued messages are combined before send, so one
 * transcript event can hold several blocks; each collapses independently.
 * Content without a marker is returned unchanged.
 */
export function askAIDisplayText(content: string): string {
  if (!hasAskAIMessage(content)) return content
  const lines = content.split('\n')
  const out: string[] = []
  let index = 0
  let lastWasBlock = false
  while (index < lines.length) {
    const match = lines[index].match(MARKER_LINE)
    if (!match) {
      out.push(lines[index])
      lastWasBlock = false
      index++
      continue
    }
    const view = match[1]
    index++
    const body: string[] = []
    while (index < lines.length && !MARKER_LINE.test(lines[index])) {
      body.push(lines[index])
      index++
    }
    const summary = body.join('\n').split(/\n\s*\n/)[0].trim().replace(/\s+/g, ' ')
    // Blank lines between blocks are consumed into the block body above, so
    // re-separate consecutive collapsed blocks explicitly.
    if (lastWasBlock) out.push('')
    out.push(summary ? `Ask AI · ${view} — ${summary}` : `Ask AI · ${view}`)
    lastWasBlock = true
  }
  return out.join('\n').replace(/\n{3,}/g, '\n\n').trim()
}
