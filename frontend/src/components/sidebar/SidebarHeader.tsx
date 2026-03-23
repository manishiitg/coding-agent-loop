import ThemeDropdown from '../ThemeDropdown'
import { AgentForgeLockup } from '../branding/AgentForgeLogo'

export default function SidebarHeader() {
  return (
    <div className="flex items-center justify-between">
      <AgentForgeLockup />
      <ThemeDropdown />
    </div>
  )
}
