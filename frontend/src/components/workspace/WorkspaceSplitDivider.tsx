import type { KeyboardEvent, PointerEvent, ReactNode } from 'react'
import { GripVertical, PanelLeftClose, PanelRightClose } from 'lucide-react'

type WorkspaceSplitDividerProps = {
  ratio: number
  onPointerDown: (event: PointerEvent<HTMLButtonElement>) => void
  onStep: (delta: number) => void
  children?: ReactNode
  className?: string
}

/** The shared AgentWorks chat/workspace resize rail. */
export function WorkspaceSplitDivider({ ratio, onPointerDown, onStep, children, className = '' }: WorkspaceSplitDividerProps) {
  const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    onStep(event.key === 'ArrowLeft' ? -0.02 : 0.02)
  }

  return (
    <div className={`group/split relative z-30 hidden min-h-0 w-0 justify-self-start md:col-start-2 md:block ${className}`}>
      <button
        type="button"
        onPointerDown={onPointerDown}
        onKeyDown={handleKeyDown}
        className="absolute -left-1.5 inset-y-0 z-10 w-3 cursor-col-resize touch-none outline-none"
        aria-label="Resize chat and workspace panels"
        aria-orientation="vertical"
        role="separator"
        aria-valuemin={15}
        aria-valuemax={85}
        aria-valuenow={Math.round(ratio * 100)}
      >
        <span className="absolute bottom-0 left-1/2 top-0 w-px -translate-x-1/2 bg-border transition-colors group-hover/split:bg-primary group-focus-within/split:bg-primary" />
        <span className="absolute left-1/2 top-1/2 flex h-6 w-3.5 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full border border-border bg-background text-muted-foreground shadow-sm transition-colors group-hover/split:border-primary group-hover/split:text-primary group-focus-within/split:border-primary group-focus-within/split:text-primary">
          <GripVertical className="h-3 w-3" />
        </span>
      </button>
      {children ? (
        <div className="pointer-events-none absolute left-0 top-1/2 z-20 flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-0.5 rounded-md border border-border bg-background/95 p-0.5 shadow-lg backdrop-blur-sm opacity-0 transition-opacity group-hover/split:opacity-100 group-focus-within/split:opacity-100">
          {children}
        </div>
      ) : null}
    </div>
  )
}

type WorkspaceSplitCollapseControlsProps = {
  onCollapseChat: () => void
  onCollapseWorkspace: () => void
}

/** Shared AgentWorks/Work controls rendered inside the resize rail. */
export function WorkspaceSplitCollapseControls({
  onCollapseChat,
  onCollapseWorkspace,
}: WorkspaceSplitCollapseControlsProps) {
  return (
    <>
      <button
        type="button"
        onPointerDown={event => event.stopPropagation()}
        onClick={onCollapseChat}
        className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        aria-label="Collapse chat panel"
        title="Collapse chat panel"
      >
        <PanelLeftClose className="h-3 w-3" />
      </button>
      <button
        type="button"
        onPointerDown={event => event.stopPropagation()}
        onClick={onCollapseWorkspace}
        className="pointer-events-auto flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        aria-label="Collapse workspace panel"
        title="Collapse workspace panel"
      >
        <PanelRightClose className="h-3 w-3" />
      </button>
    </>
  )
}
