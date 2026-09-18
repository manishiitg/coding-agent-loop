import { useEffect, useMemo } from 'react'
import { sendWorkspacePaneMessageToChat } from '../../../utils/workspacePaneChat'
import { ReportChatRequestController } from './reportChatRequest'

export function useReportChat(workspacePath: string) {
  const controller = useMemo(() => new ReportChatRequestController(workspacePath, sendWorkspacePaneMessageToChat), [workspacePath])
  useEffect(() => {
    controller.activate()
    return controller.dispose
  }, [controller])
  return controller
}
