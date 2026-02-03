// Truncate a title to max characters for tab display
export const truncateTabTitle = (title: string, maxChars: number = 10): string => {
  if (!title) return 'Chat'
  const trimmed = title.trim()
  if (trimmed.length <= maxChars) return trimmed
  return trimmed.slice(0, maxChars).trim() + '...'
}
