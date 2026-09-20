// Identity icons are either an emoji/short glyph or an uploaded image stored
// as a base64 data URL (see agent_go/pkg/workflowtypes/icons.go). The data
// URL renders everywhere an icon does with no extra fetching.
const IMAGE_ICON_PREFIXES = [
  'data:image/png;base64,',
  'data:image/jpeg;base64,',
  'data:image/gif;base64,',
  'data:image/webp;base64,',
]

export function isImageIcon(icon?: string | null): boolean {
  const trimmed = icon?.trim() || ''
  return IMAGE_ICON_PREFIXES.some(prefix => trimmed.startsWith(prefix) && trimmed.length > prefix.length)
}
