import type { PollingEvent } from '../services/api-types'

const CODING_CLI_PROVIDERS = new Set([
  'claude-code',
  'codex-cli',
  'cursor-cli',
  'pi-cli',
  'muse-cli',
])

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' ? value as Record<string, unknown> : undefined
}

// Retained coding-CLI turns can contain more than one queued user message and
// therefore more than one committed assistant reply. The live completion event
// carries only the last reply; the provider's native transcript is the durable
// source for the complete ordered exchange.
export function codingCliCompletionNeedsTranscriptReconciliation(event: PollingEvent): boolean {
  if (event.type !== 'unified_completion') return false

  const outer = asRecord(event.data)
  const payload = asRecord(outer?.data) || outer
  const metadata = asRecord(payload?.metadata) || asRecord(outer?.metadata)
  const provider = typeof metadata?.provider === 'string'
    ? metadata.provider.trim().toLowerCase()
    : ''
  const source = typeof metadata?.source === 'string'
    ? metadata.source.trim().toLowerCase()
    : ''
  const terminalFormat = metadata?.coding_agent_terminal_format === true

  return CODING_CLI_PROVIDERS.has(provider) && (
    source === 'mcpagent_session' || source === 'coding_agent_sidecar' || terminalFormat
  )
}
