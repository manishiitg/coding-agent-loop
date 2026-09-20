import { Switch } from "./Switch"

export interface ToggleRowProps {
  label: string
  description?: string
  checked: boolean
  onCheckedChange?: (checked: boolean) => void
  disabled?: boolean
  /** Tooltip shown when disabled (permission reason). */
  disabledTitle?: string
  ariaLabel?: string
}

/**
 * A standard enable row for settings forms: label + description on the
 * left, switch on the right. Replaces every hand-rolled peer-checkbox
 * toggle.
 */
export function ToggleRow({
  label,
  description,
  checked,
  onCheckedChange,
  disabled = false,
  disabledTitle,
  ariaLabel,
}: ToggleRowProps) {
  return (
    <div className="flex items-center justify-between gap-3">
      <div>
        <p className="text-sm font-medium text-foreground">{label}</p>
        {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
      </div>
      <span title={disabled ? disabledTitle : undefined} className="shrink-0">
        <Switch
          checked={checked}
          onCheckedChange={onCheckedChange}
          disabled={disabled}
          aria-label={ariaLabel ?? label}
        />
      </span>
    </div>
  )
}
