import { TooltipProvider } from './ui/tooltip'
import ThemeToggle from './topbar/ThemeToggle'
import { WorkspaceConnectionSwitcher } from './WorkspaceConnectionSwitcher'
import LlmControl from './topbar/LlmControl'
import McpControl from './topbar/McpControl'
import SkillsControl from './topbar/SkillsControl'
import SecretsControl from './topbar/SecretsControl'
import DesktopAppControl from './topbar/DesktopAppControl'
import NotificationsControl from './topbar/NotificationsControl'
import AccountControl from './topbar/AccountControl'

/**
 * WorkspaceTopBarControls - the config/account controls relocated from the
 * former left WorkspaceSidebar. A slim container that composes one focused
 * component per control; each owns its own trigger and popover/modal wiring.
 */
export default function WorkspaceTopBarControls() {
  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex items-center gap-1.5">
        <LlmControl />
        <McpControl />
        <SkillsControl />
        <SecretsControl />

        <span className="mx-0.5 h-5 w-px bg-gray-200 dark:bg-gray-700" />

        <ThemeToggle />
        <WorkspaceConnectionSwitcher placement="sidebar-minimized" />
        <DesktopAppControl />
        <NotificationsControl />
        <AccountControl />
      </div>
    </TooltipProvider>
  )
}
