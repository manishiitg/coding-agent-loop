export type ComposerTriggerKind = '/' | '@' | '#' | '!' | '$'
export interface ComposerTrigger {
  kind: ComposerTriggerKind
  start: number
  end: number
  query: string
}

/** Only the token at the caret can open a picker. URLs/emails are ordinary text. */
export function getComposerTrigger(text: string, caret: number, selectionEnd = caret): ComposerTrigger | null {
  if (caret !== selectionEnd) return null
  const before = text.slice(0, caret)
  const match = /(?:^|\s)([@/#!$])((?:[^\s]*)|"(?:\\.|[^"\\\n])*)$/u.exec(before)
  if (!match) return null
  const kind = match[1] as ComposerTriggerKind
  const raw = match[2]
  const start = caret - raw.length - 1
  if (kind === '@') {
    const quoted = raw.startsWith('"')
    if (quoted ? /(?<!\\)"/.test(raw.slice(1)) : /["<>]/.test(raw)) return null
    // Quoting permits spaces and non-ASCII filenames without consuming the next sentence.
    const query = quoted ? raw.slice(1).replace(/\\(["\\])/g, '$1') : raw
    let end = caret
    if (quoted) {
      const tail = /^(?:\\.|[^"\\\n])*"?/.exec(text.slice(caret))?.[0] ?? ''
      end += tail.length
    } else {
      end += /^[^\s]*/u.exec(text.slice(caret))![0].length
    }
    return { kind, start, end, query }
  }
  if (!/^[\p{L}\p{N}_:-]*$/u.test(raw)) return null
  // A caret inside a URL/path must not turn its first segment into a command.
  const tail = /^[^\s]*/u.exec(text.slice(caret))![0]
  if (kind === '/' && /[/.\\]/.test(tail)) return null
  return { kind, start, end: caret + tail.length, query: raw }
}

export function replaceComposerTrigger(text: string, trigger: Pick<ComposerTrigger, 'start' | 'end'>, replacement: string) {
  return { text: text.slice(0, trigger.start) + replacement + text.slice(trigger.end), caret: trigger.start + replacement.length }
}

export function formatFileReference(path: string): string {
  return '@' + (/\s|"/.test(path) ? JSON.stringify(path) : path)
}

export function fileReferenceRanges(text: string, path: string): Array<{ start: number; end: number }> {
  const spellings = [...new Set([formatFileReference(path), '@' + path])].sort((a, b) => b.length - a.length)
  const ranges: Array<{ start: number; end: number }> = []
  for (let start = 0; start < text.length; start++) {
    if (text[start] !== '@' || (start > 0 && !/\s/.test(text[start - 1]))) continue
    const spelling = spellings.find(value => {
      if (!text.startsWith(value, start)) return false
      const tail = text.slice(start + value.length)
      return !tail || /^(?:[\s,;:!?\)\]}]|\.(?:\s|$))/.test(tail)
    })
    if (spelling) {
      ranges.push({ start, end: start + spelling.length })
      start += spelling.length - 1
    }
  }
  return ranges
}

export function removeFileReferences(text: string, path: string): string {
  for (const range of fileReferenceRanges(text, path).reverse()) text = text.slice(0, range.start) + text.slice(range.end)
  return text
}

export function reconcileFileReferences<T extends { path: string }>(previous: string, next: string, files: T[]): T[] {
  return files.filter(file => !fileReferenceRanges(previous, file.path).length || fileReferenceRanges(next, file.path).length > 0)
}

export function isPlainPickerKey(event: { isComposing?: boolean; keyCode?: number; shiftKey: boolean; ctrlKey: boolean; metaKey: boolean; altKey: boolean }): boolean {
  return !event.isComposing && event.keyCode !== 229 && !event.shiftKey && !event.ctrlKey && !event.metaKey && !event.altKey
}
