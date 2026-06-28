import { LogOut, User } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { useAuthStore } from '../../stores/useAuthStore'

/**
 * AccountControl - signed-in user indicator + sign-out. Rendered only in
 * multi-user mode with an authenticated user.
 */
export default function AccountControl() {
  const { user, logout, isMultiUserMode } = useAuthStore()
  if (!isMultiUserMode || !user) return null

  return (
    <>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="w-7 h-7 rounded-full bg-primary/10 flex items-center justify-center cursor-default">
            <User className="w-4 h-4 text-primary" />
          </div>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          <div className="text-xs">
            <p className="font-medium">{user.username || user.email || 'User'}</p>
            {user.email && user.username !== user.email && (
              <p className="text-gray-300">{user.email}</p>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={logout}
            aria-label="Sign out"
            className="p-1.5 rounded-md text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          >
            <LogOut className="w-4 h-4" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">Sign out</TooltipContent>
      </Tooltip>
    </>
  )
}
