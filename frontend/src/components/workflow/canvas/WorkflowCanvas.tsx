import React, { useCallback, useRef, useImperativeHandle, forwardRef, useEffect } from 'react'
import {
  ReactFlow,
  Background,
  useNodesState,
  useEdgesState,
  useReactFlow,
  BackgroundVariant,
  ReactFlowProvider,
  type NodeChange,
  type OnNodeDrag
} from '@xyflow/react'
import { Braces, Download, FileText, GitBranch, Laptop, Loader2, RefreshCw, Route, Settings, Smartphone, TabletSmartphone, X } from 'lucide-react'
import '@xyflow/react/dist/style.css'

import { useModeStore } from '../../../stores/useModeStore'
import { nodeTypes } from '../nodes'
import { WorkflowToolbar } from './WorkflowToolbar'
import { VariablesSidebar } from './VariablesSidebar'
import { StepLegend } from './StepLegend'
import { BatchProgressHeader } from '../BatchProgressHeader'
import {
  REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT,
  REPORT_PREVIEW_PREFERENCE_KEY,
  ReportView,
} from '../ReportViewer'
import { usePlanData, type PlanChanges } from '../hooks/usePlanData'
import { useEvaluationPlanData } from '../hooks/useEvaluationPlanData'
import { usePlanToFlow, type WorkflowNode, type WorkflowEdge, type WorkflowNodeData, type StepNodeData, type ConditionalNodeData, type EvaluationStepNodeData } from '../hooks/usePlanToFlow'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useWorkflowExecution } from '../hooks/useWorkflowExecution'
import { useWorkspaceState } from '../hooks/useWorkspaceState'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useChatStore } from '../../../stores/useChatStore'
import { agentApi } from '../../../services/api'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import type { VariablesManifest } from '../../../services/api-types'
import { buildGroupFolderPath } from '../../../utils/workflowUtils'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'

// Duration to show highlights before clearing (in ms)
const HIGHLIGHT_DURATION = 4000
const SVG_EXPORT_SCALE = 3
const PNG_EXPORT_SCALE = 1
const PNG_EXPORT_MAX_SIDE = 16000
const PNG_EXPORT_MAX_PIXELS = 64_000_000

import type { ExecutionOptions } from '../../../services/api-types'

type WorkflowPreviewMode = 'desktop' | 'tablet' | 'mobile'
type WorkflowImageExportFormat = 'svg' | 'png' | 'jpeg'

function isHorizontalWorkflowLayout(direction: 'LR' | 'TB'): boolean {
  return direction === 'LR'
}

interface WorkflowCanvasProps {
  workspacePath: string | null
  presetQueryId: string | null
  currentPhase?: string
  onStartPhase?: (phaseId: string, executionOptions?: ExecutionOptions) => void
  onCreatePlan?: () => void
  showChatArea?: boolean
  onToggleChatArea?: () => void
  toolbarOnly?: boolean  // When true, only render the toolbar (skip React Flow canvas for performance)
  sharedToolbar?: boolean
  paneClassName?: string
  className?: string
}

function FloatingWorkflowViewControls({
  viewLabel,
  showPreviewControls,
  onRefresh,
  onExportImage,
  isExportingImage,
}: {
  viewLabel: string
  showPreviewControls: boolean
  onRefresh: () => void
  onExportImage?: (format: WorkflowImageExportFormat) => void
  isExportingImage?: boolean
}) {
  const [previewPreference, setPreviewPreference] = React.useState<'auto' | WorkflowPreviewMode>(() => {
    try {
      const saved = localStorage.getItem(REPORT_PREVIEW_PREFERENCE_KEY)
      return saved === 'desktop' || saved === 'tablet' || saved === 'mobile' ? saved : 'auto'
    } catch {
      return 'auto'
    }
  })
  const [previewControlsExpanded, setPreviewControlsExpanded] = React.useState(false)
  const [exportControlsExpanded, setExportControlsExpanded] = React.useState(false)

  const previewMode: WorkflowPreviewMode = previewPreference === 'auto' ? 'desktop' : previewPreference

  const setPreviewMode = (mode: WorkflowPreviewMode) => {
    setPreviewPreference(() => {
      try {
        localStorage.setItem(REPORT_PREVIEW_PREFERENCE_KEY, mode)
        window.dispatchEvent(new CustomEvent(REPORT_PREVIEW_PREFERENCE_CHANGED_EVENT, { detail: { preference: mode } }))
      } catch {
        // ignore
      }
      return mode
    })
  }

  return (
    <>
      {onExportImage && (
        <div
          role="group"
          aria-label="Export flow image"
          onMouseEnter={() => setExportControlsExpanded(true)}
          onMouseLeave={() => setExportControlsExpanded(false)}
          onFocus={() => setExportControlsExpanded(true)}
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node)) {
              setExportControlsExpanded(false)
            }
          }}
          className="absolute right-3 top-3 z-20 inline-flex items-center rounded-full border border-border/70 bg-background/95 p-0.5 shadow-lg backdrop-blur-sm focus-within:ring-1 focus-within:ring-ring sm:right-4 sm:top-4"
        >
          <button
            onClick={() => onExportImage('svg')}
            disabled={isExportingImage}
            className="inline-flex h-6 w-6 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60"
            title="Export flow as SVG"
            aria-label="Export flow as SVG"
          >
            {isExportingImage ? <Loader2 className="h-3 w-3 animate-spin" /> : <Download className="h-3 w-3" />}
          </button>
          <button
            onClick={() => onExportImage('svg')}
            disabled={isExportingImage}
            className="inline-flex h-6 items-center justify-center overflow-hidden rounded-full px-0 text-[9px] font-semibold text-muted-foreground transition-[width,opacity,background-color,color] duration-150 ease-out hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60"
            style={{
              width: exportControlsExpanded ? 30 : 0,
              opacity: exportControlsExpanded ? 1 : 0,
              pointerEvents: exportControlsExpanded ? 'auto' : 'none',
            }}
            title="Export flow as SVG"
            aria-label="Export flow as SVG"
          >
            SVG
          </button>
          <button
            onClick={() => onExportImage('png')}
            disabled={isExportingImage}
            className="inline-flex h-6 items-center justify-center overflow-hidden rounded-full px-0 text-[9px] font-semibold text-muted-foreground transition-[width,opacity,background-color,color] duration-150 ease-out hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-60"
            style={{
              width: exportControlsExpanded ? 34 : 0,
              opacity: exportControlsExpanded ? 1 : 0,
              pointerEvents: exportControlsExpanded ? 'auto' : 'none',
            }}
            title="Export flow as PNG"
            aria-label="Export flow as PNG"
          >
            PNG
          </button>
        </div>
      )}
    <div className="absolute bottom-4 right-4 z-20 flex flex-col items-end gap-2 sm:bottom-5 sm:right-5">
      {showPreviewControls && (
        <div
          role="group"
          aria-label={`${viewLabel} preview width`}
          onMouseEnter={() => setPreviewControlsExpanded(true)}
          onMouseLeave={() => setPreviewControlsExpanded(false)}
          onFocus={() => setPreviewControlsExpanded(true)}
          onBlur={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node)) {
              setPreviewControlsExpanded(false)
            }
          }}
          className="inline-flex items-center rounded-full border border-border/70 bg-background/95 p-0.5 shadow-lg backdrop-blur-sm focus-within:ring-1 focus-within:ring-ring"
        >
          {([
            { mode: 'mobile', Icon: Smartphone, label: 'Mobile preview (≈480px)' },
            { mode: 'tablet', Icon: TabletSmartphone, label: 'Tablet preview (50/50 split)' },
            { mode: 'desktop', Icon: Laptop, label: 'Laptop preview (full width)' },
          ] as const).map(({ mode, Icon, label }) => {
            const active = previewMode === mode
            const visible = active || previewControlsExpanded
            return (
              <button
                key={mode}
                onClick={() => setPreviewMode(mode)}
                className={`inline-flex h-8 items-center justify-center overflow-hidden rounded-full transition-[width,opacity] duration-150 ease-out ${
                  active
                    ? 'bg-muted text-foreground'
                    : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                }`}
                style={{
                  width: visible ? 32 : 0,
                  opacity: visible ? 1 : 0,
                  pointerEvents: visible ? 'auto' : 'none',
                }}
                title={label}
                aria-label={label}
                aria-pressed={active}
              >
                <Icon className="h-3.5 w-3.5 flex-shrink-0" />
              </button>
            )
          })}
        </div>
      )}
      <button
        onClick={onRefresh}
        className="inline-flex h-9 w-9 items-center justify-center rounded-full border border-border/70 bg-background/95 text-muted-foreground shadow-lg backdrop-blur-sm transition-all hover:-translate-y-0.5 hover:bg-muted hover:text-foreground"
        title={`Refresh ${viewLabel}`}
        aria-label={`Refresh ${viewLabel}`}
      >
        <RefreshCw className="h-3.5 w-3.5" />
      </button>
    </div>
    </>
  )
}

function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function waitForAnimationFrames(count = 2): Promise<void> {
  return new Promise(resolve => {
    const step = (remaining: number) => {
      if (remaining <= 0) {
        resolve()
        return
      }
      window.requestAnimationFrame(() => step(remaining - 1))
    }
    step(count)
  })
}

function triggerImageDownload(dataUrl: string, filename: string): void {
  const link = document.createElement('a')
  link.href = dataUrl
  link.download = filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

function dataUrlPayload(dataUrl: string): string {
  const commaIndex = dataUrl.indexOf(',')
  return commaIndex >= 0 ? dataUrl.slice(commaIndex + 1) : dataUrl
}

function utf8ToBase64(value: string): string {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  const chunkSize = 8192
  for (let index = 0; index < bytes.length; index += chunkSize) {
    const chunk = bytes.slice(index, index + chunkSize)
    binary += String.fromCharCode(...chunk)
  }
  return btoa(binary)
}

async function saveWorkflowImage(dataUrl: string, filename: string, format: WorkflowImageExportFormat): Promise<{ canceled?: boolean; filePath?: string } | null> {
  const electronAPI = (window as unknown as {
    electronAPI?: {
      saveFlowImage?: (payload: { filename: string; dataUrl: string; format: WorkflowImageExportFormat }) => Promise<{ canceled?: boolean; filePath?: string }>
    }
  }).electronAPI

  if (electronAPI?.saveFlowImage) {
    const payload = dataUrlPayload(dataUrl)
    if (format === 'png' && !payload.startsWith('iVBOR')) {
      throw new Error('PNG export payload was invalid. Reload the Electron window and try again.')
    }
    return electronAPI.saveFlowImage({
      filename,
      dataUrl: payload,
      format,
    })
  }

  triggerImageDownload(dataUrl, filename)
  return null
}

async function captureWorkflowImage(filename: string, format: WorkflowImageExportFormat, rect: DOMRect): Promise<{ canceled?: boolean; filePath?: string } | null> {
  const electronAPI = (window as unknown as {
    electronAPI?: {
      captureFlowImage?: (payload: {
        filename: string
        format: WorkflowImageExportFormat
        rect: { x: number; y: number; width: number; height: number }
      }) => Promise<{ canceled?: boolean; filePath?: string }>
    }
  }).electronAPI

  if (electronAPI && !electronAPI.captureFlowImage) {
    throw new Error('Quit and reopen Electron to load the updated flow export capture support.')
  }
  if (!electronAPI?.captureFlowImage) return null
  try {
    return await electronAPI.captureFlowImage({
      filename,
      format,
      rect: {
        x: rect.x,
        y: rect.y,
        width: rect.width,
        height: rect.height,
      },
    })
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    if (message.includes('No handler registered') || message.includes('capture-flow-image')) {
      throw new Error('Quit and reopen Electron to load the updated flow export capture support.')
    }
    throw error
  }
}

function inlineComputedStyles(source: Element, target: Element): void {
  if (target instanceof HTMLElement || target instanceof SVGElement) {
    const computed = window.getComputedStyle(source)
    for (const property of Array.from(computed)) {
      target.style.setProperty(property, computed.getPropertyValue(property), computed.getPropertyPriority(property))
    }
  }

  const sourceChildren = Array.from(source.children)
  const targetChildren = Array.from(target.children)
  sourceChildren.forEach((sourceChild, index) => {
    const targetChild = targetChildren[index]
    if (targetChild) inlineComputedStyles(sourceChild, targetChild)
  })
}

function renderFlowElementToSvg(flowElement: HTMLElement): string {
  const rect = flowElement.getBoundingClientRect()
  const width = Math.max(1, Math.ceil(rect.width))
  const height = Math.max(1, Math.ceil(rect.height))
  const exportWidth = width * SVG_EXPORT_SCALE
  const exportHeight = height * SVG_EXPORT_SCALE
  const clone = flowElement.cloneNode(true) as HTMLElement
  inlineComputedStyles(flowElement, clone)
  clone.setAttribute('xmlns', 'http://www.w3.org/1999/xhtml')
  clone.style.width = `${width}px`
  clone.style.height = `${height}px`
  clone.style.margin = '0'
  clone.style.position = 'relative'
  clone.querySelectorAll('[data-flow-exporting]').forEach(element => element.removeAttribute('data-flow-exporting'))

  const html = new XMLSerializer().serializeToString(clone)
  const svg = [
    `<svg xmlns="http://www.w3.org/2000/svg" width="${exportWidth}" height="${exportHeight}" viewBox="0 0 ${width} ${height}">`,
    `<foreignObject width="100%" height="100%">${html}</foreignObject>`,
    '</svg>',
  ].join('')
  return `data:image/svg+xml;base64,${utf8ToBase64(svg)}`
}

function svgDataUrlToPngDataUrl(svgDataUrl: string, scale = PNG_EXPORT_SCALE): Promise<string> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => {
      const sourceWidth = Math.max(1, image.naturalWidth || image.width)
      const sourceHeight = Math.max(1, image.naturalHeight || image.height)
      const maxScale = Math.min(
        scale,
        PNG_EXPORT_MAX_SIDE / sourceWidth,
        PNG_EXPORT_MAX_SIDE / sourceHeight,
        Math.sqrt(PNG_EXPORT_MAX_PIXELS / (sourceWidth * sourceHeight))
      )
      const safeScale = Math.max(0.1, maxScale)
      const canvas = document.createElement('canvas')
      canvas.width = Math.ceil(sourceWidth * safeScale)
      canvas.height = Math.ceil(sourceHeight * safeScale)
      const context = canvas.getContext('2d')
      if (!context) {
        reject(new Error('Could not create PNG export canvas'))
        return
      }
      context.scale(safeScale, safeScale)
      context.drawImage(image, 0, 0, sourceWidth, sourceHeight)
      const dataUrl = canvas.toDataURL('image/png')
      if (!dataUrl.startsWith('data:image/png;base64,')) {
        reject(new Error('Failed to create a valid PNG export'))
        return
      }
      resolve(dataUrl)
    }
    image.onerror = () => reject(new Error('Failed to render SVG export as PNG'))
    image.src = svgDataUrl
  })
}

