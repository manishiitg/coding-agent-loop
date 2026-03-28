import React, { useMemo, useState } from 'react'
import type { TodoTaskStatusUpdateEvent } from '../../../generated/event-types'
import { Plus, Minus, ListTodo } from 'lucide-react'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'

interface TodoTaskStatusUpdateEventDisplayProps {
  event: TodoTaskStatusUpdateEvent
}

type ParsedTaskStatus = 'pending' | 'in_progress' | 'completed' | 'removed'

interface ParsedTask {
  id: string
  title: string
  status: ParsedTaskStatus
}

const SECTION_ORDER: ParsedTaskStatus[] = ['pending', 'in_progress', 'completed', 'removed']

const SECTION_META: Record<ParsedTaskStatus, { label: string; text: string; chip: string; panel: string; item: string; focusItem: string }> = {
  pending: {
    label: 'Pending',
    text: 'text-gray-800 dark:text-gray-100',
    chip: 'bg-gray-100 dark:bg-gray-700/80 text-gray-700 dark:text-gray-200 border-gray-200 dark:border-gray-600',
    panel: 'bg-gray-50 dark:bg-gray-900/40 border-gray-200 dark:border-gray-700/70',
    item: 'border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-950/30',
    focusItem: 'border-indigo-300 dark:border-indigo-700 bg-indigo-50 dark:bg-indigo-950/30',
  },
  in_progress: {
    label: 'In Progress',
    text: 'text-sky-700 dark:text-sky-300',
    chip: 'bg-sky-100/70 dark:bg-sky-950/30 text-sky-700 dark:text-sky-300 border-sky-200/80 dark:border-sky-900/70',
    panel: 'bg-sky-50/40 dark:bg-sky-950/20 border-sky-200/80 dark:border-sky-900/60',
    item: 'border-sky-100 dark:border-sky-900/50 bg-white dark:bg-sky-950/10',
    focusItem: 'border-sky-300 dark:border-sky-700 bg-sky-100/80 dark:bg-sky-900/30',
  },
  completed: {
    label: 'Completed',
    text: 'text-emerald-700 dark:text-emerald-300',
    chip: 'bg-emerald-100/70 dark:bg-emerald-950/30 text-emerald-700 dark:text-emerald-300 border-emerald-200/80 dark:border-emerald-900/70',
    panel: 'bg-emerald-50/40 dark:bg-emerald-950/20 border-emerald-200/80 dark:border-emerald-900/60',
    item: 'border-emerald-100 dark:border-emerald-900/50 bg-white dark:bg-emerald-950/10',
    focusItem: 'border-emerald-300 dark:border-emerald-700 bg-emerald-100/80 dark:bg-emerald-900/30',
  },
  removed: {
    label: 'Removed',
    text: 'text-amber-700 dark:text-amber-300',
    chip: 'bg-amber-100/70 dark:bg-amber-950/30 text-amber-700 dark:text-amber-300 border-amber-200/80 dark:border-amber-900/70',
    panel: 'bg-amber-50/40 dark:bg-amber-950/20 border-amber-200/80 dark:border-amber-900/60',
    item: 'border-amber-100 dark:border-amber-900/50 bg-white dark:bg-amber-950/10',
    focusItem: 'border-amber-300 dark:border-amber-700 bg-amber-100/80 dark:bg-amber-900/30',
  },
}

function parseTasksContent(tasksContent: string): ParsedTask[] {
  const tasks: ParsedTask[] = []

  for (const rawLine of tasksContent.split('\n')) {
    const line = rawLine.trim()
    if (!line.startsWith('- [')) {
      continue
    }

    let status: ParsedTaskStatus | null = null
    let remainder = ''

    if (line.startsWith('- [ ] ')) {
      status = 'pending'
      remainder = line.slice(6)
    } else if (line.startsWith('- [~] ')) {
      status = 'in_progress'
      remainder = line.slice(6)
    } else if (line.startsWith('- [x] ')) {
      status = 'completed'
      remainder = line.slice(6)
    } else if (line.startsWith('- [REMOVED] ')) {
      status = 'removed'
      remainder = line.slice(12)
    }

    if (!status) {
      continue
    }

    const colonIndex = remainder.indexOf(':')
    const id = (colonIndex >= 0 ? remainder.slice(0, colonIndex) : remainder).trim()
    const title = (colonIndex >= 0 ? remainder.slice(colonIndex + 1) : remainder).trim()

    if (!id) {
      continue
    }

    tasks.push({
      id,
      title: title || id,
      status,
    })
  }

  return tasks
}

