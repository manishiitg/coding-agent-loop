import ThemeDropdown from '../ThemeDropdown'
import { RunloopLockup } from '../branding/RunloopLogo'

export default function SidebarHeader() {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <RunloopLockup className="flex-1 min-w-0" />
      <div className="shrink-0">
        <ThemeDropdown compact />
      </div>
    </div>
  )
}