function roundedRectPath(ctx: CanvasRenderingContext2D, x: number, y: number, width: number, height: number, radius: number): void {
  const safeRadius = Math.min(radius, width / 2, height / 2)
  ctx.beginPath()
  ctx.moveTo(x + safeRadius, y)
  ctx.lineTo(x + width - safeRadius, y)
  ctx.quadraticCurveTo(x + width, y, x + width, y + safeRadius)
  ctx.lineTo(x + width, y + height - safeRadius)
  ctx.quadraticCurveTo(x + width, y + height, x + width - safeRadius, y + height)
  ctx.lineTo(x + safeRadius, y + height)
  ctx.quadraticCurveTo(x, y + height, x, y + height - safeRadius)
  ctx.lineTo(x, y + safeRadius)
  ctx.quadraticCurveTo(x, y, x + safeRadius, y)
  ctx.closePath()
}

function wrapCanvasText(ctx: CanvasRenderingContext2D, text: string, maxWidth: number, maxLines: number): string[] {
  const words = text.replace(/\s+/g, ' ').trim().split(' ').filter(Boolean)
  const lines: string[] = []
  let current = ''

  for (const word of words) {
    const next = current ? `${current} ${word}` : word
    if (ctx.measureText(next).width <= maxWidth) {
      current = next
      continue
    }
    if (current) lines.push(current)
    current = word
    if (lines.length >= maxLines) break
  }
  if (current && lines.length < maxLines) lines.push(current)
  if (lines.length === maxLines && words.length > 0) {
    const last = lines[maxLines - 1]
    if (ctx.measureText(last).width > maxWidth || words.join(' ') !== lines.join(' ')) {
      let truncated = last
      while (truncated.length > 0 && ctx.measureText(`${truncated}...`).width > maxWidth) {
        truncated = truncated.slice(0, -1)
      }
      lines[maxLines - 1] = `${truncated.trim()}...`
    }
  }
  return lines.length > 0 ? lines : ['Workflow step']
}

function getNodeRenderRect(node: WorkflowNode, nodeMap: Map<string, WorkflowNode>): { x: number; y: number; width: number; height: number } {
  let x = node.position?.x || 0
  let y = node.position?.y || 0
  const parentId = (node as { parentId?: string; parentNode?: string }).parentId || (node as { parentNode?: string }).parentNode
  if (parentId) {
    const parent = nodeMap.get(parentId)
    if (parent) {
      const parentRect = getNodeRenderRect(parent, nodeMap)
      x += parentRect.x
      y += parentRect.y
    }
  }

  const measured = (node as { measured?: { width?: number; height?: number }; width?: number; height?: number }).measured
  const width = measured?.width || (node as { width?: number }).width || (node.type === 'start' || node.type === 'end' ? 96 : 280)
  const height = measured?.height || (node as { height?: number }).height || (node.type === 'start' || node.type === 'end' ? 40 : 104)
  return { x, y, width, height }
}

function drawArrow(ctx: CanvasRenderingContext2D, fromX: number, fromY: number, toX: number, toY: number): void {
  const angle = Math.atan2(toY - fromY, toX - fromX)
  const size = 10
  ctx.beginPath()
  ctx.moveTo(toX, toY)
  ctx.lineTo(toX - size * Math.cos(angle - Math.PI / 6), toY - size * Math.sin(angle - Math.PI / 6))
  ctx.lineTo(toX - size * Math.cos(angle + Math.PI / 6), toY - size * Math.sin(angle + Math.PI / 6))
  ctx.closePath()
  ctx.fill()
}

function drawWorkflowNode(ctx: CanvasRenderingContext2D, node: WorkflowNode, rect: { x: number; y: number; width: number; height: number }, offsetX: number, offsetY: number): void {
  const x = rect.x + offsetX
  const y = rect.y + offsetY
  const width = rect.width
  const height = rect.height
  const data = node.data as WorkflowNodeData
  const title = typeof data.title === 'string' && data.title ? data.title : node.id
  const status = typeof data.status === 'string' ? data.status : 'pending'
  const isStart = node.type === 'start'
  const isEnd = node.type === 'end'
  const isSpecial = isStart || isEnd
  const borderColor =
    status === 'completed' ? '#22c55e' :
    status === 'failed' ? '#ef4444' :
    status === 'running' ? '#3b82f6' :
    isStart ? '#10b981' :
    '#64748b'

  ctx.save()
  ctx.shadowColor = 'rgba(15, 23, 42, 0.14)'
  ctx.shadowBlur = 14
  ctx.shadowOffsetY = 5
  roundedRectPath(ctx, x, y, width, height, isSpecial ? 20 : 10)
  ctx.fillStyle = isSpecial ? (isStart ? '#dcfce7' : '#f1f5f9') : '#ffffff'
  ctx.fill()
  ctx.shadowColor = 'transparent'
  ctx.lineWidth = 2
  ctx.strokeStyle = borderColor
  ctx.stroke()

  if (!isSpecial) {
    roundedRectPath(ctx, x, y, width, Math.min(58, height), 10)
    ctx.clip()
    ctx.fillStyle = '#f8fafc'
    ctx.fillRect(x, y, width, Math.min(58, height))
    ctx.restore()
    ctx.save()
  }

  ctx.fillStyle = isStart ? '#047857' : isEnd ? '#475569' : '#0f172a'
  ctx.font = isSpecial ? '600 16px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif' : '600 14px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
  ctx.textBaseline = 'top'

  if (isSpecial) {
    ctx.textAlign = 'center'
    ctx.fillText(isStart ? 'Start' : 'End', x + width / 2, y + 11)
  } else {
    const stepIndex = typeof (data as StepNodeData).stepIndex === 'number' ? (data as StepNodeData).stepIndex : null
    ctx.fillStyle = '#dbeafe'
    roundedRectPath(ctx, x + 14, y + 14, 32, 32, 7)
    ctx.fill()
    ctx.fillStyle = '#1d4ed8'
    ctx.font = '700 14px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    ctx.textAlign = 'center'
    ctx.fillText(stepIndex === null ? '#' : String(stepIndex + 1), x + 30, y + 22)

    ctx.textAlign = 'left'
    ctx.fillStyle = '#0f172a'
    ctx.font = '600 14px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    const lines = wrapCanvasText(ctx, title, width - 66, 2)
    lines.forEach((line, index) => ctx.fillText(line, x + 56, y + 14 + index * 18))

    ctx.fillStyle = '#64748b'
    ctx.font = '500 11px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    ctx.fillText(stepIndex === null ? node.type || 'node' : `Step ${stepIndex + 1}`, x + 16, y + height - 24)

    if (status !== 'pending') {
      ctx.fillStyle = borderColor
      ctx.font = '700 11px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
      ctx.textAlign = 'right'
      ctx.fillText(status.toUpperCase(), x + width - 16, y + height - 24)
    }
  }
  ctx.restore()
}

