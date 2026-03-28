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

const SECTION_META: Record<ParsedTaskStatus, { label: string; text: string; panel: string; item: string; focusItem: string; dot: string; count: string }> = {
  pending: {
    label: 'Pending',
    text: 'text-foreground',
    panel: 'border-border',
    item: 'border-border bg-background',
    focusItem: 'border-indigo-300 dark:border-indigo-700 bg-muted/40',
    dot: 'bg-gray-400 dark:bg-gray-500',
    count: 'text-muted-foreground',
  },
  in_progress: {
    label: 'In Progress',
    text: 'text-foreground',
    panel: 'border-sky-200/60 dark:border-sky-900/50',
    item: 'border-border bg-background',
    focusItem: 'border-sky-300 dark:border-sky-700 bg-muted/80',
    dot: 'bg-sky-500 dark:bg-sky-400',
    count: 'text-sky-700 dark:text-sky-300',
  },
  completed: {
    label: 'Completed',
    text: 'text-foreground',
    panel: 'border-emerald-200/60 dark:border-emerald-900/50',
    item: 'border-border bg-background',
    focusItem: 'border-emerald-300 dark:border-emerald-700 bg-muted/80',
    dot: 'bg-emerald-500 dark:bg-emerald-400',
    count: 'text-emerald-700 dark:text-emerald-300',
  },
  removed: {
    label: 'Removed',
    text: 'text-foreground',
    panel: 'border-amber-200/60 dark:border-amber-900/50',
    item: 'border-border bg-background',
    focusItem: 'border-amber-300 dark:border-amber-700 bg-muted/80',
    dot: 'bg-amber-500 dark:bg-amber-400',
    count: 'text-amber-700 dark:text-amber-300',
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
    <div className="bg-background border border-border rounded-md p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <ListTodo className="w-4 h-4 text-muted-foreground flex-shrink-0" />
          <div className="min-w-0">
            <div className="text-xs font-semibold text-foreground">
              {getPhaseLabel(event.status_phase)}
            </div>
            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-muted-foreground">
              {showDelegationContext && event.todo_id && (
                <span>
                  Task <span className="font-mono text-foreground">{event.todo_id}</span>
                </span>
              )}
              {showDelegationContext && event.route_id && (
                <span>
                  Route <span className="font-mono text-foreground">{event.route_id}</span>
                </span>
              )}
              {event.step_title && (
                <span className="truncate">
                  Step <span className="text-foreground">{event.step_title}</span>
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0 pt-0.5">
          {event.timestamp && (
            <span className="text-[10px] text-muted-foreground">
              {formatTimestamp(event.timestamp)}
            </span>
          )}
          <button
            onClick={() => setIsExpanded(!isExpanded)}
            className="p-1 rounded-md hover:bg-muted text-muted-foreground transition-colors"
            title={isExpanded ? 'Collapse' : 'Expand'}
          >
            {isExpanded ? <Minus className="w-3.5 h-3.5" /> : <Plus className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>

      {isExpanded && (
        <div className="mt-3 pt-3 border-t border-border">
          {tasks.length > 0 ? (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px]">
                {groupedTasks.map((section, index) => (
                  <div key={section.status} className="inline-flex items-center gap-1.5">
                    <span className={`w-1.5 h-1.5 rounded-full ${section.dot}`} />
                    <span className="text-muted-foreground">{section.label}</span>
                    <span className={`font-semibold ${section.count}`}>{section.tasks.length}</span>
                    {index < groupedTasks.length - 1 && <span className="text-border ml-1">·</span>}
                  </div>
                ))}
              </div>

              <div className="space-y-2.5 max-h-[32rem] overflow-y-auto pr-1">
                {visibleTaskSections.map((section) => (
                  <div key={section.status} className={`border-l-2 pl-3 ${section.panel}`}>
                    <div className="mb-1.5 flex items-center justify-between gap-2">
                      <div className={`flex items-center gap-2 text-[11px] font-semibold ${section.text}`}>
                        <span className={`w-2 h-2 rounded-full ${section.dot}`} />
                        <span>{section.label}</span>
                      </div>
                      <span className={`text-[10px] font-medium ${section.count}`}>
                        {section.tasks.length}
                      </span>
                    </div>
                    <div className="space-y-1">
                      {section.tasks.map((task) => {
                        const isFocusTask = !!event.todo_id && event.todo_id === task.id
                        return (
                          <div
                            key={`${section.status}-${task.id}`}
                            className={`rounded border px-2 py-1.5 text-[11px] ${isFocusTask ? section.focusItem : section.item}`}
                          >
                            <div className="flex items-start gap-2 leading-relaxed">
                              <span className="font-mono text-[10px] font-semibold text-foreground/90 shrink-0 mt-0.5">
                                {task.id}
                              </span>
                              <span className="text-muted-foreground break-words">
                                {task.title}
                              </span>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                ))}
              </div>

              <details className="text-[11px]">
                <summary className="cursor-pointer text-muted-foreground hover:text-foreground transition-colors">
                  View raw tasks.md
                </summary>
                <div className="mt-2 bg-muted/20 border border-border rounded-md p-3 max-h-[24rem] overflow-y-auto">
                  <MarkdownRenderer content={event.tasks_content} className="text-xs text-foreground" />
                </div>
              </details>
            </div>
          ) : (
            <div className="bg-muted/20 border border-border rounded-md p-3 max-h-[32rem] overflow-y-auto">
              <MarkdownRenderer content={event.tasks_content} className="text-xs text-foreground" />
            </div>
          )}
        </div>
      )}
    </div>
  )
}
