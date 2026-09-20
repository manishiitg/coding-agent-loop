import React from 'react'

interface InspectorShellProps {
  /** The shell box's classes. */
  className: string
  /** The view's own header row content (title, controls). */
  header: React.ReactNode
  /** Extra classes for the header row wrapper, per view. */
  headerClassName?: string
  children: React.ReactNode
}

/**
 * The right-pane frame shared by the inspector views (Costs, Execution
 * Logs). The view supplies only its header and body.
 */
export default function InspectorShell({
  className,
  header,
  headerClassName = '',
  children,
}: InspectorShellProps) {
  return (
    <div className={className}>
      <div className={headerClassName}>
        {header}
      </div>

      {children}
    </div>
  )
}
