import type { WorkflowNotificationState } from '../../services/workflow-notifications'

export function formatNotificationStateLabel(state: WorkflowNotificationState | 'loading'): string {
  switch (state) {
    case 'ready':
      return 'Ready'
    case 'missing_secret':
    case 'invalid_secret':
      return 'Needs attention'
    case 'loading':
      return 'Loading'
    default:
      return 'Not configured'
  }
}

export function getNotificationDotClass(state: WorkflowNotificationState | 'loading'): string {
  switch (state) {
    case 'ready':
      return 'bg-emerald-500'
    case 'missing_secret':
    case 'invalid_secret':
      return 'bg-amber-500'
    case 'loading':
      return 'bg-muted-foreground/40'
    default:
      return 'bg-muted-foreground/60'
  }
}
