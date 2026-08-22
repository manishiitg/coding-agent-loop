import { useEffect, useState } from 'react'
import { playNotificationSound } from '../../utils/sound'

interface NotificationsToggle {
  /** OS-level permission, re-checked whenever the window regains focus. */
  osPermission: NotificationPermission
  enabled: boolean
  /** Blocked at the OS level — the in-app preference cannot help. */
  blocked: boolean
  description: string
  toggle: () => void
}

/**
 * Desktop notification preference plus the OS permission behind it. Extracted
 * from the old top-bar icon so the Workspace Tools drawer can render it as a
 * row with its state spelled out instead of encoded in a bell glyph.
 */
export function useNotificationsToggle(): NotificationsToggle {
  const [osPermission, setOsPermission] = useState<NotificationPermission>('default')
  const [enabled, setEnabled] = useState(() => {
    return localStorage.getItem('mcp_notifications_enabled') !== 'false' // Default true
  })

  useEffect(() => {
    if (!('Notification' in window)) return

    // Initial permission check + re-check when the window regains focus
    // (the user might have changed it in system settings).
    setOsPermission(Notification.permission)
    const handleFocus = () => setOsPermission(Notification.permission)
    window.addEventListener('focus', handleFocus)
    return () => window.removeEventListener('focus', handleFocus)
  }, [])

  const testNotification = () => {
    playNotificationSound()

    // Set Dock badge for test
    if (window.electronAPI?.setDockBadge) {
      window.electronAPI.setDockBadge('1')
      // Clear after 5 seconds for test
      setTimeout(() => {
        window.electronAPI?.setDockBadge?.('')
      }, 5000)
    }

    if (!('Notification' in window)) return

    if (Notification.permission === 'granted') {
      new Notification('Test Notification', {
        body: 'This is a test notification from AgentWorks',
        icon: '/logo.svg'
      })
    } else if (Notification.permission === 'default') {
      Notification.requestPermission().then(permission => {
        setOsPermission(permission)
        if (permission === 'granted') {
          new Notification('Test Notification', {
            body: 'This is a test notification from AgentWorks',
            icon: '/logo.svg'
          })
        }
      })
    }
  }

  const blocked = osPermission === 'denied'

  const toggle = () => {
    if (blocked) {
      alert('Notifications are blocked by your system settings. Please enable them in System Settings > Notifications > AgentWorks.')
      return
    }

    const nextValue = !enabled
    setEnabled(nextValue)
    localStorage.setItem('mcp_notifications_enabled', String(nextValue))

    if (nextValue) {
      // Just enabled: trigger test
      testNotification()
    }
  }

  return {
    osPermission,
    enabled,
    blocked,
    description: blocked
      ? 'Blocked by system settings'
      : enabled
        ? 'On — desktop alerts and sound'
        : 'Off',
    toggle,
  }
}
