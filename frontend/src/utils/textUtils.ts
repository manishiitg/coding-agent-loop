// Copy text to clipboard with fallback for non-HTTPS contexts
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
    // Fallback: temporary textarea
    const textarea = document.createElement('textarea')
    textarea.value = text
    textarea.style.position = 'fixed'
    textarea.style.left = '-9999px'
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    document.body.removeChild(textarea)
    return true
  } catch {
    console.error('Failed to copy to clipboard')
    return false
  }
}

// Truncate a title to max characters for tab display
export const truncateTabTitle = (title: string, maxChars: number = 10): string => {
  if (!title) return 'Chat'
  const trimmed = title.trim()
  if (trimmed.length <= maxChars) return trimmed
  return trimmed.slice(0, maxChars).trim() + '...'
}
