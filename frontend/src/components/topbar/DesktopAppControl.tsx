import { Download } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '../ui/tooltip'
import { useIsElectron } from './useIsElectron'

/**
 * DesktopAppControl - "Download Mac App" link. Browser only; renders nothing
 * inside the Electron desktop shell.
 */
export default function DesktopAppControl() {
  const isElectron = useIsElectron()
  if (isElectron) return null

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <a
          href="https://github.com/manishiitg/mcp-agent-builder-go/releases/latest"
          target="_blank"
          rel="noopener noreferrer"
          className="p-1.5 rounded-md text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
          aria-label="Download Mac App"
        >
          <Download className="w-4 h-4" />
        </a>
      </TooltipTrigger>
      <TooltipContent side="bottom">Download Mac App</TooltipContent>
    </Tooltip>
  )
}
