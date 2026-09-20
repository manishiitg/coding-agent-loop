import { Card } from "./Card"

export interface FormSectionProps {
  title: React.ReactNode
  description?: React.ReactNode
  /** Right-side header action (e.g. an Add button). */
  actions?: React.ReactNode
  children?: React.ReactNode
  className?: string
}

/**
 * A standard settings section: card + title + description. Every settings
 * form builds its blocks from this so headings, spacing, and surfaces
 * stay identical across panes.
 */
export function FormSection({ title, description, actions, children, className }: FormSectionProps) {
  return (
    <Card className={`p-4 ${className ?? ""}`}>
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-sm font-medium text-foreground">{title}</h3>
          {description && <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="shrink-0">{actions}</div>}
      </div>
      {children && <div className="mt-3 space-y-3">{children}</div>}
    </Card>
  )
}
