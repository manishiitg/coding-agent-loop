import React from 'react'
import { EntityIdentityIcon } from '../ui/EntityIdentityIcon'

export type WorkflowIconProps = {
  icon?: string | null
  label?: string
  className?: string
}

/** A shared workflow identity. Existing manifests get a stable initial badge. */
export function WorkflowIcon({ icon, label, className = '' }: WorkflowIconProps) {
  return <EntityIdentityIcon icon={icon} label={label || 'Automation'} className={className} />
}
