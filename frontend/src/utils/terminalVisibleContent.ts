// Terminal seeds can contain only reset, cursor, color, or title-control bytes.
// Those bytes are meaningful to xterm but do not paint anything a user can see.
export function terminalPayloadHasVisibleContent(value: string): boolean {
  if (!value) return false
  const withoutStringControls = value
    .replace(/\x1B\][^\x07]*(?:\x07|\x1B\\)/g, '')
    .replace(/\x1B[PX^_][\s\S]*?\x1B\\/g, '')
  // eslint-disable-next-line no-control-regex
  const withoutCsi = withoutStringControls.replace(/\x1B\[[0-?]*[ -/]*[@-~]/g, '')
  // eslint-disable-next-line no-control-regex
  const withoutEscapes = withoutCsi.replace(/\x1B[ -/]*[@-~]/g, '')
  // eslint-disable-next-line no-control-regex
  return withoutEscapes.replace(/[\x00-\x1F\x7F-\x9F]/g, '').trim().length > 0
}
