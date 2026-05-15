import ThemeDropdown from '../ThemeDropdown'
import { RunloopLockup } from '../branding/RunloopLogo'

export default function SidebarHeader() {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-2">
      <RunloopLockup className="min-w-0 flex-1" />
      <div className="flex shrink-0 items-center gap-1">
        <ThemeDropdown compact />
      </div>
    </div>
  )
}
