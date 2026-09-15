export function isProviderTranscriptArtifact(content: string): boolean {
  const normalized = content.trim().toLowerCase()
  return normalized.startsWith('[previous tool call:') ||
	 normalized.startsWith('[previous tool call]:') ||
    normalized.startsWith('[previous tool result:') ||
	 normalized.startsWith('[previous tool result]:') ||
    normalized.startsWith('[canceled run context — tools executed before cancellation:') ||
    normalized.startsWith('[cancelled run context — tools executed before cancellation:')
}
