import { KeyRound } from 'lucide-react'
import IconPopover from '../ui/IconPopover'
import { SecretsSection } from '../secrets'

/** Secrets popover trigger. */
export default function SecretsControl() {
  return (
    <IconPopover
      icon={<KeyRound className="w-4 h-4" />}
      label="Secrets"
      dataTour="sidebar-secrets"
      dataTestid="tour-sidebar-secrets"
    >
      <SecretsSection />
    </IconPopover>
  )
}
