import { ServerCog } from 'lucide-react'
import IconPopover from '../ui/IconPopover'
import MCPServersSection from '../sidebar/MCPServersSection'

/** MCP servers popover trigger. */
export default function McpControl() {
  return (
    <IconPopover
      icon={<ServerCog className="w-4 h-4" />}
      label="MCP Servers"
      dataTour="sidebar-mcp-servers"
      dataTestid="tour-sidebar-mcp-servers"
    >
      <MCPServersSection />
    </IconPopover>
  )
}