function getPhaseLabel(phase?: string): string {
  switch (phase) {
    case 'before_delegation':
      return 'Todo Snapshot Before Dispatch'
    case 'after_delegation':
      return 'Todo Snapshot After Return'
    default:
      return 'Todo Snapshot'
  }
}

export const TodoTaskStatusUpdateEventDisplay: React.FC<TodoTaskStatusUpdateEventDisplayProps> = ({ event }) => {
  const [isExpanded, setIsExpanded] = useState(true)
  const tasks = useMemo(() => parseTasksContent(event.tasks_content || ''), [event.tasks_content])
  const hasMatchingTodoId = useMemo(
    () => !!event.todo_id && tasks.some((task) => task.id === event.todo_id),
    [event.todo_id, tasks]
  )
  const showDelegationContext = tasks.length === 0 || hasMatchingTodoId
  const groupedTasks = useMemo(() => {
    return SECTION_ORDER.map((status) => ({
      status,
      ...SECTION_META[status],
      tasks: tasks.filter((task) => task.status === status),
    }))
  }, [tasks])
  const visibleTaskSections = useMemo(
    () => groupedTasks.filter((section) => section.tasks.length > 0),
    [groupedTasks]
  )

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return ''
    return new Date(timestamp).toLocaleTimeString()
  }

  if (!event.tasks_content) return null

  return (
    <div className="bg-white dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700 rounded p-2 shadow-sm">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <ListTodo className="w-4 h-4 text-indigo-600 dark:text-indigo-300 flex-shrink-0" />
          <div className="min-w-0">
            <div className="text-xs font-semibold text-gray-900 dark:text-gray-100">
              {getPhaseLabel(event.status_phase)}
            </div>
            <div className="text-[10px] text-gray-500 dark:text-gray-400 flex flex-wrap items-center gap-x-2 gap-y-1">
              {showDelegationContext && event.todo_id && (
                <span>
                  Task: <span className="font-mono">{event.todo_id}</span>
                </span>
              )}
              {showDelegationContext && event.route_id && (
                <span>
                  Route: <span className="font-mono">{event.route_id}</span>
                </span>
              )}
              {event.step_title && (
                <span className="truncate">
                  Step: {event.step_title}
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <span className="text-[10px] text-gray-400 dark:text-gray-500">
              {formatTimestamp(event.timestamp)}
            </span>
          )}
          <button
            onClick={() => setIsExpanded(!isExpanded)}
            className="p-0.5 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400 transition-colors"
            title={isExpanded ? 'Collapse' : 'Expand'}
          >
            {isExpanded ? <Minus className="w-3.5 h-3.5" /> : <Plus className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>

      {isExpanded && (
        <div className="mt-2 pt-2 border-t border-gray-200 dark:border-gray-700">
          {tasks.length > 0 ? (
            <div className="space-y-2">
              <div className="flex flex-wrap gap-1.5">
                {groupedTasks.map((section) => (
                  <span
                    key={section.status}
                    className={`text-[10px] px-2 py-0.5 rounded-full border ${section.chip}`}
                  >
                    {section.label}: {section.tasks.length}
                  </span>
                ))}
              </div>

              <div className="space-y-2 max-h-[32rem] overflow-y-auto pr-1">
                {visibleTaskSections.map((section) => (
                  <div key={section.status} className={`border rounded p-2 ${section.panel}`}>
                    <div className={`text-[11px] font-semibold mb-1 ${section.text}`}>
                      {section.label}
                    </div>
                    <div className="space-y-1">
                      {section.tasks.map((task) => {
                        const isFocusTask = !!event.todo_id && event.todo_id === task.id
                        return (
                          <div
                              key={`${section.status}-${task.id}`}
                              className={`rounded border px-2 py-1 text-[11px] ${isFocusTask ? section.focusItem : section.item}`}
                            >
                              <div className="font-mono text-gray-800 dark:text-gray-100">{task.id}</div>
                              <div className="text-gray-600 dark:text-gray-400 whitespace-pre-wrap break-words">
                                {task.title}
                              </div>
                            </div>
                          )
                      })}
                    </div>
                  </div>
                ))}
              </div>

              <details className="text-[11px]">
                <summary className="cursor-pointer text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200">
                  View raw tasks.md
                </summary>
                <div className="mt-2 bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded p-2 max-h-[24rem] overflow-y-auto">
                  <MarkdownRenderer content={event.tasks_content} className="text-xs text-gray-800 dark:text-gray-200" />
                </div>
              </details>
            </div>
          ) : (
            <div className="bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700 rounded p-2 max-h-[32rem] overflow-y-auto">
              <MarkdownRenderer content={event.tasks_content} className="text-xs text-gray-800 dark:text-gray-200" />
            </div>
          )}
        </div>
      )}
    </div>
  )
}