function escapeSvgText(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function wrapSvgText(text: string, maxChars: number, maxLines: number): string[] {
  const words = text.replace(/\s+/g, ' ').trim().split(' ').filter(Boolean)
  const lines: string[] = []
  let current = ''
  words.forEach(word => {
    const next = current ? `${current} ${word}` : word
    if (next.length <= maxChars) {
      current = next
      return
    }
    if (current) lines.push(current)
    current = word
  })
  if (current) lines.push(current)
  const limited = lines.slice(0, maxLines)
  if (lines.length > maxLines && limited.length > 0) {
    limited[limited.length - 1] = `${limited[limited.length - 1].slice(0, Math.max(0, maxChars - 3)).trim()}...`
  }
  return limited.length > 0 ? limited : ['Workflow step']
}

function renderFlowToSvg(nodes: WorkflowNode[], edges: WorkflowEdge[]): string {
  if (nodes.length === 0) throw new Error('No workflow nodes to export')

  const nodeMap = new Map(nodes.map(node => [node.id, node]))
  const rects = new Map(nodes.map(node => [node.id, getNodeRenderRect(node, nodeMap)]))
  const padding = 96
  const bounds = Array.from(rects.values()).reduce((acc, rect) => ({
    minX: Math.min(acc.minX, rect.x),
    minY: Math.min(acc.minY, rect.y),
    maxX: Math.max(acc.maxX, rect.x + rect.width),
    maxY: Math.max(acc.maxY, rect.y + rect.height),
  }), { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity })
  const width = Math.ceil(bounds.maxX - bounds.minX + padding * 2)
  const height = Math.ceil(bounds.maxY - bounds.minY + padding * 2)
  const offsetX = padding - bounds.minX
  const offsetY = padding - bounds.minY
  const isDark = document.documentElement.classList.contains('dark')
  const background = isDark ? '#111827' : '#f8fafc'
  const dot = isDark ? '#374151' : '#e2e8f0'
  const text = isDark ? '#f8fafc' : '#0f172a'
  const muted = isDark ? '#94a3b8' : '#64748b'
  const nodeFill = isDark ? '#1f2937' : '#ffffff'
  const nodeHeader = isDark ? '#111827' : '#f8fafc'

  const parts: string[] = [
    `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-label="Workflow flow">`,
    `<defs><pattern id="flow-grid" width="24" height="24" patternUnits="userSpaceOnUse"><circle cx="1" cy="1" r="1" fill="${dot}"/></pattern><marker id="arrow" markerWidth="10" markerHeight="10" refX="9" refY="5" orient="auto" markerUnits="strokeWidth"><path d="M 0 0 L 10 5 L 0 10 z" fill="#94a3b8"/></marker></defs>`,
    `<rect width="100%" height="100%" fill="${background}"/>`,
    `<rect width="100%" height="100%" fill="url(#flow-grid)"/>`,
  ]

  edges.forEach(edge => {
    const source = rects.get(edge.source)
    const target = rects.get(edge.target)
    if (!source || !target) return
    const fromX = source.x + source.width / 2 + offsetX
    const fromY = source.y + source.height + offsetY
    const toX = target.x + target.width / 2 + offsetX
    const toY = target.y + offsetY
    const midY = fromY + Math.max(24, (toY - fromY) / 2)
    parts.push(`<path d="M ${fromX} ${fromY} C ${fromX} ${midY}, ${toX} ${midY}, ${toX} ${toY}" fill="none" stroke="#94a3b8" stroke-width="2" marker-end="url(#arrow)"/>`)
  })

  nodes.forEach(node => {
    const rect = rects.get(node.id)
    if (!rect) return
    const x = rect.x + offsetX
    const y = rect.y + offsetY
    const data = node.data as WorkflowNodeData
    const title = typeof data.title === 'string' && data.title ? data.title : node.id
    const status = typeof data.status === 'string' ? data.status : 'pending'
    const isStart = node.type === 'start'
    const isEnd = node.type === 'end'
    const isSpecial = isStart || isEnd
    const borderColor =
      status === 'completed' ? '#22c55e' :
      status === 'failed' ? '#ef4444' :
      status === 'running' ? '#3b82f6' :
      isStart ? '#10b981' :
      '#64748b'
    const fill = isSpecial ? (isStart ? (isDark ? '#064e3b' : '#dcfce7') : (isDark ? '#1f2937' : '#f1f5f9')) : nodeFill
    parts.push(`<g filter="drop-shadow(0 5px 10px rgba(15,23,42,0.18))">`)
    parts.push(`<rect x="${x}" y="${y}" width="${rect.width}" height="${rect.height}" rx="${isSpecial ? 20 : 10}" fill="${fill}" stroke="${borderColor}" stroke-width="2"/>`)
    if (isSpecial) {
      parts.push(`<text x="${x + rect.width / 2}" y="${y + 25}" text-anchor="middle" fill="${isStart ? '#10b981' : muted}" font-family="system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="16" font-weight="700">${isStart ? 'Start' : 'End'}</text>`)
    } else {
      parts.push(`<path d="M ${x + 10} ${y} H ${x + rect.width - 10} Q ${x + rect.width} ${y} ${x + rect.width} ${y + 10} V ${y + 58} H ${x} V ${y + 10} Q ${x} ${y} ${x + 10} ${y}" fill="${nodeHeader}"/>`)
      const stepIndex = typeof (data as StepNodeData).stepIndex === 'number' ? (data as StepNodeData).stepIndex : null
      parts.push(`<rect x="${x + 14}" y="${y + 14}" width="32" height="32" rx="7" fill="${isDark ? '#1e3a8a' : '#dbeafe'}"/>`)
      parts.push(`<text x="${x + 30}" y="${y + 35}" text-anchor="middle" fill="${isDark ? '#bfdbfe' : '#1d4ed8'}" font-family="system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="14" font-weight="800">${stepIndex === null ? '#' : String(stepIndex + 1)}</text>`)
      wrapSvgText(title, Math.max(14, Math.floor((rect.width - 70) / 8)), 2).forEach((line, index) => {
        parts.push(`<text x="${x + 56}" y="${y + 28 + index * 18}" fill="${text}" font-family="system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="14" font-weight="700">${escapeSvgText(line)}</text>`)
      })
      parts.push(`<text x="${x + 16}" y="${y + rect.height - 17}" fill="${muted}" font-family="system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="11" font-weight="600">${stepIndex === null ? escapeSvgText(node.type || 'node') : `Step ${stepIndex + 1}`}</text>`)
      if (status !== 'pending') {
        parts.push(`<text x="${x + rect.width - 16}" y="${y + rect.height - 17}" text-anchor="end" fill="${borderColor}" font-family="system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="11" font-weight="800">${escapeSvgText(status.toUpperCase())}</text>`)
      }
    }
    parts.push(`</g>`)
  })

  parts.push(`<text x="${padding}" y="${height - 32}" fill="${muted}" font-family="system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif" font-size="13" font-weight="700">Workflow flow - ${nodes.length} steps</text>`)
  parts.push('</svg>')
  const svg = parts.join('')
  return `data:image/svg+xml;base64,${utf8ToBase64(svg)}`
}

function renderFlowToImage(nodes: WorkflowNode[], edges: WorkflowEdge[], format: WorkflowImageExportFormat): string {
  if (format === 'svg') return renderFlowToSvg(nodes, edges)
  if (nodes.length === 0) throw new Error('No workflow nodes to export')

  const nodeMap = new Map(nodes.map(node => [node.id, node]))
  const rects = new Map(nodes.map(node => [node.id, getNodeRenderRect(node, nodeMap)]))
  const padding = 80
  const bounds = Array.from(rects.values()).reduce((acc, rect) => ({
    minX: Math.min(acc.minX, rect.x),
    minY: Math.min(acc.minY, rect.y),
    maxX: Math.max(acc.maxX, rect.x + rect.width),
    maxY: Math.max(acc.maxY, rect.y + rect.height),
  }), { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity })

  const width = Math.ceil(bounds.maxX - bounds.minX + padding * 2)
  const height = Math.ceil(bounds.maxY - bounds.minY + padding * 2)
  const pixelRatio = Math.min(window.devicePixelRatio || 1, 2)
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(width * pixelRatio))
  canvas.height = Math.max(1, Math.round(height * pixelRatio))

  const context = canvas.getContext('2d')
  if (!context) throw new Error('Could not create image export canvas')

  context.scale(pixelRatio, pixelRatio)
  context.fillStyle = format === 'jpeg' ? '#ffffff' : '#f8fafc'
  context.fillRect(0, 0, width, height)

  context.fillStyle = '#e2e8f0'
  for (let x = 0; x < width; x += 24) {
    for (let y = 0; y < height; y += 24) {
      context.beginPath()
      context.arc(x, y, 1, 0, Math.PI * 2)
      context.fill()
    }
  }

  const offsetX = padding - bounds.minX
  const offsetY = padding - bounds.minY

  context.strokeStyle = '#94a3b8'
  context.fillStyle = '#94a3b8'
  context.lineWidth = 2
  edges.forEach(edge => {
    const source = rects.get(edge.source)
    const target = rects.get(edge.target)
    if (!source || !target) return
    const fromX = source.x + source.width / 2 + offsetX
    const fromY = source.y + source.height + offsetY
    const toX = target.x + target.width / 2 + offsetX
    const toY = target.y + offsetY
    const midY = fromY + Math.max(24, (toY - fromY) / 2)
    context.beginPath()
    context.moveTo(fromX, fromY)
    context.bezierCurveTo(fromX, midY, toX, midY, toX, toY)
    context.stroke()
    drawArrow(context, fromX, fromY, toX, toY)
  })

  nodes.forEach(node => {
    const rect = rects.get(node.id)
    if (rect) drawWorkflowNode(context, node, rect, offsetX, offsetY)
  })

  context.fillStyle = '#475569'
  context.font = '600 13px system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
  context.textAlign = 'left'
  context.fillText(`Workflow flow • ${nodes.length} steps`, padding, height - 32)

  return canvas.toDataURL(format === 'jpeg' ? 'image/jpeg' : 'image/png', 0.95)
}

function workflowExportFilename(workspacePath: string | null, format: WorkflowImageExportFormat): string {
  const workflowName = (workspacePath || 'workflow')
    .split('/')
    .filter(Boolean)
    .pop() || 'workflow'
  const safeName = workflowName.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'workflow'
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-')
  return `${safeName}-flow-${timestamp}.${format === 'jpeg' ? 'jpg' : format}`
}

function DetailSection({
  icon: Icon,
  title,
  children,
}: {
  icon: React.ElementType
  title: string
  children: React.ReactNode
}) {
  return (
    <section className="border-b border-border px-4 py-3 last:border-b-0">
      <div className="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        <Icon className="h-3.5 w-3.5" />
        {title}
      </div>
      {children}
    </section>
  )
}

function ReadOnlyStepDetailPanel({
  node,
  onClose,
}: {
  node: WorkflowNode
  onClose: () => void
}) {
  const data = node.data as WorkflowNodeData
  const step = 'step' in data && data.step ? data.step as PlanStep : null
  const title = (typeof data.title === 'string' && data.title) || step?.title || node.id
  const type = step?.type || node.type || 'node'
  const routes = step?.type === 'routing'
    ? step.routes
    : step?.type === 'todo_task'
      ? step.predefined_routes
      : undefined
  const validationSchema = step?.validation_schema || ('validation_schema' in data ? data.validation_schema : undefined)
  const agentConfigs = step?.agent_configs
  const contextInputs = step?.context_dependencies || []
  const contextOutput = step?.context_output
  const contextOutputs = Array.isArray(contextOutput) ? contextOutput : (contextOutput ? [contextOutput] : [])
  const conditionQuestion = typeof data.condition_question === 'string' ? data.condition_question : undefined
  const routingQuestion = typeof data.routing_question === 'string' ? data.routing_question : undefined

  return (
    <aside className="flex h-full w-[380px] max-w-[42vw] shrink-0 flex-col border-l border-border bg-background shadow-xl">
      <div className="flex shrink-0 items-start gap-3 border-b border-border px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="mb-1 flex flex-wrap items-center gap-2">
            <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">{type}</span>
            {step?.id && <span className="truncate font-mono text-[10px] text-muted-foreground">{step.id}</span>}
          </div>
          <h3 className="truncate text-sm font-semibold text-foreground">{title}</h3>
        </div>
        <button
          onClick={onClose}
          className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          aria-label="Close step details"
          title="Close"
        >
          <X className="h-3.5 w-3.5" />
          <span>Close</span>
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {step?.description && (
          <DetailSection icon={FileText} title="Description">
            <div className="prose prose-sm max-w-none dark:prose-invert prose-p:my-1 prose-headings:my-2 prose-ul:my-1 prose-ol:my-1">
              <MarkdownRenderer content={step.description} className="max-w-none" />
            </div>
          </DetailSection>
        )}

        {(conditionQuestion || routingQuestion || routes?.length) && (
          <DetailSection icon={step?.type === 'routing' || step?.type === 'todo_task' ? Route : GitBranch} title="Routing">
            {conditionQuestion && <p className="mb-2 text-sm text-foreground/85">{conditionQuestion}</p>}
            {routingQuestion && <p className="mb-2 text-sm text-foreground/85">{routingQuestion}</p>}
            {routes?.length ? (
              <div className="space-y-2">
                {routes.map((route, index) => (
                  <div key={route.route_id || route.route_name || index} className="rounded-md border border-border bg-muted/25 p-2">
                    <div className="text-xs font-semibold text-foreground">{route.route_name || route.route_id || `Route ${index + 1}`}</div>
                    {'condition' in route && route.condition && (
                      <div className="mt-1 text-xs leading-relaxed text-muted-foreground">{route.condition}</div>
                    )}
                  </div>
                ))}
              </div>
            ) : null}
          </DetailSection>
        )}

        {(contextInputs.length > 0 || contextOutputs.length > 0) && (
          <DetailSection icon={FileText} title="Context">
            <div className="space-y-2 text-xs">
              {contextInputs.length > 0 && (
                <div>
                  <div className="mb-1 font-medium text-muted-foreground">Reads</div>
                  <div className="space-y-1">{contextInputs.map(path => <div key={path} className="rounded bg-muted/40 px-2 py-1 font-mono text-foreground/80">{path}</div>)}</div>
                </div>
              )}
              {contextOutputs.length > 0 && (
                <div>
                  <div className="mb-1 font-medium text-muted-foreground">Writes</div>
                  <div className="space-y-1">{contextOutputs.map(path => <div key={path} className="rounded bg-muted/40 px-2 py-1 font-mono text-foreground/80">{path}</div>)}</div>
                </div>
              )}
            </div>
          </DetailSection>
        )}

        {!!validationSchema && (
          <DetailSection icon={Braces} title="Schema">
            <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-muted/35 p-2 font-mono text-[11px] leading-relaxed text-foreground/80">
              {formatJson(validationSchema)}
            </pre>
          </DetailSection>
        )}

        {agentConfigs && (
          <DetailSection icon={Settings} title="Step Config">
            <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-muted/35 p-2 font-mono text-[11px] leading-relaxed text-foreground/80">
              {formatJson(agentConfigs)}
            </pre>
          </DetailSection>
        )}

        {!step && (
          <DetailSection icon={FileText} title="Details">
            <pre className="overflow-x-auto whitespace-pre-wrap rounded-md bg-muted/35 p-2 font-mono text-[11px] leading-relaxed text-foreground/80">
              {formatJson(data)}
            </pre>
          </DetailSection>
        )}
      </div>
    </aside>
  )
}

