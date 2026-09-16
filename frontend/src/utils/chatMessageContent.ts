const TRANSPORT_CONTEXT_MARKERS = [
  '\n\n📁 Files in context:',
  '\n\nPrevious workflow-builder conversation file:',
]

/** Remove request-only context that was not typed by the user. */
export function getDisplaySafeUserMessageContent(content: string): string {
  const markerIndexes = TRANSPORT_CONTEXT_MARKERS
    .map(marker => content.indexOf(marker))
    .filter(index => index >= 0)
  const end = markerIndexes.length > 0 ? Math.min(...markerIndexes) : content.length
  return content.slice(0, end).trim()
}
