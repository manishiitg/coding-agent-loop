import type { CSSProperties } from 'react'

export interface WorkSurfaceLayoutInput {
  chatOpen: boolean
  panelOpen: boolean
  splitRatio: number
}

export interface WorkSurfaceLayout {
  gridClassName: string
  gridStyle: CSSProperties | undefined
  toolbarClassName: string
  showChat: boolean
  chatClassName: string
  showDivider: boolean
  showPanel: boolean
  panelClassName: string
}

/**
 * The single layout decision point for the Crew split. Every class and mount
 * branch for the chat/panel panes derives here from the same flags, so no
 * call site can combine them inconsistently.
 *
 * Visibility rule (shared with the Builder resolver): a pane hidden below md
 * must restore its own display at md+ (`hidden md:flex` for flex panes).
 * Restoring with `md:block` would override flex, collapse flex-1 scroll
 * regions to content height, and freeze pane scrolling.
 */
export function resolveWorkSurfaceLayout(input: WorkSurfaceLayoutInput): WorkSurfaceLayout {
  const { chatOpen, panelOpen, splitRatio } = input
  const split = chatOpen && panelOpen

  return {
    gridClassName: `grid h-full min-h-0 min-w-0 grid-cols-1 grid-rows-[auto_minmax(0,1fr)] ${split ? 'md:[grid-template-columns:var(--work-split-columns)]' : ''}`,
    gridStyle: split
      ? ({ '--work-split-columns': `minmax(240px, ${splitRatio}fr) minmax(240px, ${1 - splitRatio}fr)` } as CSSProperties)
      : undefined,
    toolbarClassName: `${split ? 'md:col-span-2' : ''} col-start-1 row-start-1`,
    showChat: chatOpen,
    chatClassName: `flex min-h-0 min-w-0 flex-col overflow-hidden bg-background col-start-1 row-start-2 ${panelOpen ? 'border-b border-border md:border-b-0 md:border-r' : ''}`,
    showDivider: split,
    showPanel: panelOpen,
    panelClassName: `min-h-0 min-w-0 overflow-hidden bg-background row-start-2 ${chatOpen ? 'md:col-start-2' : 'col-start-1'}`,
  }
}