// Ref interface for external control of the canvas
export interface WorkflowCanvasRef {
  refresh: (changedStepIDs?: string[], deletedStepIDs?: string[]) => Promise<PlanChanges | null>
  getStepCount: () => number
  focusStep: (stepId: string) => void  // Alias for highlightStepNode
}

const WorkflowCanvasInner = forwardRef<WorkflowCanvasRef, WorkflowCanvasProps>(({
  workspacePath,
  presetQueryId,
  currentPhase,
  onStartPhase,
  onCreatePlan,
  showChatArea = false,
  onToggleChatArea,
  toolbarOnly = false,
  sharedToolbar = false,
  paneClassName = '',
  className = ''
}, ref) => {
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const highlightTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const { setViewport, getNode, updateNode, fitView, getViewport } = useReactFlow()
  const hasInitializedView = React.useRef(false)

  // --- Performance diagnostics for workflow switching ---
  const renderCountRef = useRef(0)
  const lastPresetRef = useRef(presetQueryId)
  renderCountRef.current++
  if (lastPresetRef.current !== presetQueryId) {
    console.log(`%c[WorkflowCanvas] Preset switched: ${lastPresetRef.current?.slice(0,8)} → ${presetQueryId?.slice(0,8)}`, 'color: orange; font-weight: bold')
    lastPresetRef.current = presetQueryId
  }
  if (renderCountRef.current % 50 === 0) {
    console.log(`%c[WorkflowCanvas] render #${renderCountRef.current} (preset: ${presetQueryId?.slice(0,8)})`, 'color: gray')
  }
  // Store step ID to focus on after nodes update (from backend plan changes)
  const pendingFocusStepIdRef = React.useRef<string | null>(null)
  // Store current viewport state (x, y, zoom) to preserve it during refresh
  const viewportStateRef = React.useRef<{ x: number; y: number; zoom: number } | null>(null)
  const viewportSaveTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null)

  // Get workflow mode, layout direction, and canvas view mode
  // Flow view is vertical-only.
  const layoutDirection: 'LR' | 'TB' = 'TB'
  const canvasViewMode = useWorkflowStore(state => state.canvasViewMode)
  const workflowWorkspaceView = useWorkflowStore(state => state.workflowWorkspaceView)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)

  const isBuilderWorkspace = workflowWorkspaceView === null || workflowWorkspaceView === 'builder'

  // Generate localStorage key for viewport state (workspace-specific)
  const getViewportStorageKey = React.useCallback(() => {
    return workspacePath
      ? `workflow-viewport-${workspacePath}`
      : 'workflow-viewport-default'
  }, [workspacePath])

  // PERF: Debounced viewport change handler — saves to localStorage at most once per 500ms
  // instead of on every pixel of pan/zoom (which was causing excessive localStorage writes)
  const onViewportChange = React.useCallback((viewport: { x: number; y: number; zoom: number }) => {
    viewportStateRef.current = { x: viewport.x, y: viewport.y, zoom: viewport.zoom }
    if (hasInitializedView.current) {
      if (viewportSaveTimerRef.current) clearTimeout(viewportSaveTimerRef.current)
      viewportSaveTimerRef.current = setTimeout(() => {
        try {
          const storageKey = getViewportStorageKey()
          localStorage.setItem(storageKey, JSON.stringify(viewportStateRef.current))
        } catch { /* ignore */ }
      }, 500)
    }
  }, [getViewportStorageKey])

  // Get workflow layout file path
  const getLayoutFilePath = React.useCallback(() => {
    if (!workspacePath) return null
    return `${workspacePath}/planning/workflow_layout.json`
  }, [workspacePath])

  // Load saved node positions and offsets from workspace
  const loadSavedLayout = React.useCallback(async (): Promise<{
    positions: Map<string, { x: number; y: number }>;
    offsets: Map<string, { parentId: string; dx: number; dy: number }>;
    layoutDirection?: 'LR' | 'TB';
  } | null> => {
    const layoutPath = getLayoutFilePath()
    if (!layoutPath) return null

    try {
      const response = await agentApi.getPlannerFileContent(layoutPath)
      if (response.success && response.data?.content) {
        const layout = JSON.parse(response.data.content)
        const positions = new Map<string, { x: number; y: number }>()
        const offsets = new Map<string, { parentId: string; dx: number; dy: number }>()
        let savedDirection: 'LR' | 'TB' | undefined
        
        if (layout.nodePositions && typeof layout.nodePositions === 'object') {
          Object.entries(layout.nodePositions).forEach(([nodeId, pos]: [string, unknown]) => {
            // CRITICAL: Never load saved positions for header nodes
            // They must always use the enforced horizontal layout from usePlanToFlow
            if (nodeId === 'start' || nodeId === 'variables') {
              return // Skip header nodes
            }
            if (pos && typeof pos === 'object' && 'x' in pos && 'y' in pos) {
              positions.set(nodeId, { x: (pos as { x: number }).x, y: (pos as { x: number; y: number }).y })
            }
          })
        }
        
        // Load child offsets if available (version 1.1+)
        if (layout.childOffsets && typeof layout.childOffsets === 'object') {
          Object.entries(layout.childOffsets).forEach(([nodeId, offset]: [string, unknown]) => {
            if (offset && typeof offset === 'object' && 'parentId' in offset && 'dx' in offset && 'dy' in offset) {
              offsets.set(nodeId, {
                parentId: (offset as { parentId: string }).parentId,
                dx: (offset as { dx: number }).dx,
                dy: (offset as { dy: number }).dy
              })
            }
          })
        }

        // Load layout direction if available (version 1.2+)
        if (layout.layoutDirection === 'LR' || layout.layoutDirection === 'TB') {
          savedDirection = layout.layoutDirection
        }
        
        console.log('[WorkflowCanvas] 📂 Loaded saved layout:', positions.size, 'positions,', offsets.size, 'offsets, direction:', savedDirection)
        return { positions, offsets, layoutDirection: savedDirection }
      }
    } catch {
      // File doesn't exist yet - that's okay
      // No saved layout found - this is normal for new workspaces
    }
    return null
  }, [getLayoutFilePath])

  // Variables state
  const [variablesManifest, setVariablesManifest] = React.useState<VariablesManifest | null>(null)
  const [isLoadingVariables, setIsLoadingVariables] = React.useState(false)
  const [showVariablesSidebar, setShowVariablesSidebar] = React.useState(false)
  const [selectedFlowNode, setSelectedFlowNode] = React.useState<WorkflowNode | null>(null)
  
  // Workflow store actions
  const setVariablesManifestInStore = useWorkflowStore.getState().setVariablesManifest
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  // Highlight execution folder in workspace when selectedRunFolder changes
  // This ensures workspace shows the correct group folder during multi-group execution
  const { highlightFile } = useWorkspaceStore()
  const prevSelectedRunFolderRef = useRef<string | null>(null)
  useEffect(() => {
    // Reset ref if selectedRunFolder is cleared
    if (!selectedRunFolder || selectedRunFolder === 'new') {
      prevSelectedRunFolderRef.current = null
      return
    }

    // Only highlight if selectedRunFolder actually changed and is valid.
    // Guard: skip when the workflow canvas isn't the active mode — this effect
    // can fire while the canvas stays mounted in other modes (e.g. multi-agent
    // chat), and the fetchFiles(workspacePath) below would overwrite the
    // workspace state with workflow-scoped files, leaving the multi-agent file
    // panel empty after the filter pass.
    const activeMode = useModeStore.getState().selectedModeCategory
    if (activeMode !== 'workflow') {
      return
    }

    if (selectedRunFolder !== prevSelectedRunFolderRef.current && workspacePath) {
      prevSelectedRunFolderRef.current = selectedRunFolder

      // Construct execution folder path
      const executionPath = `${workspacePath}/runs/${selectedRunFolder}/execution`

      // PERF: Use getState() to avoid fetchFiles reference changes triggering this effect
      useWorkspaceStore.getState().fetchFiles(workspacePath || undefined).then(() => {
        // Small delay to ensure files are loaded before highlighting
        setTimeout(() => {
          highlightFile(executionPath)
        }, 100)
      }).catch(err => {
        console.error('[WorkflowCanvas] Failed to refresh files before highlighting:', err)
        // Still try to highlight even if refresh fails
        highlightFile(executionPath)
      })
    }
  }, [selectedRunFolder, workspacePath, highlightFile])

  // Load workflow data for the main canvas and append evaluation steps to it.
  const planData = usePlanData(workspacePath)
  const evalData = useEvaluationPlanData(workspacePath)

  const plan = planData.plan
  const evaluationPlan = evalData.evaluationPlan
  const refreshEvaluationPlan = evalData.refresh

  const loading = planData.loading || evalData.loading
  const error = planData.error
  const changes = planData.changes

  const loadPlanRefresh = planData.refresh
  const clearChanges = planData.clearChanges
  const setChanges = planData.setChanges

  // *** NEW CONSOLIDATED API ***
  // Load all workspace state (run folders, variables, phases, progress) in one call
  // This replaces the old individual API calls and eliminates race conditions
  const {
    state: workspaceState,
    loading: isLoadingWorkspaceState,
    error: workspaceStateError,
    isRetrying: isRetryingWorkspaceState,
    retryCountdown: workspaceStateRetryCountdown,
    refresh: refreshWorkspaceState
  } = useWorkspaceState(workspacePath, selectedRunFolder)

  // Sync workspace state to local state for backward compatibility
  // TODO: Eventually migrate all consumers to use workspaceState directly
  React.useEffect(() => {
    if (workspaceState) {
      const manifest = workspaceState.variables_manifest || null
      setVariablesManifest(manifest)
      setIsLoadingVariables(false)

    } else if (!isLoadingWorkspaceState) {
      setVariablesManifest(null)
      setIsLoadingVariables(false)
    } else {
      setIsLoadingVariables(isLoadingWorkspaceState)
    }
  }, [workspaceState, isLoadingWorkspaceState])

  // Transform run folders for WorkflowToolbar (memoized to avoid repeated transformations)
  const runFoldersForToolbar = React.useMemo(() => {
    if (!workspaceState?.run_folders) return []
    return workspaceState.run_folders.map(f => ({ name: f.name }))
  }, [workspaceState?.run_folders])

  useEffect(() => {
    if (!isBuilderWorkspace || !workspaceState?.run_folders?.length) {
      return
    }

    const availableRunFolders = new Set(workspaceState.run_folders.map(folder => folder.name))
    const activeRunFolder = workspaceState.active_executions?.find(execution => execution.run_folder)?.run_folder
    if (activeRunFolder && availableRunFolders.has(activeRunFolder) && selectedRunFolder !== activeRunFolder) {
      setSelectedRunFolder(activeRunFolder)
      return
    }

    if (
      selectedRunFolder &&
      selectedRunFolder !== 'new' &&
      availableRunFolders.has(selectedRunFolder)
    ) {
      return
    }

    const preferredGroupId = selectedGroupIds[0]
      || variablesManifest?.groups?.find(group => group.enabled !== false)?.name
      || null

    const builderGroupRunFolder = preferredGroupId
      ? buildGroupFolderPath(preferredGroupId, 'iteration-0', variablesManifest)
      : null

    const fallbackBuilderRunFolder =
      (builderGroupRunFolder && availableRunFolders.has(builderGroupRunFolder) && builderGroupRunFolder)
      || (availableRunFolders.has('iteration-0') ? 'iteration-0' : null)
      || workspaceState.run_folders.find(folder => folder.name.startsWith('iteration-0/'))?.name
      || null

    if (fallbackBuilderRunFolder) {
      setSelectedRunFolder(fallbackBuilderRunFolder)
    }
  }, [
    isBuilderWorkspace,
    workspaceState?.run_folders,
    workspaceState?.active_executions,
    selectedRunFolder,
    selectedGroupIds,
    variablesManifest,
    setSelectedRunFolder
  ])

  // Log workspace state errors
  React.useEffect(() => {
    if (workspaceStateError) {
      console.error('[WorkflowCanvas] Workspace state error:', workspaceStateError)
    }
  }, [workspaceStateError])

  // Callback for opening variables sidebar
  const handleOpenVariablesSidebar = useCallback(() => {
    setShowVariablesSidebar(true)
  }, [])

  // Callback for when variables are updated
  const handleVariablesUpdate = useCallback((manifest: VariablesManifest) => {
    setVariablesManifest(manifest)
    // Also update in workflow store for buildExecutionOptions to access
    setVariablesManifestInStore(manifest)
  }, [setVariablesManifestInStore])

  // Refresh handler - reloads plan, step config, and workspace state
  const handleRefresh = useCallback(async () => {
    if (!workspacePath) return

    console.log('[WorkflowCanvas] Refreshing plan, step config, and workspace state...')

    // Save current viewport state before refresh
    // Only save if viewport has been initialized (not on first load)
    const currentViewport = hasInitializedView.current ? viewportStateRef.current : null
    console.log('[WorkflowCanvas] Saving viewport state before refresh:', currentViewport, 'hasInitializedView:', hasInitializedView.current)

    await Promise.all([
      loadPlanRefresh(),
      refreshEvaluationPlan(),
      refreshWorkspaceState()
    ])

    // Restore viewport state after refresh completes
    // Only restore if we had a saved viewport (not on first load)
    // Use a small delay to ensure nodes have been updated
    if (currentViewport && hasInitializedView.current) {
      setTimeout(() => {
        console.log('[WorkflowCanvas] Restoring viewport state after refresh:', currentViewport)
        setViewport(
          { x: currentViewport.x, y: currentViewport.y, zoom: currentViewport.zoom },
          { duration: 300 }
        )
      }, 100)
    }

    console.log('[WorkflowCanvas] Refresh completed')
  }, [workspacePath, loadPlanRefresh, refreshEvaluationPlan, refreshWorkspaceState, setViewport])

  // Workflow execution
  const {
    status
  } = useWorkflowExecution()

  // Current step and status from store (set by ChatArea polling when step_progress_updated events arrive)
  const currentStepId = useWorkflowStore(state => state.currentStepId)
  const stepStatusMap = useWorkflowStore(state => state.stepStatusMap)

  // React Flow state (need to define before usePlanToFlow to use in callbacks)
  const [nodes, setNodes, onNodesChangeBase] = useNodesState<WorkflowNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<WorkflowEdge>([])
  const [isExportingImage, setIsExportingImage] = React.useState(false)
  // Store latest nodes in ref to avoid dependency issues
  const nodesRef = React.useRef(nodes)
  React.useEffect(() => {
    nodesRef.current = nodes
  }, [nodes])
  const edgesRef = React.useRef(edges)
  React.useEffect(() => {
    edgesRef.current = edges
  }, [edges])

  const handleExportImage = useCallback(async (format: WorkflowImageExportFormat) => {
    if (toolbarOnly || canvasViewMode !== 'flow') return
    setIsExportingImage(true)
    const previousViewport = getViewport()
    try {
      const filename = workflowExportFilename(workspacePath, format)
      const flowElement = reactFlowWrapper.current?.querySelector<HTMLElement>('.react-flow')
      let result: { canceled?: boolean; filePath?: string } | null = null

      if (flowElement) {
        flowElement.setAttribute('data-flow-exporting', 'true')
        try {
          await fitView({ padding: 0.18, duration: 0, minZoom: 0.15, maxZoom: 1.1 })
          await waitForAnimationFrames(3)
          if (format === 'svg' || format === 'png') {
            const svgDataUrl = renderFlowElementToSvg(flowElement)
            const dataUrl = format === 'png' ? await svgDataUrlToPngDataUrl(svgDataUrl) : svgDataUrl
            result = await saveWorkflowImage(dataUrl, filename, format)
          } else {
            result = await captureWorkflowImage(filename, format, flowElement.getBoundingClientRect())
          }
        } finally {
          flowElement.removeAttribute('data-flow-exporting')
        }
      }

      if (!result) {
        const dataUrl = renderFlowToImage(nodesRef.current, edgesRef.current, format)
        result = await saveWorkflowImage(dataUrl, filename, format)
      }

      if (result?.canceled) return
      const location = result?.filePath ? ` to ${result.filePath}` : ''
      const formatLabel = format === 'jpeg' ? 'JPG' : format.toUpperCase()
      useChatStore.getState().addToast(`Exported flow as ${formatLabel}${location}`, 'success')
    } catch (error) {
      console.error('[WorkflowCanvas] Failed to export flow image:', error)
      useChatStore.getState().addToast(error instanceof Error ? error.message : 'Failed to export flow image', 'error')
    } finally {
      setViewport(previousViewport, { duration: 0 })
      setIsExportingImage(false)
    }
  }, [canvasViewMode, fitView, getViewport, setViewport, toolbarOnly, workspacePath])

  // Map of parent node ID to child node IDs (for grouped movement)
  const nodeGroupsRef = React.useRef<Map<string, string[]>>(new Map())
  
  // Map of child node ID to parent node ID (for quick lookup)
  const childToParentRef = React.useRef<Map<string, string>>(new Map())
  
  // Map of child node ID to relative offset from parent { dx, dy }
  const childOffsetsRef = React.useRef<Map<string, { dx: number; dy: number }>>(new Map())

  // Store current node positions before refresh (to preserve layout when saving from sidebar)
  const currentPositionsRef = React.useRef<Map<string, { x: number; y: number }>>(new Map())
  const currentOffsetsRef = React.useRef<Map<string, { parentId: string; dx: number; dy: number }>>(new Map())

  const saveCurrentLayout = useCallback(async (currentNodes: WorkflowNode[]) => {
    if (toolbarOnly || canvasViewMode !== 'flow') return

    const layoutPath = getLayoutFilePath()
    if (!layoutPath) return

    const nodePositions: Record<string, { x: number; y: number }> = {}
    const childOffsets: Record<string, { parentId: string; dx: number; dy: number }> = {}
    const nodeById = new Map(currentNodes.map(node => [node.id, node]))

    currentNodes.forEach(node => {
      if (node.id === 'start' || node.id === 'variables') {
        return
      }
      nodePositions[node.id] = { x: node.position.x, y: node.position.y }
    })

    childToParentRef.current.forEach((parentId, nodeId) => {
      const childNode = nodeById.get(nodeId)
      const parentNode = nodeById.get(parentId)
      if (!childNode || !parentNode) {
        return
      }
      childOffsets[nodeId] = {
        parentId,
        dx: childNode.position.x - parentNode.position.x,
        dy: childNode.position.y - parentNode.position.y
      }
    })

    const layout = {
      version: '1.2',
      updatedAt: new Date().toISOString(),
      layoutDirection,
      nodePositions,
      childOffsets
    }

    try {
      await agentApi.updatePlannerFile(
        layoutPath,
        JSON.stringify(layout, null, 2),
        'Save workflow canvas layout'
      )
      console.log('[WorkflowCanvas] Saved custom layout:', Object.keys(nodePositions).length, 'positions')
    } catch (error) {
      console.error('[WorkflowCanvas] Failed to save custom layout:', error)
    }
  }, [canvasViewMode, getLayoutFilePath, layoutDirection, toolbarOnly])

  // Build node groups: map parent nodes to their child nodes (validation, learning, evaluation, sub-agents)
  const buildNodeGroups = useCallback((currentNodes: WorkflowNode[]) => {
    const groups = new Map<string, string[]>()
    const childToParent = new Map<string, string>()
    const offsets = new Map<string, { dx: number; dy: number }>()

    // Helper to check if a node is a parent node type
    const isParentNode = (node: WorkflowNode): boolean => {
      return node.type === 'step' ||
             node.type === 'conditional' ||
                node.type === 'human_input'
    }

    // Also treat sub-agents as parent nodes (they have learning/validation children)
    const isSubAgentNode = (node: WorkflowNode): boolean => {
      return node.id.includes('-sub-agent-')
    }

    // First pass: Build groups for regular parent nodes (step, conditional, decision, loop, orchestrator, human_input)
    currentNodes.forEach(parentNode => {
      if (!isParentNode(parentNode)) return

      const children: string[] = []
      
      // Find validation, learning, and evaluation nodes by parentStepId
      currentNodes.forEach(childNode => {
        if (childNode.type === 'validation') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === parentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, parentNode.id)
            // Calculate relative offset
            const dx = childNode.position.x - parentNode.position.x
            const dy = childNode.position.y - parentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        } else if (childNode.type === 'learning') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === parentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, parentNode.id)
            const dx = childNode.position.x - parentNode.position.x
            const dy = childNode.position.y - parentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        } else if (childNode.type === 'evaluation') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === parentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, parentNode.id)
            const dx = childNode.position.x - parentNode.position.x
            const dy = childNode.position.y - parentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        }
      })

      if (children.length > 0) {
        groups.set(parentNode.id, children)
      }
    })

    // Second pass: Build groups for sub-agents (they have learning/validation children)
    currentNodes.forEach(subAgentNode => {
      if (!isSubAgentNode(subAgentNode)) return

      const children: string[] = []
      
      // Find validation, learning, and evaluation nodes that belong to this sub-agent
      currentNodes.forEach(childNode => {
        if (childNode.type === 'validation' || childNode.type === 'learning' || childNode.type === 'evaluation') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === subAgentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, subAgentNode.id)
            const dx = childNode.position.x - subAgentNode.position.x
            const dy = childNode.position.y - subAgentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        }
      })

      if (children.length > 0) {
        groups.set(subAgentNode.id, children)
      }
    })

    nodeGroupsRef.current = groups
    childToParentRef.current = childToParent
    childOffsetsRef.current = offsets
  }, [])

  // Custom onNodesChange handler that groups nodes together
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    // Allow all nodes to be draggable: sub-agents, validation, learning, evaluation, and parent nodes
    // These nodes can be manually positioned independently
    const filteredChanges = changes.filter(change => {
      if (change.type === 'position') {
        const nodeId = change.id
        // Allow sub-agents to be draggable (they're children but should be independently movable)
        if (nodeId.includes('-sub-agent-')) {
          return true // Allow sub-agents to be draggable
        }
        // Allow validation, learning, and evaluation nodes to be draggable
        const node = nodesRef.current.find(n => n.id === nodeId)
        if (node && (node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact')) {
          return true // Allow validation, learning, and evaluation nodes to be draggable
        }
        // Check if this is a child node (has a parent) - these should not be draggable
        // But we've already handled sub-agents and validation/learning/evaluation above
        if (childToParentRef.current.has(nodeId)) {
          return false // Ignore position changes for other child nodes
        }
      }
      return true
    })

    // Apply filtered changes
    onNodesChangeBase(filteredChanges as NodeChange<WorkflowNode>[])

    // Check if any parent node position changed (including sub-agents, validation, learning, evaluation)
    const parentPositionChanges = new Map<string, { x: number; y: number }>()
    
    filteredChanges.forEach(change => {
      if (change.type === 'position' && change.position) {
        const nodeId = change.id
        const node = nodesRef.current.find(n => n.id === nodeId)
        // Include sub-agents, validation, learning, and evaluation nodes as independently movable
        const isSubAgent = nodeId.includes('-sub-agent-')
        const isValidationLearningEval = node && (node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact')
        // Check if this is a parent node (not a child) OR a sub-agent OR validation/learning/evaluation
        if (isSubAgent || isValidationLearningEval || (nodeGroupsRef.current.has(nodeId) && !childToParentRef.current.has(nodeId))) {
          parentPositionChanges.set(nodeId, { x: change.position.x, y: change.position.y })
        }
      }
    })

    // If any parent nodes moved, update their children (with cascading updates)
    if (parentPositionChanges.size > 0) {
      setNodes((nds) => {
        // First pass: update direct children
        // Note: Sub-agents SHOULD move with their parent orchestrator
        // Validation, learning, and evaluation nodes remain independent
        let updatedNodes = nds.map(node => {
          const parentId = childToParentRef.current.get(node.id)
          
          // Skip if this is a validation, learning, or evaluation node
          // These are independent and can be manually positioned
          const isValidationLearningEval = node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact'
          if (isValidationLearningEval) {
            return node // These nodes are independent, don't update them here
          }
          
          if (parentId && parentPositionChanges.has(parentId)) {
            const newParentPos = parentPositionChanges.get(parentId)!
            const offset = childOffsetsRef.current.get(node.id)
            if (offset) {
              return {
                ...node,
                position: {
                  x: newParentPos.x + offset.dx,
                  y: newParentPos.y + offset.dy
                }
              }
            }
          }
          return node
        })

        // Second pass: update children of nodes that moved in first pass (cascading)
        // This handles orchestrator -> sub-agents -> learning nodes
        const nodesThatMoved = new Set<string>()
        updatedNodes.forEach(node => {
          const parentId = childToParentRef.current.get(node.id)
          if (parentId && parentPositionChanges.has(parentId)) {
            nodesThatMoved.add(node.id)
          }
        })

        // Update children of nodes that moved
        // Skip validation, learning, and evaluation nodes (they're independent)
        updatedNodes = updatedNodes.map(node => {
          // Skip validation, learning, and evaluation nodes - they're independent
          const isValidationLearningEval = node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact'
          if (isValidationLearningEval) {
            return node
          }
          
          const parentId = childToParentRef.current.get(node.id)
          if (parentId && nodesThatMoved.has(parentId)) {
            // Find the updated parent node
            const updatedParent = updatedNodes.find(n => n.id === parentId)
            if (updatedParent) {
              const offset = childOffsetsRef.current.get(node.id)
              if (offset) {
                return {
                  ...node,
                  position: {
                    x: updatedParent.position.x + offset.dx,
                    y: updatedParent.position.y + offset.dy
                  }
                }
              }
            }
          }
          return node
        })

        return updatedNodes
      })
    }

  }, [onNodesChangeBase, setNodes])

  const onNodeDragStop = useCallback<OnNodeDrag<WorkflowNode>>(() => {
    // Let React Flow apply the final position first, then persist the current graph.
    window.setTimeout(() => {
      void saveCurrentLayout(nodesRef.current)
    }, 0)
  }, [saveCurrentLayout])

  // Single reusable function to focus/position a node at the top-left of the screen
  const focusNode = useCallback((
    nodeId: string,
    options?: {
      topPadding?: number  // Vertical padding from top (default: 50)
      delay?: number  // Delay before positioning (default: 100ms)
    }
  ) => {
    const {
      topPadding = 50,
      delay = 100
    } = options || {}

    setTimeout(() => {
      const flowNode = getNode(nodeId)
      if (flowNode) {
        const padding = 150 // Padding from left edge
        setViewport(
          {
            x: padding - flowNode.position.x, // Position on left with padding
            y: topPadding - flowNode.position.y, // Position at top with padding
            zoom: 1.0
          },
          { duration: 500 }
        )

      }
    }, delay)
  }, [getNode, setViewport])

  // Handle navigating to a step from legend (without opening sidebar)
  const handleNavigateToStep = useCallback((nodeId: string) => {
    focusNode(nodeId, { topPadding: 150, delay: 100 })
    console.log('[WorkflowCanvas] Navigated to step from legend:', nodeId)
  }, [focusNode])

  // Stabilize stepStatusMap by serializing it - Maps are compared by reference, so we need to serialize
  // to detect actual content changes. This prevents unnecessary recalculations in usePlanToFlow.
  const stableStepStatusMap = React.useMemo(() => {
    if (!stepStatusMap || stepStatusMap.size === 0) {
      return null // Return null instead of the Map to ensure stable reference
    }
    // Serialize Map to object for stable comparison
    const serialized = Object.fromEntries(stepStatusMap)
    return serialized
  }, [stepStatusMap])

  // Convert plan to React Flow nodes and edges (with change highlights and run callback)
  const planFlow = usePlanToFlow(plan, {
    changes,  // Pass changes to highlight modified nodes
    stepStatusMap: stableStepStatusMap,  // Pass stabilized step status map
    workspacePath,  // Pass workspace path for file opening
    selectedRunFolder: selectedRunFolder ?? undefined,  // Pass selected run folder for file opening (convert null to undefined)
    variablesManifest,  // Pass variables manifest for Variables node
    onOpenVariablesSidebar: handleOpenVariablesSidebar,  // Callback for opening variables sidebar
    isLoadingVariables,  // Whether variables are loading
    layoutDirection,  // Layout direction: 'LR' for horizontal, 'TB' for vertical
    disabled: toolbarOnly
  })

  const augmentedFlow = React.useMemo(() => {
    if (!planFlow.nodes.length) {
      return planFlow
    }

    const nodes = [...planFlow.nodes]
    const edges = [...planFlow.edges]
    const endNode = nodes.find(node => node.id === 'end')

    if (!endNode) {
      return planFlow
    }

    const evaluationSteps = evaluationPlan?.steps ?? []
    if (evaluationSteps.length === 0) {
      return { nodes, edges }
    }

    const evalNodeIds = evaluationSteps.map((step, index) => `workflow-evaluation-step-${step.id || index}`)
    const isHorizontal = isHorizontalWorkflowLayout(layoutDirection)
    const stepGap = isHorizontal ? 360 : 190

    evaluationSteps.forEach((step, index) => {
      const nodeId = evalNodeIds[index]
      const position = isHorizontal
        ? { x: endNode.position.x + ((index + 1) * stepGap), y: endNode.position.y }
        : { x: endNode.position.x - 100, y: endNode.position.y + ((index + 1) * stepGap) }

      const data: EvaluationStepNodeData = {
        id: nodeId,
        title: step.title || `Evaluation step ${index + 1}`,
        description: step.description,
        success_criteria: step.success_criteria,
        status: 'pending',
        stepIndex: index,
        step,
        workspacePath,
        selectedRunFolder: selectedRunFolder ?? undefined,
        isEvaluationStep: true
      }

      nodes.push({
        id: nodeId,
        type: 'step',
        position,
        data,
        draggable: true
      })

      const source = index === 0 ? 'end' : evalNodeIds[index - 1]
      edges.push({
        id: `${source}-to-${nodeId}`,
        source,
        target: nodeId,
        type: 'smoothstep',
        style: {
          stroke: '#6b7280',
          strokeWidth: 2
        }
      })
    })

    return { nodes, edges }
  }, [planFlow, evaluationPlan, layoutDirection, workspacePath, selectedRunFolder])

  const { nodes: initialNodes, edges: initialEdges } = augmentedFlow

  // Helper function to highlight and position a specific step node
  const highlightStepNode = useCallback((stepId: string) => {
    // Find the node by matching step.id in node data (works for both top-level and branch steps)
    // Branch steps have node IDs like "step-3-true-0" but step.id is the actual step ID
    const nodeToFocus = nodesRef.current.find(node => {
      if (node.type === 'step' || node.type === 'conditional') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData
        const nodeStepId = nodeData?.step?.id
        // Match by step.id (for branch steps) or by node ID (for top-level steps)
        return nodeStepId === stepId || (nodeStepId === undefined && node.id === stepId)
      }
      return false
    })

    if (nodeToFocus) {
      console.log('[WorkflowCanvas] highlightStepNode found node:', nodeToFocus.id)
      // Focus viewport on the node but don't select it (don't open sidebar)
      // User can manually open sidebar if needed
      focusNode(nodeToFocus.id, { topPadding: 150, delay: 100 })
    } else {
      console.log('[WorkflowCanvas] highlightStepNode - no node found for stepId:', stepId)
    }
  }, [focusNode])

  // Auto-focus disabled - running step name is now shown in StepLegend instead
  // This prevents the canvas from jumping around during workflow execution

  // Expose methods via ref
  useImperativeHandle(ref, () => ({
    refresh: async (changedStepIDs?: string[], deletedStepIDs?: string[]) => {
      // Refresh plan to get latest data
      await loadPlanRefresh()

      // If granular change data is provided, use it directly
      if (changedStepIDs || deletedStepIDs) {
        // The backend combines added and updated into changed_step_ids
        // For now, we'll treat all changedStepIDs as "updated" since the backend combines them
        // The visual highlighting will work correctly (blue ring for updated steps)
        const updated = changedStepIDs?.filter(id => !deletedStepIDs?.includes(id)) || []
        const deleted = deletedStepIDs || []
        const changes: PlanChanges = {
          added: [], // Backend combines added into changed_step_ids, so we can't distinguish here
          updated,
          deleted,
          hasChanges: updated.length > 0 || deleted.length > 0
        }
        // Set changes directly from granular event data
        if (changes.hasChanges) {
          setChanges(changes)
        }
        return changes
      }

      // No granular data - just refresh without setting changes
      return null
    },
    getStepCount: () => {
      // Count steps from plan data
      if (!plan?.steps) return 0
      return plan.steps.length
    },
    focusStep: (stepId: string) => {
      // Use the existing highlightStepNode function
      highlightStepNode(stepId)
    }
  }), [loadPlanRefresh, plan, setChanges, highlightStepNode])

  // Store step ID to focus on when changes are detected (will focus after nodes update)
  React.useEffect(() => {
    if (changes?.hasChanges) {
      // Store the step ID to focus on (will be used after nodes are updated)
      const stepToFocus = changes.added?.[0] || changes.updated?.[0]
      if (stepToFocus) {
        pendingFocusStepIdRef.current = stepToFocus
      }
      
      // Clear any existing timeout
      if (highlightTimeoutRef.current) {
        clearTimeout(highlightTimeoutRef.current)
      }
      
      // Set new timeout to clear highlights
      highlightTimeoutRef.current = setTimeout(() => {
        console.log('[WorkflowCanvas] Clearing change highlights after', HIGHLIGHT_DURATION, 'ms')
        clearChanges()
      }, HIGHLIGHT_DURATION)
    }

    // Cleanup on unmount
    return () => {
      if (highlightTimeoutRef.current) {
        clearTimeout(highlightTimeoutRef.current)
      }
    }
  }, [changes, clearChanges])

  // Track previous nodes/edges to detect actual changes
  const prevNodesRef = React.useRef<typeof initialNodes>([])
  const prevEdgesRef = React.useRef<typeof initialEdges>([])
  // CRITICAL: Force header nodes to correct positions after nodes update
  // Ensure header nodes maintain correct positions (safety net in case something tries to override them)
  React.useEffect(() => {
    if (toolbarOnly) return // Skip when canvas is hidden
    if (nodes.length === 0 || initialNodes.length === 0) return
    
    const varsNode = initialNodes.find(n => n.id === 'variables')
    const startNode = initialNodes.find(n => n.id === 'start')
    
    if (!varsNode && !startNode) return
    
    // Check if any header node position has been overridden
    const currentVars = nodes.find(n => n.id === 'variables')
    const currentStart = nodes.find(n => n.id === 'start')
    
    let needsFix = false
    
    if (varsNode && currentVars && 
        (currentVars.position.x !== varsNode.position.x || currentVars.position.y !== varsNode.position.y)) {
      needsFix = true
    }
    if (startNode && currentStart && 
        (currentStart.position.x !== startNode.position.x || currentStart.position.y !== startNode.position.y)) {
      needsFix = true
    }
    
    if (needsFix) {
      // Use updateNode API to restore correct positions
      if (varsNode) updateNode('variables', { position: varsNode.position })
      if (startNode) updateNode('start', { position: startNode.position })
    }
  }, [nodes, initialNodes, updateNode, toolbarOnly, canvasViewMode])

  // Rebuild node groups when nodes change
  React.useEffect(() => {
    if (toolbarOnly) return // Skip when canvas is hidden
    hasInitializedView.current = false
  }, [canvasViewMode, toolbarOnly])

  React.useEffect(() => {
    if (toolbarOnly) return // Skip when canvas is hidden
    if (nodes.length > 0) {
      buildNodeGroups(nodes)
    }
  }, [nodes, buildNodeGroups, toolbarOnly, canvasViewMode])

  // Update nodes when plan changes (only if nodes actually changed)
  React.useEffect(() => {
    // Skip node/edge updates when the flow canvas is hidden. The saved layout
    // only applies to React Flow; report/toolbar-only views should not fetch it.
    if (toolbarOnly || canvasViewMode !== 'flow') return

    // Compare by reference first (fast path)
    if (prevNodesRef.current === initialNodes && prevEdgesRef.current === initialEdges) {
      return // No change
    }
    
    // Compare by length, IDs, node data (status), and step configs to detect actual changes
    const nodesChanged =
      prevNodesRef.current.length !== initialNodes.length ||
      prevNodesRef.current.some((node, i) => {
        const newNode = initialNodes[i]
        if (!newNode) return true
        // Check if ID changed
        if (node?.id !== newNode.id) return true
        // Check if position changed (important for layout direction changes)
        if (node?.position?.x !== newNode.position?.x || node?.position?.y !== newNode.position?.y) return true
        // Check if status changed (important for completed steps highlighting)
        if (node?.data?.status !== newNode.data?.status) return true
        
        // Check if VariablesNode manifest changed
        if (node?.type === 'variables' || newNode.type === 'variables') {
          const oldData = node?.data as VariablesNodeData | undefined
          const newData = newNode.data as VariablesNodeData | undefined
          const oldManifest = oldData?.manifest
          const newManifest = newData?.manifest
          const oldManifestStr = JSON.stringify(oldManifest)
          const newManifestStr = JSON.stringify(newManifest)
          if (oldManifestStr !== newManifestStr) {
            console.log(`[WorkflowPlanUpdate] Variables node manifest changed`)
            return true
          }
        }
        
        // Check if step data changed (especially agent_configs)
        // This is important when saving config in the side panel
        const oldData = node?.data as StepNodeData | ConditionalNodeData | undefined
        const newData = newNode?.data as StepNodeData | ConditionalNodeData | undefined
        const oldStep = oldData?.step
        const newStep = newData?.step
        if (oldStep && newStep) {
          // Compare agent_configs by JSON stringify (handles nested objects)
          const oldConfigs = JSON.stringify(oldStep.agent_configs || {})
          const newConfigs = JSON.stringify(newStep.agent_configs || {})
          if (oldConfigs !== newConfigs) {
            console.log(`[WorkflowPlanUpdate] Node ${node.id} agent_configs changed`)
            return true
          }
          // Also check if other step fields changed
          const oldStepStr = JSON.stringify(oldStep)
          const newStepStr = JSON.stringify(newStep)
          if (oldStepStr !== newStepStr) {
            console.log(`[WorkflowPlanUpdate] Node ${node.id} step data changed`)
            return true
          }
        } else if (oldStep !== newStep) {
          // One has step data and the other doesn't
          console.log(`[WorkflowPlanUpdate] Node ${node.id} step data presence changed`)
          return true
        }
        return false
      })
    
    const edgesChanged = 
      prevEdgesRef.current.length !== initialEdges.length ||
      prevEdgesRef.current.some((edge, i) => edge?.id !== initialEdges[i]?.id)
    
    if (nodesChanged) {
      // Nodes changed - will apply positions from usePlanToFlow
      console.log(`%c[WorkflowCanvas] setNodes: ${initialNodes.length} nodes (preset: ${presetQueryId?.slice(0,8)})`, 'color: #4CAF50')
      console.time(`[WorkflowCanvas] setNodes-${presetQueryId?.slice(0,8)}`)
      setNodes(initialNodes)

      // Always try to restore positions after nodes regenerate (unless layout direction changed)
      // Priority: 1) Saved layout from file, 2) Current positions (captured before refresh), 3) Auto-layout
      if (initialNodes.length > 0) {
        // Extract header node positions from initialNodes BEFORE any restoration
        // These positions are calculated by usePlanToFlow and MUST be preserved
        const headerNodePositions = new Map<string, { x: number; y: number }>()
        initialNodes.forEach(node => {
          if (node.id === 'start' || node.id === 'variables') {
            headerNodePositions.set(node.id, { x: node.position.x, y: node.position.y })
          }
        })
        // Checking for saved layout...
        
        // First try to load saved layout from file
        loadSavedLayout().then(savedLayout => {
          // Use saved layout if available, otherwise use current positions (captured before refresh)
          const positionsToUse = savedLayout?.positions && savedLayout.positions.size > 0
            ? savedLayout.positions
            : currentPositionsRef.current
          const offsetsToUse = savedLayout?.offsets && savedLayout.offsets.size > 0
            ? savedLayout.offsets
            : currentOffsetsRef.current
          
          if (positionsToUse.size > 0) {
            setNodes((nds) => {
              // First, apply saved/current positions to parent nodes
              let updated = nds.map(node => {
                const savedPos = positionsToUse.get(node.id)
                
                // Header nodes are skipped from restoration (will be forced to correct positions later)

                // Apply saved position unless it's a header node (start, variables)
                // Header nodes MUST always use the enforced horizontal layout from usePlanToFlow
                if (savedPos && node.id !== 'start' && node.id !== 'variables') {
                  return { ...node, position: savedPos }
                }
                return node
              })
              
            // Build groups from original auto-layout to get parent-child relationships
              buildNodeGroups(nds)
              
              // If we have saved/current offsets, use them (version 1.1+)
              // Otherwise, fall back to calculating from original auto-layout
              // Note: Sub-agents are now saved as parent positions, not offsets
              if (offsetsToUse.size > 0) {
                // Apply offsets in multiple passes to handle cascading parent-child relationships
                // Pass 1: Apply offsets for nodes whose parent is a top-level parent (orchestrator, step, etc.)
                // Pass 2: Apply learning/validation offsets (relative to sub-agents or other parents)
                
                // First pass: Apply offsets for nodes whose parent is a top-level parent (orchestrator, step, etc.)
                // Skip sub-agents, validation, learning, and evaluation nodes (they're loaded from parentPositions, not offsets)
                updated = updated.map(node => {
                  // Skip sub-agents, validation, learning, and evaluation nodes - they're loaded from parentPositions, not offsets
                  if (node.id.includes('-sub-agent-') || 
                      node.type === 'validation' || 
                      node.type === 'learning' || 
                      node.type === 'evaluation' ||
                      node.type === 'workflow-artifact') {
                    return node
                  }
                  
                  const savedOffset = offsetsToUse.get(node.id)
                  if (savedOffset) {
                    const parentNode = updated.find(n => n.id === savedOffset.parentId)
                    // Only apply if parent is a top-level parent (not a sub-agent)
                    if (parentNode && !parentNode.id.includes('-sub-agent-')) {
                      return {
                        ...node,
                        position: {
                          x: parentNode.position.x + savedOffset.dx,
                          y: parentNode.position.y + savedOffset.dy
                        }
                      }
                    }
                  }
                  return node
                })
                
                // Second pass: Apply offsets for nodes whose parent is a sub-agent (learning/validation nodes)
                updated = updated.map(node => {
                  const savedOffset = offsetsToUse.get(node.id)
                  if (savedOffset) {
                    const parentNode = updated.find(n => n.id === savedOffset.parentId)
                    // Only apply if parent is a sub-agent
                    if (parentNode && parentNode.id.includes('-sub-agent-')) {
                      return {
                        ...node,
                        position: {
                          x: parentNode.position.x + savedOffset.dx,
                          y: parentNode.position.y + savedOffset.dy
                        }
                      }
                    }
                  }
                  return node
                })
              } else {
                // Fallback: calculate offsets from original auto-layout (for old saved layouts)
                updated = updated.map(node => {
                  const parentId = childToParentRef.current.get(node.id)
                  if (parentId) {
                    const parentNode = updated.find(n => n.id === parentId)
                    const originalParentNode = nds.find(n => n.id === parentId)
                    const originalNode = nds.find(n => n.id === node.id)
                    
                    if (parentNode && originalParentNode && originalNode) {
                      const originalOffset = {
                        dx: originalNode.position.x - originalParentNode.position.x,
                        dy: originalNode.position.y - originalParentNode.position.y
                      }
                      
                      return {
                        ...node,
                        position: {
                          x: parentNode.position.x + originalOffset.dx,
                          y: parentNode.position.y + originalOffset.dy
                        }
                      }
                    }
                  }
                  return node
                })
              }
              
              // Rebuild groups with final positions to ensure offsets are correct for future moves
              buildNodeGroups(updated)
              
              // CRITICAL: Force header nodes to correct positions
              updated = updated.map(node => {
                if (headerNodePositions.has(node.id)) {
                  return { ...node, position: headerNodePositions.get(node.id)! }
                }
                return node
              })
              
              // Clear current positions after use (they've been applied)
              if (positionsToUse === currentPositionsRef.current) {
                currentPositionsRef.current.clear()
                currentOffsetsRef.current.clear()
              }
              
              return updated
            })
          } else {
            // No saved layout - force header nodes immediately
            setNodes((nds) => {
              return nds.map(node => {
                if (headerNodePositions.has(node.id)) {
                  return { ...node, position: headerNodePositions.get(node.id)! }
                }
                return node
              })
            })
          }
        }).catch(err => {
          console.error('[WorkflowCanvas] Failed to load saved layout:', err)
          // If saved layout fails, try to use current positions
          if (currentPositionsRef.current.size > 0) {
            setNodes((nds) => {
              let updated = nds.map(node => {
                const savedPos = currentPositionsRef.current.get(node.id)
                
                // Header nodes are skipped from restoration (will be forced to correct positions later)

                // Apply saved position unless it's a header node (start, variables)
                if (savedPos && node.id !== 'start' && node.id !== 'variables') {
                  return { ...node, position: savedPos }
                }
                return node
              })
              buildNodeGroups(updated)
              
              // CRITICAL: Force header nodes to correct positions
              updated = updated.map(node => {
                if (headerNodePositions.has(node.id)) {
                  return { ...node, position: headerNodePositions.get(node.id)! }
                }
                return node
              })
              
              // Clear current positions after use
              currentPositionsRef.current.clear()
              currentOffsetsRef.current.clear()
              return updated
            })
          }
        })
      } else {
        // No saved layout or layout direction changed - ensure header nodes have correct positions from usePlanToFlow
        const headerNodePositions = new Map<string, { x: number; y: number }>()
        initialNodes.forEach(node => {
          if (node.id === 'start' || node.id === 'variables') {
            headerNodePositions.set(node.id, { x: node.position.x, y: node.position.y })
          }
        })
        
        if (headerNodePositions.size > 0) {
          setNodes((nds) => {
            return nds.map(node => {
              if (headerNodePositions.has(node.id)) {
                return { ...node, position: headerNodePositions.get(node.id)! }
              }
              return node
            })
          })
          
          // Also use updateNode API to force positions
          headerNodePositions.forEach((pos, nodeId) => {
            updateNode(nodeId, { position: pos })
          })
        }
      }
      
      hasInitializedView.current = false
      
      prevNodesRef.current = initialNodes
      
      // After nodes are updated, check if we need to focus on a changed step (from backend updates)
      // Use setTimeout to ensure nodes are fully rendered in React Flow
      if (pendingFocusStepIdRef.current) {
        const stepIdToFocus = pendingFocusStepIdRef.current
        // Store focusNode in a local variable to avoid dependency issues
        const focusNodeFn = focusNode
        setTimeout(() => {
          // Find the node for this step ID - prioritize step.id over node.id for accurate matching
          // For orchestration steps: nodeData.step.id is the wrapper step ID (e.g., "orchestrate-hdfc-bank-login")
          // For conditional steps: nodeData.step.id is the wrapper step ID
          // For branch steps: nodeData.step.id is the actual step ID from plan.json (not the constructed node ID)
          const nodeToFocus = initialNodes.find(n => {
            if (n.type === 'step' || n.type === 'conditional') {
              const nodeData = n.data as StepNodeData | ConditionalNodeData
              // Match by step.id first (this is the actual step ID from plan.json - the wrapper step ID for orchestration/conditional)
              // This matches what the backend sends in changed_step_ids
              const stepId = nodeData?.step?.id
              if (stepId === stepIdToFocus) {
                return true
              }
              // Fallback: match by node.id only if step.id doesn't exist (shouldn't happen for valid steps)
              if (!stepId && n.id === stepIdToFocus) {
                return true
              }
              return false
            }
            return false
          })
          
          if (nodeToFocus) {
            // Focus on the changed step (position viewport, but don't open sidebar)
            focusNodeFn(nodeToFocus.id, { topPadding: 150, delay: 0 })
            const nodeData = nodeToFocus.data as StepNodeData | ConditionalNodeData
            console.log('[WorkflowCanvas] Auto-focused on step that was changed by backend:', {
              stepId: stepIdToFocus,
              nodeId: nodeToFocus.id,
              stepTitle: nodeData?.step?.title,
              matchedBy: nodeData?.step?.id === stepIdToFocus ? 'step.id' : 'node.id'
            })
          } else {
            console.warn('[WorkflowCanvas] Could not find node for changed step ID:', stepIdToFocus, {
              availableNodes: initialNodes
                .filter(n => n.type === 'step' || n.type === 'conditional')
                .map(n => {
                  const nodeData = n.data as StepNodeData | ConditionalNodeData
                  return {
                    nodeId: n.id,
                    stepId: nodeData?.step?.id,
                    stepTitle: nodeData?.step?.title
                  }
                })
            })
          }
          
          // Clear the pending focus
          pendingFocusStepIdRef.current = null
        }, 200) // Small delay to ensure React Flow has rendered the nodes
      }
    }
    
    if (nodesChanged) {
      console.timeEnd(`[WorkflowCanvas] setNodes-${presetQueryId?.slice(0,8)}`)
    }

    if (edgesChanged) {
      console.log(`%c[WorkflowCanvas] setEdges: ${initialEdges.length} edges (preset: ${presetQueryId?.slice(0,8)})`, 'color: #4CAF50')
      setEdges(initialEdges)
      prevEdgesRef.current = initialEdges
    }

  }, [initialNodes, initialEdges, setNodes, setEdges, focusNode, buildNodeGroups, loadSavedLayout, layoutDirection, updateNode, presetQueryId, toolbarOnly, canvasViewMode])

  // Fit the full plan on first render so the workflow shape is visible by default.
  React.useEffect(() => {
    if (toolbarOnly) return
    if (!hasInitializedView.current && nodes.length > 0) {
      const fitTimer = window.setTimeout(() => {
        window.requestAnimationFrame(() => {
          Promise.resolve(
            fitView({ padding: 0.18, duration: 350, minZoom: 0.15, maxZoom: 1.1 })
          ).finally(() => {
            viewportStateRef.current = getViewport()
            hasInitializedView.current = true
          })
        })
      }, 220)

      return () => window.clearTimeout(fitTimer)
    }
  }, [nodes, fitView, getViewport, toolbarOnly, canvasViewMode])

  // Track previous stepStatusMap to detect actual changes
  const prevStepStatusMapRef = React.useRef<Map<string, 'pending' | 'running' | 'completed' | 'failed'>>(new Map())

  // Update node status based on maps from events (only when stepStatusMap actually changes)
  React.useEffect(() => {
    if (toolbarOnly) return // Skip when canvas is hidden

    // Check if stepStatusMap actually changed by comparing entries
    const hasChanged = stepStatusMap.size !== prevStepStatusMapRef.current.size ||
      Array.from(stepStatusMap.entries()).some(([stepId, status]) => 
        prevStepStatusMapRef.current.get(stepId) !== status
      )

    if (!hasChanged) {
      return // No actual changes, skip update
    }

    setNodes(nds => {
      let hasUpdates = false
      const updatedNodes = nds.map(node => {
        // Only update status for step-type nodes (step, conditional, loop)
        // Validation and learning nodes have different status types
        if (node.type === 'step' || node.type === 'conditional') {
          const nodeData = node.data as StepNodeData | ConditionalNodeData
          const stepId = nodeData?.step?.id || node.id
          const stepStatus = stepStatusMap.get(stepId)
          const currentStatus = nodeData?.status

          // Only update if status actually changed
          if (stepStatus && stepStatus !== currentStatus) {
            hasUpdates = true
            if (node.type === 'step') {
              return {
                ...node,
                data: {
                  ...node.data,
                  status: stepStatus
                } as StepNodeData
              } as WorkflowNode
            } else if (node.type === 'conditional') {
              return {
                ...node,
                data: {
                  ...node.data,
                  status: stepStatus
                } as ConditionalNodeData
              } as WorkflowNode
            }
          }
        }
        return node
      })

      // Only return new array if there were actual updates
      return hasUpdates ? updatedNodes : nds
    })
    
    // Update previous status map (for tracking changes)
    prevStepStatusMapRef.current = new Map(stepStatusMap)
  }, [stepStatusMap, setNodes, toolbarOnly, canvasViewMode])


  useEffect(() => {
    if (!selectedFlowNode) return

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setSelectedFlowNode(null)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [selectedFlowNode])

  const onNodeClick = useCallback((_: React.MouseEvent, node: WorkflowNode) => {
    if (node.type === 'variables') {
      setShowVariablesSidebar(true)
      setSelectedFlowNode(null)
      return
    }
    setSelectedFlowNode(current => current?.id === node.id ? null : node)
  }, [])
  const onPaneClick = useCallback(() => {
    setSelectedFlowNode(null)
  }, [])

  // Handle start phase with execution options (for toolbar)
  const handleStartPhase = useCallback((phaseId: string, executionOptions?: ExecutionOptions) => {
    if (onStartPhase) {
      onStartPhase(phaseId, executionOptions)
    }
  }, [onStartPhase])

  // Unified loading state - wait for ALL data before showing canvas
  // This ensures consistent state: plan, step_config, run folders, variables, phases, progress
  const isFullyLoaded = !loading && !isLoadingWorkspaceState
  const loadingMessages = []
  if (loading) loadingMessages.push('plan & step config')
  if (isLoadingWorkspaceState) loadingMessages.push('workspace state')

  if (!isFullyLoaded) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${paneClassName} ${className}`}>
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-gray-400 dark:border-gray-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-sm text-gray-500 dark:text-gray-400">
            Loading {loadingMessages.join(' & ')}...
          </span>
          <span className="text-xs text-gray-400 dark:text-gray-500">
            Please wait while we load everything
          </span>
        </div>
      </div>
    )
  }

  // Error state - show errors from plan loading or workspace state loading
  // Treat "plan.json not found" as "no plan" rather than an error (new workflows don't have plan.json yet)
  const isPlanNotFoundError = error && /not found|does not exist|planning must be run first/i.test(error)
  const effectiveError = isPlanNotFoundError ? null : error
  const hasError = effectiveError || workspaceStateError

  if (hasError) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${paneClassName} ${className}`}>
        <div className="flex flex-col items-center gap-3 text-center max-w-md">
          <div className="w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <span className="text-2xl">⚠️</span>
          </div>
          <div className="flex flex-col gap-2">
            {effectiveError && (
              <span className="text-sm text-red-600 dark:text-red-400">
                <strong>Plan error:</strong> {effectiveError}
              </span>
            )}
            {workspaceStateError && (
              <div className="flex flex-col gap-2">
                <span className="text-sm text-red-600 dark:text-red-400">
                  <strong>Workspace error:</strong> {workspaceStateError}
                </span>
                {isRetryingWorkspaceState && (
                  <div className="flex items-center gap-2 text-sm text-blue-600 dark:text-blue-400">
                    <div className="w-4 h-4 border-2 border-blue-600 dark:border-blue-400 border-t-transparent rounded-full animate-spin" />
                    <span>
                      Retrying in {workspaceStateRetryCountdown !== null ? `${workspaceStateRetryCountdown} second${workspaceStateRetryCountdown !== 1 ? 's' : ''}...` : '5 seconds...'}
                    </span>
                  </div>
                )}
              </div>
            )}
          </div>
          <button
            onClick={() => {
              loadPlanRefresh()
              refreshWorkspaceState()
            }}
            className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
            disabled={isRetryingWorkspaceState}
          >
            {isRetryingWorkspaceState ? 'Retrying...' : 'Retry Loading'}
          </button>
        </div>
      </div>
    )
  }

  // No plan state
  const hasPlan = !!(plan && plan.steps && plan.steps.length > 0)
  if (!hasPlan) {
    return (
      <div className={`flex flex-col h-full bg-gray-50 dark:bg-gray-900 ${className} ${sharedToolbar && showChatArea ? 'md:contents' : ''}`}>
        <div className={sharedToolbar && showChatArea ? 'md:col-span-2 md:row-start-1' : ''}>
          <WorkflowToolbar
            status={status}
            hasPlan={false}
            currentPhase={currentPhase}
            workspacePath={workspacePath}
            presetQueryId={presetQueryId}
            runFolders={runFoldersForToolbar}
            variablesManifest={variablesManifest}
            isLoadingWorkspaceState={isLoadingWorkspaceState}
            onStartPhase={handleStartPhase}
            onCreatePlan={onCreatePlan || (() => {})}
            showChatArea={showChatArea}
            onToggleChatArea={onToggleChatArea}
            onRefresh={handleRefresh}
          />
        </div>
        <div className={`${sharedToolbar && showChatArea ? 'flex-1 md:col-start-2 md:row-start-2' : 'flex-1'} ${paneClassName} flex min-h-0 items-center justify-center`}>
          <div className="flex flex-col items-center gap-4 text-center">
            <div className="w-16 h-16 rounded-full bg-gray-100 dark:bg-gray-800 flex items-center justify-center">
              <span className="text-3xl">📋</span>
            </div>
            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-gray-100">
                No Plan Yet
              </h3>
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                Create a plan to visualize your workflow
              </p>
            </div>
            {onCreatePlan && (
              <button
                onClick={onCreatePlan}
                className="px-6 py-2.5 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 font-medium"
              >
                Build Plan
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`flex flex-col h-full ${className} ${sharedToolbar && showChatArea ? 'md:contents' : ''}`} ref={reactFlowWrapper}>
      <div className={sharedToolbar && showChatArea ? 'md:col-span-2 md:row-start-1' : ''}>
        <WorkflowToolbar
          status={status}
          hasPlan={true}
          plan={plan || undefined}
          currentPhase={currentPhase}
          workspacePath={workspacePath}
          presetQueryId={presetQueryId}
          runFolders={runFoldersForToolbar}
          variablesManifest={variablesManifest}
          isLoadingWorkspaceState={isLoadingWorkspaceState}
          onStartPhase={handleStartPhase}
          onCreatePlan={onCreatePlan || (() => {})}
          showChatArea={showChatArea}
          onToggleChatArea={onToggleChatArea}
          onRefresh={handleRefresh}
        />
      </div>

      <div className={`${sharedToolbar && showChatArea ? 'flex-1 md:col-start-2 md:row-start-2' : 'flex-1'} ${paneClassName} min-h-0`}>
        {/* Canvas area — skip when toolbarOnly to avoid rendering 1000+ SVG nodes */}
        {toolbarOnly ? null : canvasViewMode === 'report' ? (
          <div className="h-full min-h-0 relative">
            {workspacePath && <ReportView workspacePath={workspacePath} mobilePreview={showChatArea} />}
          </div>
        ) : <div className="h-full min-h-0 relative flex">
          <div className={`flex-1 min-h-0 h-full transition-all duration-300 ${showVariablesSidebar ? 'mr-[450px]' : ''}`}>
        <ReactFlow
          className="w-full h-full bg-gray-50 dark:bg-gray-900"
          style={{ width: '100%', height: '100%' }}
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
          onNodeDragStop={onNodeDragStop}
          panOnDrag
          panOnScroll
          nodesDraggable
          nodesConnectable={false}
          nodesFocusable={false}
          elementsSelectable={false}
          edgesFocusable={false}
          onlyRenderVisibleElements={false}
          onViewportChange={onViewportChange}
          nodeTypes={nodeTypes}
          fitView={false}
          fitViewOptions={{ padding: 0.18, minZoom: 0.15, maxZoom: 1.1 }}
          minZoom={0.1}
          maxZoom={2}
          defaultViewport={{ x: 100, y: 0, zoom: 0.9 }}
          attributionPosition="bottom-right"
        >
          <Background 
            variant={BackgroundVariant.Dots} 
            gap={20} 
            size={1} 
            color="#e5e7eb"
            className="dark:!bg-gray-900"
          />
        </ReactFlow>

        {/* Batch Progress Header - Above Legend */}
        <BatchProgressHeader position="canvas" />

        {/* Step Legend - Bottom Left */}
        {plan && plan.steps && plan.steps.length > 0 && (
          <StepLegend
            plan={plan}
            nodes={nodes}
            selectedNodeId={null}
            onStepClick={handleNavigateToStep}
            workspacePath={workspacePath}
            currentStepId={currentStepId}
          />
        )}

        <FloatingWorkflowViewControls
          viewLabel="flow"
          showPreviewControls={showChatArea}
          onRefresh={() => { void handleRefresh() }}
          onExportImage={(format) => { void handleExportImage(format) }}
          isExportingImage={isExportingImage}
        />
        </div>

        {selectedFlowNode && (
          <ReadOnlyStepDetailPanel
            node={selectedFlowNode}
            onClose={() => setSelectedFlowNode(null)}
          />
        )}

        {/* Variables Sidebar */}
        {showVariablesSidebar && (
          <VariablesSidebar
            workspacePath={workspacePath}
            onClose={() => setShowVariablesSidebar(false)}
            onUpdate={handleVariablesUpdate}
            showChatArea={showChatArea}
          />
        )}
      </div>}
      </div>

    </div>
  )
})

// Add display name for debugging
WorkflowCanvasInner.displayName = 'WorkflowCanvasInner'

// Wrap with ReactFlowProvider for hooks to work. Memoizing this boundary keeps
// chat/tool stream renders from repainting the canvas when workflow props are unchanged.
export const WorkflowCanvasWithProvider = React.memo(forwardRef<WorkflowCanvasRef, WorkflowCanvasProps>((props, ref) => {
  return (
    <ReactFlowProvider>
      <WorkflowCanvasInner {...props} ref={ref} />
    </ReactFlowProvider>
  )
}))

WorkflowCanvasWithProvider.displayName = 'WorkflowCanvasWithProvider'

export const WorkflowCanvas = WorkflowCanvasWithProvider

export default WorkflowCanvasWithProvider
