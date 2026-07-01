export function normalizeAnsiForEmbeddedXterm(input: string): string {
  // Preserve CLI ANSI foregrounds and styles. Drop only Claude Code's neutral
  // bg-237 canvas/fill, which the app can receive across assistant body text
  // during tmux backfill and which renders as an unnatural gray panel in xterm.
  return input.replace(/\x1b\[([0-9;:]*)m/g, (match, rawParams: string) => {
    if (rawParams === '') return match
    const params = rawParams.split(';')
    const kept: string[] = []
    let removedNeutralBackground = false

    for (let i = 0; i < params.length; i++) {
      const raw = params[i]
      const codeText = raw.includes(':') ? raw.split(':', 1)[0] : raw
      const code = Number(codeText)
      if (!Number.isFinite(code)) {
        kept.push(raw)
        continue
      }

      if (code === 48) {
        if (raw.includes(':')) {
          if (isNeutralColonBackground(raw)) {
            removedNeutralBackground = true
            continue
          }
          kept.push(raw)
          continue
        }

        const mode = Number(params[i + 1])
        if (mode === 2) {
          const red = Number(params[i + 2])
          const green = Number(params[i + 3])
          const blue = Number(params[i + 4])
          if (isNeutralRgbBackground(red, green, blue)) {
            removedNeutralBackground = true
          } else {
            kept.push(raw, params[i + 1], params[i + 2], params[i + 3], params[i + 4])
          }
          i += 4
          continue
        }

        if (mode === 5) {
          const color = Number(params[i + 2])
          if (isNeutralPaletteBackground(color)) {
            removedNeutralBackground = true
          } else {
            kept.push(raw, params[i + 1], params[i + 2])
          }
          i += 2
          continue
        }
      }

      if (code === 38 && !raw.includes(':')) {
        const mode = Number(params[i + 1])
        if (mode === 2) {
          kept.push(raw, params[i + 1], params[i + 2], params[i + 3], params[i + 4])
          i += 4
          continue
        }
        if (mode === 5) {
          const color = Number(params[i + 2])
          kept.push(raw, params[i + 1], removedNeutralBackground && color === 239 ? '244' : params[i + 2])
          i += 2
          continue
        }
      }

      kept.push(raw)
    }

    return kept.length > 0 ? `\x1b[${kept.join(';')}m` : ''
  })
}

function isNeutralPaletteBackground(color: number): boolean {
  return color === 237
}

function isNeutralRgbBackground(red: number, green: number, blue: number): boolean {
  return red === 58 && green === 58 && blue === 58
}

function isNeutralColonBackground(raw: string): boolean {
  const parts = raw.split(':')
  if (parts[0] !== '48') return false
  if (parts[1] === '5') return isNeutralPaletteBackground(Number(parts[2]))
  if (parts[1] === '2') {
    const rgb = parts.slice(2).filter(part => part !== '').map(Number)
    if (rgb.length < 3) return false
    return isNeutralRgbBackground(rgb[0], rgb[1], rgb[2])
  }
  return false
}
