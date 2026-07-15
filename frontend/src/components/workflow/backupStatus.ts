import {
  CheckCircle2,
  Clock3,
  AlertTriangle,
  XCircle,
  Info,
  type LucideIcon
} from 'lucide-react'

// Shared backup-status presentation helpers used by both the dedicated
// WorkflowBackupPopup and the toolbar status dot, so the two never drift.

export type BackupState =
  | 'not_configured'
  | 'local_only'
  | 'configured_not_verified'
  | 'running'
  | 'healthy'
  | 'stale'
  | 'partial'
  | 'failed'
  | 'skipped'
  | string

export const formatBackupStateLabel = (state?: string): string => {
  switch (state) {
    case 'loading':
      return 'Checking status'
    case 'not_configured':
      return 'Not configured'
    case 'local_only':
      return 'Local only'
    case 'configured_not_verified':
      return 'Needs verification'
    case 'running':
      return 'Running'
    case 'healthy':
      return 'Healthy'
    case 'stale':
      return 'Changed since backup'
    case 'partial':
      return 'Partial'
    case 'failed':
      return 'Failed'
    case 'skipped':
      return 'Skipped'
    default:
      return state || 'Unknown'
  }
}

export interface BackupStateVisual {
  Icon: LucideIcon
  badge: string
  icon: string
}

export const getBackupStateVisual = (state?: string): BackupStateVisual => {
  switch (state) {
    case 'not_configured':
    case 'local_only':
      return {
        Icon: AlertTriangle,
        badge: 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300',
        icon: 'text-red-600 dark:text-red-300'
      }
    case 'healthy':
      return {
        Icon: CheckCircle2,
        badge: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
        icon: 'text-emerald-600 dark:text-emerald-300'
      }
    case 'running':
      return {
        Icon: Clock3,
        badge: 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300',
        icon: 'text-sky-600 dark:text-sky-300'
      }
    case 'stale':
    case 'partial':
    case 'configured_not_verified':
      return {
        Icon: AlertTriangle,
        badge: 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
        icon: 'text-amber-600 dark:text-amber-300'
      }
    case 'failed':
      return {
        Icon: XCircle,
        badge: 'border-destructive/30 bg-destructive/10 text-destructive',
        icon: 'text-destructive'
      }
    default:
      return {
        Icon: Info,
        badge: 'border-border bg-muted text-muted-foreground',
        icon: 'text-muted-foreground'
      }
  }
}

// Tailwind classes for the small status dot overlaid on the toolbar backup icon.
export const getBackupDotClass = (state?: string): string => {
  switch (state) {
    case 'not_configured':
    case 'local_only':
      return 'bg-red-500'
    case 'healthy':
      return 'bg-emerald-500'
    case 'running':
      return 'bg-sky-500 animate-pulse'
    case 'stale':
    case 'partial':
    case 'configured_not_verified':
      return 'bg-amber-500'
    case 'failed':
      return 'bg-red-500'
    default:
      return 'bg-muted-foreground/40'
  }
}
