import { Moon, Sun } from 'lucide-react'
import { useTheme } from '../../hooks/useTheme'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { iconButtonClass } from '../ui/IconPopover'

/**
 * ThemeToggle - a single icon button that toggles between light and dark.
 * Replaces the old dropdown: with only two options a toggle is cleaner and
 * avoids a positioned menu (which overlapped the content below the top bar).
 */
export default function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const isDark = theme === 'dark'

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={() => setTheme(isDark ? 'light' : 'dark')}
          aria-label={isDark ? 'Switch to light theme' : 'Switch to dark theme'}
          className={iconButtonClass}
        >
          {isDark ? <Moon className="w-4 h-4" /> : <Sun className="w-4 h-4" />}
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom">Switch to {isDark ? 'light' : 'dark'} theme</TooltipContent>
    </Tooltip>
  )
}
