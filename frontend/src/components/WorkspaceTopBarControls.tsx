import { useEffect, useState, type ReactNode } from 'react'
import {
  Activity,
  Bell,
  BellOff,
  BrainCircuit,
  Download,
  KeyRound,
  LayoutGrid,
  LogOut,
  Plug,
  WandSparkles,
} from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from './ui/tooltip'
import LlmModalHost from './topbar/LlmModalHost'
import RuntimeHealthPanel from './topbar/RuntimeHealthPanel'
import WorkspaceToolsDrawer, {
  type WorkspaceToolItem,
  type WorkspaceToolPage,
  type WorkspaceToolSection,
} from './topbar/WorkspaceToolsDrawer'
import { useNotificationsToggle } from './topbar/useNotificationsToggle'
import { ConnectionsModal } from './connections'
import { SkillsSection } from './skills'
import { SecretsSection } from './secrets'
import { useLLMStore } from '../stores'
import { useConnectionsStore } from '../stores/useConnectionsStore'
import { useAuthStore } from '../stores/useAuthStore'
import { useIsElectron } from './topbar/useIsElectron'

type ToolPage = 'skills' | 'secrets' | 'runtime'

const PAGE_CONFIG: Record<ToolPage, { title: string; icon: ReactNode; content: ReactNode }> = {
  skills: {
    title: 'Skills',
    icon: <WandSparkles className="h-4 w-4" />,
    content: <SkillsSection />,
  },
  secrets: {
    title: 'Secrets',
    icon: <KeyRound className="h-4 w-4" />,
    content: <SecretsSection />,
  },
  runtime: {
    title: 'Runtime Health',
    icon: <Activity className="h-4 w-4" />,
    content: <RuntimeHealthPanel />,
  },
}

interface WorkspaceTopBarControlsProps {
  /**
   * Sections contributed by the bar that hosts this button — the automation
   * controls whose state lives up there (bots, schedules, walkthrough).
   */
  sections?: WorkspaceToolSection[]
}

/**
 * WorkspaceTopBarControls - a single "Workspace Tools" entry point in the top
 * bar. Everything that used to be a separate unlabelled icon (runtime health,
 * connections, notifications, account, models, skills, secrets) now lives in
 * the right-hand drawer this button opens, named and grouped.
 */
