import { useRef, useState } from 'react'
import { Loader2, Upload } from 'lucide-react'
import { Button } from './Button'
import { EntityIdentityIcon } from './EntityIdentityIcon'
import { Input } from './Input'
import { isImageIcon } from './entityIcon'
import { downscaleIconFile } from '../../utils/downscaleImage'

// Shared icon editor for Crew projects and automations: an emoji/glyph
// field plus an image upload that stores a downscaled thumbnail inline.
// Uploading only edits the draft; the parent Save persists it.
export function IconUploadField({ value, onChange, label, disabled, inputAriaLabel }: {
  value: string
  onChange: (value: string) => void
  label: string
  disabled?: boolean
  inputAriaLabel: string
}) {
  const fileInput = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const busy = disabled || uploading

  const upload = async (file: File | undefined) => {
    if (!file || busy) return
    setUploading(true)
    setError(null)
    try {
      onChange(await downscaleIconFile(file))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not use that image.')
    } finally {
      setUploading(false)
      if (fileInput.current) fileInput.current.value = ''
    }
  }

  return (
    <div>
      <div className="flex items-center gap-3">
        <EntityIdentityIcon icon={value.trim() || undefined} label={label} className="h-9 w-9 rounded-lg text-lg" />
        <Input
          value={isImageIcon(value) ? '' : value}
          onChange={event => { setError(null); onChange(Array.from(event.target.value).slice(0, 8).join('')) }}
          disabled={busy}
          placeholder={isImageIcon(value) ? 'Custom image — remove it to use an emoji' : 'Paste an emoji'}
          aria-label={inputAriaLabel}
          readOnly={isImageIcon(value)}
          className="min-w-0 flex-1"
        />
        <input
          ref={fileInput}
          type="file"
          accept="image/png,image/jpeg,image/gif,image/webp"
          className="hidden"
          aria-label="Upload icon image"
          disabled={busy}
          onChange={event => void upload(event.target.files?.[0])}
        />
        <Button size="sm" variant="outline" disabled={busy} onClick={() => fileInput.current?.click()}>
          {uploading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Upload className="h-4 w-4" />}
          {uploading ? 'Uploading…' : 'Upload'}
        </Button>
        {isImageIcon(value) && (
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => { setError(null); onChange('') }}>
            Remove
          </Button>
        )}
      </div>
      {error && <p className="mt-2 text-xs text-destructive">{error}</p>}
    </div>
  )
}
