/**
 * Converts standard Markdown to Slack's mrkdwn format.
 *
 * Key differences:
 * - Bold: **text** → *text*
 * - Italic: *text* / _text_ → _text_
 * - Strikethrough: ~~text~~ → ~text~
 * - Links: [text](url) → <url|text>
 * - Images: ![alt](url) → <url|alt>
 * - Headers: # text → *text* (bold, since Slack has no headers)
 * - Code blocks: ```lang → ``` (strip language identifier)
 */
export function convertToSlackMarkdown(md: string): string {
  const lines = md.split('\n')
  const result: string[] = []
  let inCodeBlock = false

  for (let i = 0; i < lines.length; i++) {
    let line = lines[i]

    // Toggle code block state
    if (line.trimStart().startsWith('```')) {
      if (!inCodeBlock) {
        // Opening: strip language identifier
        result.push('```')
        inCodeBlock = true
      } else {
        result.push('```')
        inCodeBlock = false
      }
      continue
    }

    // Inside code blocks, pass through unchanged
    if (inCodeBlock) {
      result.push(line)
      continue
    }

    // Headers → bold text
    const headerMatch = line.match(/^(#{1,6})\s+(.*)/)
    if (headerMatch) {
      const content = convertInline(headerMatch[2])
      result.push(`*${content}*`)
      continue
    }

    // Horizontal rules
    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      result.push('---')
      continue
    }

    // Convert inline formatting
    result.push(convertInline(line))
  }

  return result.join('\n')
}

function convertInline(text: string): string {
  // Protect inline code spans from further processing
  const codeSpans: string[] = []
  text = text.replace(/`([^`]+)`/g, (_, code) => {
    codeSpans.push(code)
    return `\x00CODE${codeSpans.length - 1}\x00`
  })

  // Images: ![alt](url) → <url|alt>
  text = text.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<$2|$1>')

  // Links: [text](url) → <url|text>
  text = text.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<$2|$1>')

  // Bold+Italic: ***text*** or ___text___ → *_text_*
  text = text.replace(/\*{3}(.+?)\*{3}/g, '*_$1_*')
  text = text.replace(/_{3}(.+?)_{3}/g, '*_$1_*')

  // Bold: **text** → *text*
  text = text.replace(/\*{2}(.+?)\*{2}/g, '*$1*')
  // Bold: __text__ → *text*
  text = text.replace(/__(.+?)__/g, '*$1*')

  // Italic: *text* → _text_ (single asterisks not already consumed)
  // Only match single * not preceded/followed by *
  text = text.replace(/(?<!\*)\*(?!\*)(.+?)(?<!\*)\*(?!\*)/g, '_$1_')

  // Strikethrough: ~~text~~ → ~text~
  text = text.replace(/~~(.+?)~~/g, '~$1~')

  // Restore code spans
  text = text.replace(/\x00CODE(\d+)\x00/g, (_, idx) => `\`${codeSpans[Number(idx)]}\``)

  return text
}
