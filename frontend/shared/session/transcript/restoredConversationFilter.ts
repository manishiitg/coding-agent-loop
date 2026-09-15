const providerTranscriptArtifactPrefixes = [
  '[previous tool call:',
  '[previous tool call]:',
  '[previous tool result:',
  '[previous tool result]:',
]

const canceledRunContextPrefixes = [
  '[canceled run context — tools executed before cancellation:',
  '[cancelled run context — tools executed before cancellation:',
]

function isProviderTranscriptArtifactLine(content: string): boolean {
  const normalized = content.trim().toLowerCase()
  return providerTranscriptArtifactPrefixes.some(prefix => normalized.startsWith(prefix))
}

export function sanitizeProviderTranscriptContent(content: string): string {
  const normalized = content.trim().toLowerCase()
  if (canceledRunContextPrefixes.some(prefix => normalized.startsWith(prefix))) return ''
  return content
    .split('\n')
    .filter(line => !isProviderTranscriptArtifactLine(line))
    .join('\n')
    .trim()
}

export function isProviderTranscriptArtifact(content: string): boolean {
  return content.trim().length > 0 && sanitizeProviderTranscriptContent(content).length === 0
}
