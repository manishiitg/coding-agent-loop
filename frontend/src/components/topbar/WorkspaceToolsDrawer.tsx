import { useEffect, type ReactNode } from 'react'
import { ChevronLeft, ChevronRight, X } from 'lucide-react'
import ModalPortal from '../ui/ModalPortal'

export interface WorkspaceToolItem {
  id: string
  icon: ReactNode
  label: string
  detail?: string
  /** Right-aligned state: a count, a pill, a dot. */
  status?: ReactNode
  /** Set when the row opens a page inside the drawer rather than acting. */
  opensPage?: boolean
  onClick: () => void
  dataTour?: string
  dataTestid?: string
}

export interface WorkspaceToolSection {
  id: string
  title: string
  items: WorkspaceToolItem[]
}

/** A page pushed on top of the root list, with a back affordance. */
export interface WorkspaceToolPage {
  title: string
  icon: ReactNode
  content: ReactNode
  onBack: () => void
}

function ToolRow({ item }: { item: WorkspaceToolItem }) {
  return (
    <button
      type="button"
      onClick={item.onClick}
      data-tour={item.dataTour}
      data-testid={item.dataTestid}
      className="flex w-full items-center gap-3 rounded-md px-2 py-2 text-left transition-colors hover:bg-gray-100 dark:hover:bg-slate-700/70"
    >
      <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-500 dark:bg-slate-700 dark:text-gray-300">
        {item.icon}
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium leading-5 text-gray-900 dark:text-gray-100">
          {item.label}
        </span>
        {item.detail && (
          <span className="block truncate text-xs text-gray-500 dark:text-gray-400">{item.detail}</span>
        )}
      </span>
      {item.status}
      {item.opensPage && (
        <ChevronRight className="h-4 w-4 shrink-0 text-gray-400 dark:text-gray-500" aria-hidden="true" />
      )}
    </button>
  )
}

interface WorkspaceToolsDrawerProps {
  open: boolean
  onClose: () => void
  sections: WorkspaceToolSection[]
  /** When set, replaces the root list until the user goes back. */
  page?: WorkspaceToolPage | null
  /** Rendered under the last section — secondary links, not tools. */
  footer?: ReactNode
}

/**
 * WorkspaceToolsDrawer - the right-hand slide-over that holds everything the
 * top bar used to scatter across a row of unlabelled icons. It overlays the
 * content rather than resizing it, so opening a tool never reflows the chat or
 * automation view underneath.
 */
export default function WorkspaceToolsDrawer({
  open,
  onClose,
  sections,
  page = null,
  footer,
}: WorkspaceToolsDrawerProps) {
  useEffect(() => {
    if (!open) return
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      // Escape unwinds one level at a time: page first, then the drawer.
      if (page) page.onBack()
      else onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [open, onClose, page])

  const visibleSections = sections.filter(section => section.items.length > 0)

  return (
    <ModalPortal>
      {open && (
        <div
          className="fixed inset-0 z-[49]"
          onClick={() => (page ? page.onBack() : onClose())}
          aria-hidden="true"
        />
      )}
      <aside
        aria-label="Workspace tools"
        aria-hidden={!open}
        className={`fixed right-0 top-0 z-[50] flex h-full w-80 max-w-[calc(100vw-2rem)] transform flex-col border-l border-gray-200 bg-white shadow-2xl transition-transform duration-200 ease-out dark:border-slate-700 dark:bg-slate-800 ${
          open ? 'translate-x-0' : 'pointer-events-none translate-x-full'
        }`}
      >
        <div className="flex items-center justify-between gap-2 border-b border-gray-200 px-3 py-3 dark:border-slate-700">
          {page ? (
            <button
              type="button"
              onClick={page.onBack}
              className="flex min-w-0 items-center gap-2 rounded-md px-1.5 py-1 text-sm font-semibold text-gray-900 hover:bg-gray-100 dark:text-gray-100 dark:hover:bg-slate-700"
            >
              <ChevronLeft className="h-4 w-4 shrink-0" />
              <span className="flex min-w-0 items-center gap-2">
                {page.icon}
                <span className="truncate">{page.title}</span>
              </span>
            </button>
          ) : (
            <h2 className="px-1.5 text-sm font-semibold text-gray-900 dark:text-gray-100">Workspace Tools</h2>
          )}
          <button
            type="button"
            onClick={onClose}
            aria-label="Close workspace tools"
            className="rounded-md p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-slate-700 dark:hover:text-gray-200"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">
          {!open ? null : page ? (
            <div className="p-3">{page.content}</div>
          ) : (
            <div className="space-y-4 p-2">
              {visibleSections.map(section => (
                <section key={section.id}>
                  <div className="px-2 pb-1 text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">
                    {section.title}
                  </div>
                  <div className="space-y-0.5">
                    {section.items.map(item => (
                      <ToolRow key={item.id} item={item} />
                    ))}
                  </div>
                </section>
              ))}
              {footer && <div className="border-t border-gray-200 pt-2 dark:border-slate-700">{footer}</div>}
            </div>
          )}
        </div>
      </aside>
    </ModalPortal>
  )
}
