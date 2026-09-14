import type { ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'

type WorkspaceToolbarGroupProps = {
  label: string
  open: boolean
  onToggle?: () => void
  title: string
  children: ReactNode
} & Record<`data-${string}`, string | undefined>

/** Shared AgentWorks Views/Setup toolbar group used by workflow and product workspaces. */
export function WorkspaceToolbarGroup({ label, open, onToggle, title, children, ...rest }: WorkspaceToolbarGroupProps) {
  return (
    <div {...rest} className="inline-flex h-full items-center gap-0.5 px-1 first:pl-0.5 last:pr-0.5">
      {onToggle ? (
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          title={title}
          className={`inline-flex h-6 items-center gap-1 rounded px-2 text-[11px] font-medium outline-none transition-colors hover:bg-background/70 ${open ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'}`}
        >
          <span>{label}</span>
          {open ? <ChevronDown className="h-3 w-3" aria-hidden="true" /> : <ChevronRight className="h-3 w-3" aria-hidden="true" />}
        </button>
      ) : (
        <span title={title} className="inline-flex h-6 items-center px-2 text-[11px] font-medium text-foreground">{label}</span>
      )}
      {open && children}
    </div>
  )
}
