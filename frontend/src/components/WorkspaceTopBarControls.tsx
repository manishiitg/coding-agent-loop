import { TooltipProvider } from './ui/tooltip'
import RuntimeHealthControl from './topbar/RuntimeHealthControl'
import AccountControl from './topbar/AccountControl'

interface WorkspaceTopBarControlsProps {
  onOpenWalkthrough: () => void
  onOpenShortcuts: () => void
}

export default function WorkspaceTopBarControls(props: WorkspaceTopBarControlsProps) {
  return (
    <TooltipProvider delayDuration={400}>
      <div className="flex items-center gap-1.5">
        <RuntimeHealthControl />
        <AccountControl {...props} />
      </div>
    </TooltipProvider>
  )
}