export default function WorkspaceTopBarControls({ sections = [] }: WorkspaceTopBarControlsProps) {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [activePage, setActivePage] = useState<ToolPage | null>(null)
  const [connectionsOpen, setConnectionsOpen] = useState(false)

  const setShowLLMModal = useLLMStore(s => s.setShowLLMModal)
  const llmCount = useLLMStore(s => s.savedLLMs.length)
  const connectionsSummary = useConnectionsStore(s => s.summary)
  const loadConnections = useConnectionsStore(s => s.loadConnections)
  const { user, logout, isMultiUserMode } = useAuthStore()
  const notifications = useNotificationsToggle()
  const isElectron = useIsElectron()

  // The badge on the button is the only connection health signal left in the
  // top bar, so it has to be loaded whether or not the drawer is ever opened.
  useEffect(() => {
    loadConnections()
  }, [loadConnections])

  const needsAttention = connectionsSummary.needs_attention > 0

  const closeDrawer = () => {
    setDrawerOpen(false)
    setActivePage(null)
  }

  /** Runs a tool that takes over the screen; the drawer gets out of the way. */
  const runAndClose = (action: () => void) => () => {
    closeDrawer()
    action()
  }

  const statusItems: WorkspaceToolItem[] = [
    {
      id: 'runtime',
      icon: <Activity className="h-4 w-4" />,
      label: 'Runtime health',
      detail: 'Browser sessions and workflow processes',
      opensPage: true,
      onClick: () => setActivePage('runtime'),
    },
    {
      id: 'connections',
      icon: <Plug className="h-4 w-4" />,
      label: 'Connections',
      detail:
        connectionsSummary.total === 0
          ? 'Gmail, Slack, GitHub, and more'
          : `${connectionsSummary.connected} connected${
              needsAttention ? ` · ${connectionsSummary.needs_attention} need attention` : ''
            }`,
      status: needsAttention ? (
        <span className="h-2 w-2 shrink-0 rounded-full bg-amber-500" aria-hidden="true" />
      ) : undefined,
      onClick: runAndClose(() => setConnectionsOpen(true)),
    },
  ]

  if (isElectron) {
    statusItems.push({
      id: 'notifications',
      icon: notifications.blocked || !notifications.enabled
        ? <BellOff className="h-4 w-4" />
        : <Bell className="h-4 w-4" />,
      label: 'Notifications',
      detail: notifications.description,
      // Toggling in place: the drawer stays open so the new state is visible.
      onClick: notifications.toggle,
    })
  }

  if (isMultiUserMode && user) {
    statusItems.push({
      id: 'account',
      icon: <LogOut className="h-4 w-4" />,
      label: 'Sign out',
      detail: `Signed in as ${user.username || user.email || 'user'}`,
      onClick: logout,
    })
  }

  const setupItems: WorkspaceToolItem[] = [
    {
      id: 'models',
      icon: <BrainCircuit className="h-4 w-4" />,
      label: 'Models',
      detail: `${llmCount} enabled`,
      onClick: runAndClose(() => setShowLLMModal(true)),
    },
    {
      id: 'skills',
      icon: <WandSparkles className="h-4 w-4" />,
      label: 'Skills',
      detail: 'Installed capabilities',
      opensPage: true,
      onClick: () => setActivePage('skills'),
    },
    {
      id: 'secrets',
      icon: <KeyRound className="h-4 w-4" />,
      label: 'Secrets',
      detail: 'Keys and credentials',
      opensPage: true,
      onClick: () => setActivePage('secrets'),
    },
  ]

  const allSections: WorkspaceToolSection[] = [
    { id: 'status', title: 'Status', items: statusItems },
    { id: 'setup', title: 'Setup', items: setupItems },
    ...sections.map(section => ({
      ...section,
      items: section.items.map(item => ({
        ...item,
        // Automation tools open their own modals, so the drawer steps aside.
        onClick: runAndClose(item.onClick),
      })),
    })),
  ]

  const page: WorkspaceToolPage | null = activePage
    ? { ...PAGE_CONFIG[activePage], onBack: () => setActivePage(null) }
    : null

  return (
    <TooltipProvider delayDuration={400}>
      {/* LlmModalHost renders the LLM modals once; the trigger lives in the drawer. */}
      <LlmModalHost />

      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={() => (drawerOpen ? closeDrawer() : setDrawerOpen(true))}
            data-tour="workspace-tools"
            data-testid="workspace-tools-button"
            aria-label="Workspace tools"
            aria-expanded={drawerOpen}
            className={`relative flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm font-medium transition-colors ${
              drawerOpen
                ? 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200'
                : 'text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200'
            }`}
          >
            <LayoutGrid className="h-4 w-4" />
            <span className="hidden sm:inline">Workspace Tools</span>
            {needsAttention && (
              <span
                className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-amber-500 ring-2 ring-white dark:ring-slate-800"
                aria-hidden="true"
              />
            )}
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">
          {needsAttention ? 'Workspace tools — a connection needs attention' : 'Workspace tools'}
        </TooltipContent>
      </Tooltip>

      <WorkspaceToolsDrawer
        open={drawerOpen}
        onClose={closeDrawer}
        sections={allSections}
        page={page}
        footer={
          !isElectron ? (
            <a
              href="https://github.com/manishiitg/coding-agent-loop/releases/latest"
              target="_blank"
              rel="noopener noreferrer"
              className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left transition-colors hover:bg-gray-100 dark:hover:bg-slate-700/70"
              onClick={closeDrawer}
            >
              <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-gray-100 text-blue-500 dark:bg-slate-700 dark:text-blue-300">
                <Download className="h-4 w-4" />
              </span>
              <span className="min-w-0">
                <span className="block text-sm font-medium leading-5 text-gray-900 dark:text-gray-100">
                  Download Mac App
                </span>
                <span className="block truncate text-xs text-gray-500 dark:text-gray-400">Latest release</span>
              </span>
            </a>
          ) : null
        }
      />

      {connectionsOpen && <ConnectionsModal onClose={() => setConnectionsOpen(false)} />}
    </TooltipProvider>
  )
}
