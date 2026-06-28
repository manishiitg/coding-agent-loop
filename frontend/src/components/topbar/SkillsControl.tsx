import { WandSparkles } from 'lucide-react'
import IconPopover from '../ui/IconPopover'
import { SkillsSection } from '../skills'

/** Skills popover trigger. */
export default function SkillsControl() {
  return (
    <IconPopover
      icon={<WandSparkles className="w-4 h-4" />}
      label="Skills"
      dataTour="sidebar-skills"
      dataTestid="tour-sidebar-skills"
    >
      <SkillsSection />
    </IconPopover>
  )
}
