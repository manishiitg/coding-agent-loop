import { TooltipProvider } from './ui/tooltip'
import ThemeToggle from './topbar/ThemeToggle'
import { WorkspaceConnectionSwitcher } from './WorkspaceConnectionSwitcher'
import LlmModalHost from './topbar/LlmModalHost'
import LlmTriggerButton from './topbar/LlmTriggerButton'
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
      {/* LlmModalHost renders the LLM modals once (no visible UI); the global
          Models trigger lives here in the top bar as <LlmTriggerButton>. */}
      <LlmModalHost />
      <div className="flex items-center gap-1.5">
        <LlmTriggerButton />
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
