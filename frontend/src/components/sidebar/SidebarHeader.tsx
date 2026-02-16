import { Bot } from 'lucide-react'
import ThemeDropdown from '../ThemeDropdown'

export default function SidebarHeader() {
  return (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <Bot className="w-5 h-5 text-blue-600 dark:text-blue-400 dark-plus:text-blue-400" />
      </div>
      <ThemeDropdown />
    </div>
  )
}
