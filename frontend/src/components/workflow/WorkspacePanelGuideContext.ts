import { createContext } from 'react'
import type { WorkspacePanelSurface } from './workspacePanelGuides'

/** AgentWorks is the default; Crew wraps its workspace pane with its own scope. */
export const WorkspacePanelGuideContext = createContext<WorkspacePanelSurface>('agentworks')
