import { useEffect, useRef, useState } from 'react'
import { KeyRound, LogOut, Terminal } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { useAuthStore } from '../../stores/useAuthStore'
import ChangePasswordDialog from './ChangePasswordDialog'
import AccessTokensDialog from './AccessTokensDialog'

/**
 * AccountControl - the signed-in user's avatar (their initial) which opens a
 * small account menu for hosted and local installations. Local single-user
 * installs expose access tokens after automatic session initialization. Same outside-click /
 * Escape behaviour as IconPopover; not reusing it because the trigger here
 * is the round avatar itself rather than a padded icon button.
 */
export default function AccountControl() {
  const { user, logout, isMultiUserMode } = useAuthStore()
  const [open, setOpen] = useState(false)
  const [changingPassword, setChangingPassword] = useState(false)
  const [managingTokens, setManagingTokens] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onMouseDown = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) setOpen(false)
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onMouseDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onMouseDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  if (!user) return null

  const displayName = isMultiUserMode ? (user.username || user.email || 'User') : 'Local account'
  const initial = displayName.trim().charAt(0).toUpperCase() || '?'
  const itemClass =
    'w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm text-left text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors'

  return (
    <div ref={containerRef} className="relative">
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={() => setOpen((prev) => !prev)}
            aria-label={`Account: ${displayName}`}
            aria-haspopup="menu"
            aria-expanded={open}
            className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-semibold select-none transition-colors bg-primary/15 text-primary hover:bg-primary/25 ${open ? 'ring-2 ring-primary/40' : ''}`}
          >
            {initial}
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">{displayName}</TooltipContent>
      </Tooltip>

      {open && (
        <div
          role="menu"
          className="absolute right-0 top-full mt-2 w-56 rounded-lg border border-gray-200 dark:border-slate-700 bg-white dark:bg-slate-800 shadow-xl z-[60] p-1.5"
        >
          <div className="px-2 py-1.5 mb-1 border-b border-gray-200 dark:border-slate-700">
            <p className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">{displayName}</p>
            {isMultiUserMode && user.email && user.username !== user.email && (
              <p className="text-xs text-gray-500 dark:text-gray-400 truncate">{user.email}</p>
            )}
          </div>
          {isMultiUserMode && <button
            type="button"
            role="menuitem"
            className={itemClass}
            onClick={() => {
              setOpen(false)
              setChangingPassword(true)
            }}
          >
            <KeyRound className="w-4 h-4 text-gray-500 dark:text-gray-400" />
            Change password
          </button>}
          <button type="button" role="menuitem" className={itemClass} onClick={() => {
            setOpen(false)
            setManagingTokens(true)
          }}>
            <Terminal className="w-4 h-4 text-gray-500 dark:text-gray-400" />
            Access tokens
          </button>
          {isMultiUserMode && <button
            type="button"
            role="menuitem"
            className={`${itemClass} hover:text-red-600 dark:hover:text-red-400`}
            onClick={() => {
              setOpen(false)
              logout()
            }}
          >
            <LogOut className="w-4 h-4" />
            Sign out
          </button>}
        </div>
      )}

      {managingTokens && <AccessTokensDialog onClose={() => setManagingTokens(false)} />}
      <ChangePasswordDialog isOpen={changingPassword} onClose={() => setChangingPassword(false)} />
    </div>
  )
}
