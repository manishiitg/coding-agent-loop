const DIFF_FILE_EXTENSIONS = ['.diff', '.patch']

export const isDiffFilePath = (filepath: string): boolean => {
  const lowerPath = filepath.toLowerCase()
  return DIFF_FILE_EXTENSIONS.some(ext => lowerPath.endsWith(ext))
}

export const looksLikeDiffContent = (content: string): boolean => {
  const lines = content.split('\n').slice(0, 12)
  if (lines.length === 0) {
    return false
  }

  const hasGitHeader = lines.some(line => line.startsWith('diff --git ') || line.startsWith('index '))
  const hasFileHeaders = lines.some(line => line.startsWith('--- ')) && lines.some(line => line.startsWith('+++ '))
  const hasHunkHeader = lines.some(line => line.startsWith('@@ '))
  const additions = lines.filter(line => line.startsWith('+ ') && !line.startsWith('+++ ')).length
  const deletions = lines.filter(line => line.startsWith('- ') && !line.startsWith('--- ')).length

  if (hasHunkHeader) {
    return true
  }

  if (hasFileHeaders && (additions > 0 || deletions > 0)) {
    return true
  }

  if (hasGitHeader && (hasFileHeaders || additions + deletions >= 2)) {
    return true
  }

  return false
}
