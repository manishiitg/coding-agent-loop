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

  const matches = lines.filter(line => (
    line.startsWith('diff --git ') ||
    line.startsWith('index ') ||
    line.startsWith('--- ') ||
    line.startsWith('+++ ') ||
    line.startsWith('@@ ') ||
    line.startsWith('+ ') ||
    line.startsWith('- ')
  )).length

  return matches >= 3
}
