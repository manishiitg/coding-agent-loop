import { isValidElement, type ComponentType, type ReactElement, type ReactNode } from 'react'
import { WorkspaceViewTabs, type WorkspaceViewTabOption } from './WorkspaceViewTabs'

export interface WorkspaceViewHeaderTabs {
  value: string
  // Plain strings: every pane keeps its own narrow tab union and adapts
  // at the boundary (the header only ever passes back an option value).
  onChange: (value: string) => void
  options: WorkspaceViewTabOption<string>[]
  ariaLabel: string
}

type WorkspaceViewHeaderProps = {
  /**
   * Leading icon as a component reference (`icon={Activity}`): rendered in
   * the standard h-9 tile spanning the title + subtitle block. A custom
   * element renders as-is at the same footprint.
   */
  icon?: ComponentType<{ className?: string }> | ReactElement
  /** View title. */
  title: ReactNode
  /** Inline context next to the title: counts, timestamps, badges. */
  context?: ReactNode
  /** Muted line under the title row. */
  subtitle?: ReactNode
  /** Right side. Order rule: Ask AI left, refresh right; tertiary controls left of the pair. */
  actions?: ReactNode
  /** Full-width row under the title (stats strip, pills). For tabs use the tabs prop. */
  below?: ReactNode
  /** Standard tab row pinned to the header's bottom edge, after `below`. */
  tabs?: WorkspaceViewHeaderTabs
  /**
   * Per-tab extra action icons, keyed by tab value. The active tab's extras
   * render left of `actions` (tertiary-left-of-pair rule). To change the base
   * pair itself per tab (Ask AI message, refresh handler), compute `actions`
   * from the tab instead — see WorkflowCapabilitiesPanel.
   */
  tabActions?: Partial<Record<string, ReactNode>>
  /** Sticky variant for scroll views (triggers). */
  sticky?: boolean
  /**
   * Bare variant: title row without the bordered header wrapper, for views
   * whose shell owns the row (Costs, Execution Logs via InspectorShell).
   * Visually identical to the standard header.
   */
  bare?: boolean
  className?: string
}

/**
 * The one header for every right-side workspace view — workflow inspectors,
 * capability sections, the automation hub, and Crew panes. Plan and Report
 * are the only views without one (canvas overlays instead of headers).
 *
 * The fixed standard, enforced by this component:
 * - icon: h-9 tile (h-4 glyph) spanning title + subtitle; custom elements
 *   render as-is at the same h-9 footprint
 * - title: text-sm font-semibold, rendered here — never override the size
 * - subtitle: text-xs text-muted-foreground, rendered here
 * - actions: AskAIButton + WorkspaceViewIconButton only (or the
 *   WorkspaceViewActions pair), Ask AI left and refresh right
 */
export function WorkspaceViewHeader({
  icon,
  title,
  context,
  subtitle,
  actions,
  below,
  tabs,
  sticky = false,
  bare = false,
  className = '',
  tabActions,
}: WorkspaceViewHeaderProps) {
  const tabExtra = tabs ? tabActions?.[tabs.value] : undefined
  const renderIcon = () => {
    if (!icon) return null
    if (isValidElement(icon)) return icon
    const Glyph = icon
    return (
      <span className="flex h-9 w-9 items-center justify-center rounded-md border border-primary/25 bg-primary/10 text-primary">
        <Glyph className="h-4 w-4" />
      </span>
    )
  }
  const body = (
    <>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-1 gap-2">
          {icon && <div className="shrink-0">{renderIcon()}</div>}
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="truncate text-sm font-semibold text-foreground">{title}</h2>
              {context}
            </div>
            {subtitle && <p className="mt-0.5 truncate text-xs text-muted-foreground">{subtitle}</p>}
          </div>
        </div>
        {(actions || tabExtra) && <div className="flex shrink-0 items-center gap-2">{tabExtra}{actions}</div>}
      </div>
      {below}
      {tabs && (
        <div className={bare ? 'pt-1' : '-mb-2 pt-1'}>
          <WorkspaceViewTabs value={tabs.value} onChange={tabs.onChange} options={tabs.options} ariaLabel={tabs.ariaLabel} />
        </div>
      )}
    </>
  )
  if (bare) return <>{body}</>
  return (
    <header className={`${sticky ? 'sticky top-0 z-10 border-b border-border bg-background/95 backdrop-blur' : 'shrink-0 border-b border-border'} px-3 py-2 ${className}`}>
      {body}
    </header>
  )
}
