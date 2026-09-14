import type { HTMLAttributes } from 'react'

/** Shared AgentWorks toolbar row used above chat/workspace split surfaces. */
export function WorkspaceTopToolbar({ className = '', ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      {...props}
      className={`relative z-10 flex min-h-10 min-w-0 flex-nowrap items-center gap-3 overflow-visible border-b border-border bg-background px-3 py-1.5 ${className}`}
    />
  )
}
