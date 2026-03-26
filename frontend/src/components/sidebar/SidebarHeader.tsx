import ThemeDropdown from '../ThemeDropdown'
import { AgentForgeLockup } from '../branding/AgentForgeLogo'

export default function SidebarHeader() {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <AgentForgeLockup className="flex-1 min-w-0" />
      <div className="shrink-0">
        <ThemeDropdown compact />
      </div>
    </div>
  )
}
