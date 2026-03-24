import React, { useEffect, useRef, useState, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { prism } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'
import { useAppStore } from '../../stores/useAppStore'
import { useModeStore } from '../../stores/useModeStore'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { workspaceApi, agentApi, getApiBaseUrl } from '../../services/api'
import mermaid from 'mermaid'

interface MarkdownRendererProps {
  content: string
  className?: string
  maxHeight?: string
  showScrollbar?: boolean
  disablePathLinking?: boolean
}

interface ChartDatum {
  label: string
  value: number
  color?: string
}

interface ChartSpec {
  type: 'bar' | 'line'
  title?: string
  description?: string
  xLabel?: string
  yLabel?: string
  data: ChartDatum[]
}

const ToolDefinition: React.FC<{ content: string }> = ({ content }) => {
  const lines = content.split('\n').filter(line => line.trim() !== '')
  const data: Record<string, string> = {}
  
  lines.forEach(line => {
    const colonIndex = line.indexOf(':')
    if (colonIndex !== -1) {
      const key = line.slice(0, colonIndex).trim()
      let value = line.slice(colonIndex + 1).trim()
      // Remove quotes if present
      if (value.startsWith('"') && value.endsWith('"')) {
        value = value.slice(1, -1)
      }
      data[key] = value
    }
  })

  return (
    <div className="bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg p-4 my-4 shadow-sm">
      <div className="font-semibold text-gray-900 dark:text-gray-100 mb-2 border-b border-gray-200 dark:border-gray-700 pb-2 flex items-center gap-2">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="text-blue-500">
          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
        </svg>
        Tool Definition
      </div>
      <div className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
        {Object.entries(data).map(([key, value]) => (
          <React.Fragment key={key}>
            <div className="font-medium text-gray-700 dark:text-gray-300 capitalize text-right self-start whitespace-nowrap">
              {key.replace(/-/g, ' ')}:
            </div>
            <div className="text-gray-900 dark:text-gray-100 font-mono text-xs bg-white dark:bg-gray-900 px-2 py-0.5 rounded border border-gray-200 dark:border-gray-700 break-all">
              {value || <span className="text-gray-400 italic">None</span>}
            </div>
          </React.Fragment>
        ))}
      </div>
    </div>
  )
}

const CHART_COLORS = ['#3b82f6', '#14b8a6', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4']

const ChartBlock: React.FC<{ content: string }> = ({ content }) => {
  const parsed = React.useMemo(() => {
    try {
      const raw = JSON.parse(content) as Record<string, unknown>
      const type = raw.type
      if (type !== 'bar' && type !== 'line') {
        return {
          error: 'Chart blocks currently support only "bar" and "line" types.',
        }
      }

      const rawData = Array.isArray(raw.data) ? raw.data : []
      const data: ChartDatum[] = []

      rawData.forEach((item, index) => {
        if (!item || typeof item !== 'object') {
          return
        }
        const row = item as Record<string, unknown>
        const numericValue = typeof row.value === 'number' ? row.value : Number(row.value)
        if (!Number.isFinite(numericValue) || numericValue < 0) {
          return
        }
        data.push({
          label: typeof row.label === 'string' && row.label.trim() !== '' ? row.label : `Item ${index + 1}`,
          value: numericValue,
          color: typeof row.color === 'string' ? row.color : undefined,
        })
      })

      if (data.length === 0) {
        return {
          error: 'Chart blocks need a non-empty "data" array with { "label", "value" } items.',
        }
      }

      return {
        spec: {
          type,
          title: typeof raw.title === 'string' ? raw.title : undefined,
          description: typeof raw.description === 'string' ? raw.description : undefined,
          xLabel: typeof raw.xLabel === 'string' ? raw.xLabel : undefined,
          yLabel: typeof raw.yLabel === 'string' ? raw.yLabel : undefined,
          data,
        } satisfies ChartSpec,
      }
    } catch (error) {
      return {
        error: error instanceof Error ? error.message : 'Invalid chart JSON.',
      }
    }
  }, [content])

  if (parsed.error || !parsed.spec) {
    return (
      <div className="my-4 rounded-lg border border-red-300 bg-red-50 p-4 dark:border-red-700 dark:bg-red-900/20">
        <div className="mb-2 text-xs font-medium text-red-600 dark:text-red-400">Chart block error</div>
        <pre className="whitespace-pre-wrap text-xs text-red-500 dark:text-red-400">{parsed.error ?? 'Invalid chart block.'}</pre>
        <details className="mt-2">
          <summary className="cursor-pointer text-xs text-gray-500">Show source</summary>
          <pre className="mt-1 whitespace-pre-wrap text-xs text-gray-600 dark:text-gray-400">{content}</pre>
        </details>
      </div>
    )
  }

  const spec = parsed.spec
  const chartWidth = 700
  const chartHeight = 320
  const chartTop = 24
  const chartRight = 24
  const chartBottom = 88
  const chartLeft = 56
  const plotWidth = chartWidth - chartLeft - chartRight
  const plotHeight = chartHeight - chartTop - chartBottom
  const maxValue = Math.max(...spec.data.map(item => item.value), 0)
  const niceMax = maxValue <= 0 ? 1 : Math.ceil(maxValue / 5) * 5
  const gridValues = Array.from({ length: 5 }, (_, index) => (niceMax / 4) * index)
  const shouldRotateLabels = spec.data.some(item => item.label.length > 10) || spec.data.length > 5
  const resolvedData: Array<ChartDatum & { color: string }> = spec.data.map((item, index) => ({
    ...item,
    color: item.color || CHART_COLORS[index % CHART_COLORS.length],
  }))

  let chartBody: React.ReactNode

  if (spec.type === 'bar') {
    const slotWidth = plotWidth / resolvedData.length
    const barWidth = Math.max(18, Math.min(52, slotWidth * 0.62))

    chartBody = (
      <>
        {gridValues.map(value => {
          const y = chartTop + plotHeight - (value / niceMax) * plotHeight
          return (
            <g key={`grid-${value}`}>
              <line x1={chartLeft} y1={y} x2={chartLeft + plotWidth} y2={y} stroke="currentColor" strokeOpacity="0.12" />
              <text x={chartLeft - 8} y={y + 4} textAnchor="end" className="fill-gray-500 text-[11px] dark:fill-gray-400">
                {value.toLocaleString()}
              </text>
            </g>
          )
        })}
        {resolvedData.map((item, index) => {
          const height = (item.value / niceMax) * plotHeight
          const x = chartLeft + slotWidth * index + (slotWidth - barWidth) / 2
          const y = chartTop + plotHeight - height
          const labelX = chartLeft + slotWidth * index + slotWidth / 2
          return (
            <g key={`${item.label}-${index}`}>
              <rect x={x} y={y} width={barWidth} height={Math.max(height, 2)} rx={8} fill={item.color} fillOpacity="0.92" />
              <text x={labelX} y={y - 8} textAnchor="middle" className="fill-gray-600 text-[11px] dark:fill-gray-300">
                {item.value.toLocaleString()}
              </text>
              <text
                x={labelX}
                y={chartTop + plotHeight + (shouldRotateLabels ? 18 : 24)}
                textAnchor={shouldRotateLabels ? 'end' : 'middle'}
                transform={shouldRotateLabels ? `rotate(-32 ${labelX} ${chartTop + plotHeight + 18})` : undefined}
                className="fill-gray-600 text-[11px] dark:fill-gray-300"
              >
                {item.label}
              </text>
            </g>
          )
        })}
      </>
    )
  } else {
    const slotWidth = resolvedData.length > 1 ? plotWidth / (resolvedData.length - 1) : 0
    const points = resolvedData.map((item, index) => {
      const x = chartLeft + (resolvedData.length === 1 ? plotWidth / 2 : slotWidth * index)
      const y = chartTop + plotHeight - (item.value / niceMax) * plotHeight
      return { ...item, x, y }
    })

    chartBody = (
      <>
        {gridValues.map(value => {
          const y = chartTop + plotHeight - (value / niceMax) * plotHeight
          return (
            <g key={`grid-${value}`}>
              <line x1={chartLeft} y1={y} x2={chartLeft + plotWidth} y2={y} stroke="currentColor" strokeOpacity="0.12" />
              <text x={chartLeft - 8} y={y + 4} textAnchor="end" className="fill-gray-500 text-[11px] dark:fill-gray-400">
                {value.toLocaleString()}
              </text>
            </g>
          )
        })}
        <polyline
          fill="none"
          stroke="#3b82f6"
          strokeWidth="3"
          points={points.map(point => `${point.x},${point.y}`).join(' ')}
          strokeLinecap="round"
          strokeLinejoin="round"
        />
        {points.map((point, index) => (
          <g key={`${point.label}-${index}`}>
            <circle cx={point.x} cy={point.y} r="5" fill={point.color} />
            <text x={point.x} y={point.y - 10} textAnchor="middle" className="fill-gray-600 text-[11px] dark:fill-gray-300">
              {point.value.toLocaleString()}
            </text>
            <text
              x={point.x}
              y={chartTop + plotHeight + (shouldRotateLabels ? 18 : 24)}
              textAnchor={shouldRotateLabels ? 'end' : 'middle'}
              transform={shouldRotateLabels ? `rotate(-32 ${point.x} ${chartTop + plotHeight + 18})` : undefined}
              className="fill-gray-600 text-[11px] dark:fill-gray-300"
            >
              {point.label}
            </text>
          </g>
        ))}
      </>
    )
  }

  return (
    <div className="my-4 overflow-hidden rounded-xl border border-gray-200 bg-gray-50/80 shadow-sm dark:border-gray-700 dark:bg-gray-900/80">
      <div className="border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <div className="text-sm font-semibold text-gray-900 dark:text-gray-100">
          {spec.title || (spec.type === 'bar' ? 'Bar chart' : 'Line chart')}
        </div>
        {spec.description ? (
          <div className="mt-1 text-xs leading-5 text-gray-600 dark:text-gray-400">{spec.description}</div>
        ) : null}
      </div>
      <div className="overflow-x-auto px-3 py-3">
        <svg viewBox={`0 0 ${chartWidth} ${chartHeight}`} className="h-auto min-w-[640px] text-gray-700 dark:text-gray-200">
          <line x1={chartLeft} y1={chartTop} x2={chartLeft} y2={chartTop + plotHeight} stroke="currentColor" strokeOpacity="0.24" />
          <line x1={chartLeft} y1={chartTop + plotHeight} x2={chartLeft + plotWidth} y2={chartTop + plotHeight} stroke="currentColor" strokeOpacity="0.24" />
          {chartBody}
        </svg>
      </div>
      {(spec.xLabel || spec.yLabel) ? (
        <div className="border-t border-gray-200 px-4 py-2 text-xs text-gray-500 dark:border-gray-700 dark:text-gray-400">
          {spec.xLabel ? <span className="mr-4">X: {spec.xLabel}</span> : null}
          {spec.yLabel ? <span>Y: {spec.yLabel}</span> : null}
        </div>
      ) : null}
    </div>
  )
}

// Mermaid initialization
let mermaidInitialized = false
const initMermaid = () => {
  if (!mermaidInitialized) {
    mermaid.initialize({
      startOnLoad: false,
      theme: document.documentElement.classList.contains('dark') ||
             document.documentElement.classList.contains('dark-plus') ? 'dark' : 'default',
      securityLevel: 'loose',
    })
    mermaidInitialized = true
  }
}

let mermaidCounter = 0

export const MermaidDiagram: React.FC<{ content: string }> = ({ content }) => {
  const containerRef = useRef<HTMLDivElement>(null)
  const [svg, setSvg] = useState<string>('')
  const [error, setError] = useState<string>('')
  const idRef = useRef(`mermaid-${Date.now()}-${mermaidCounter++}`)

  const renderDiagram = useCallback(async () => {
    try {
      initMermaid()
      // Re-init theme on each render to pick up dark mode changes
      const isDark = document.documentElement.classList.contains('dark') ||
                     document.documentElement.classList.contains('dark-plus')
      mermaid.initialize({
        startOnLoad: false,
        theme: isDark ? 'dark' : 'default',
        securityLevel: 'loose',
      })
      const { svg: renderedSvg } = await mermaid.render(idRef.current, content)
      setSvg(renderedSvg)
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to render mermaid diagram')
      setSvg('')
    }
  }, [content])

  useEffect(() => {
    renderDiagram()
  }, [renderDiagram])

  if (error) {
    return (
      <div className="my-4 p-4 border border-red-300 dark:border-red-700 rounded-lg bg-red-50 dark:bg-red-900/20">
        <div className="text-xs text-red-600 dark:text-red-400 mb-2 font-medium">Mermaid diagram error</div>
        <pre className="text-xs text-red-500 dark:text-red-400 whitespace-pre-wrap">{error}</pre>
        <details className="mt-2">
          <summary className="text-xs text-gray-500 cursor-pointer">Show source</summary>
          <pre className="mt-1 text-xs text-gray-600 dark:text-gray-400 whitespace-pre-wrap">{content}</pre>
        </details>
      </div>
    )
  }

  if (!svg) {
    return (
      <div className="my-4 p-4 border border-gray-200 dark:border-gray-700 rounded-lg bg-gray-50 dark:bg-gray-800 text-center text-sm text-gray-500">
        Rendering diagram...
      </div>
    )
  }

  return (
    <div
      ref={containerRef}
      className="my-4 p-4 border border-gray-200 dark:border-gray-700 rounded-lg bg-white dark:bg-gray-900 overflow-x-auto"
      dangerouslySetInnerHTML={{ __html: svg }}
    />
  )
}

export const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({
  content,
  className = "",
  maxHeight = "none",
  showScrollbar = false,
  disablePathLinking = false
}) => {
  const containerClasses = `prose prose-sm max-w-none dark:prose-invert ${className}`
  const scrollClasses = showScrollbar ? 'overflow-y-auto overflow-x-auto min-w-0' : ""
  const containerStyle = showScrollbar && maxHeight !== 'none'
    ? { maxHeight }
    : undefined

  const highlightFile = useWorkspaceStore(state => state.highlightFile)
  const expandFoldersForFile = useWorkspaceStore(state => state.expandFoldersForFile)
  const setSelectedFile = useWorkspaceStore(state => state.setSelectedFile)
  const setShowFileContent = useWorkspaceStore(state => state.setShowFileContent)
  const setFileContent = useWorkspaceStore(state => state.setFileContent)
  const setLoadingFileContent = useWorkspaceStore(state => state.setLoadingFileContent)
  const setBinaryFileData = useWorkspaceStore(state => state.setBinaryFileData)
  const setWorkspaceMinimized = useAppStore(state => state.setWorkspaceMinimized)

  // Standard file opening logic — mirrors Workspace.tsx handleFileClick
  const handleWorkspaceLink = async (filepath: string) => {
    try {
      // In workflow mode, paths like "knowledgebase/..." are adjusted (prefix stripped).
      // We need to prepend the workflow folder path for the API call.
      const standardPrefixes = ['Chats/', 'Downloads/', 'Plans/', 'skills/', 'Workflow/']
      const isStandardPath = standardPrefixes.some(p => filepath.startsWith(p))
      let resolvedPath = filepath

      if (!isStandardPath) {
        const modeCategory = useModeStore.getState().selectedModeCategory
        if (modeCategory === 'workflow') {
          const activePreset = useGlobalPresetStore.getState().getActivePreset('workflow')
          const folderFilepath = activePreset?.selectedFolder?.filepath
          if (folderFilepath) {
            const parts = folderFilepath.split('/').filter(Boolean)
            const lastPart = parts[parts.length - 1]
            const isFile = lastPart?.includes('.')
            const workflowFolderPath = isFile && parts.length > 1 ? parts.slice(0, -1).join('/') : parts.join('/')
            resolvedPath = `${workflowFolderPath}/${filepath}`
          }
        }
      }

      // Auto-resolve folder paths to their default file (e.g. Plans/foo -> Plans/foo/plan.md)
      if (resolvedPath.startsWith('Plans/') && !resolvedPath.includes('.')) {
        resolvedPath = resolvedPath + '/plan.md'
      }

      // displayPath is what the workspace tree uses (adjusted path without workflow prefix)
      // resolvedPath is the full path for API calls
      const displayPath = filepath.startsWith('Plans/') && !filepath.includes('.') ? filepath + '/plan.md' : filepath

      const fileName = resolvedPath.split('/').pop() || resolvedPath
      const ext = fileName.split('.').pop()?.toLowerCase() || ''

      // Check if binary viewable file (xlsx, docx, pdf)
      const isViewableBinary = ['xls', 'xlsx', 'docx', 'pdf'].includes(ext)
      // Check if image file (workspace API returns base64 data URL for these)
      const isImage = ['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext)

      // 1. Ensure workspace is visible
      setWorkspaceMinimized(false)

      // 2. Expand folders and highlight in explorer (use display path for tree navigation)
      expandFoldersForFile(displayPath)
      highlightFile(displayPath)

      // 3. Select file and start loading content (use resolvedPath for API)
      setSelectedFile({ name: fileName, path: resolvedPath })
      setLoadingFileContent(true)

      if (isViewableBinary) {
        const response = await workspaceApi.get(
          `/api/documents/${encodeURIComponent(resolvedPath)}`,
          { params: { download: 'true' }, responseType: 'arraybuffer' }
        )
        setBinaryFileData(response.data as ArrayBuffer)
        setFileContent('')
        setShowFileContent(true)
        setLoadingFileContent(false)
        return
      }

      // Text or image file — workspace API returns base64 data URL for images
      setBinaryFileData(null)
      const response = await agentApi.getPlannerFileContent(resolvedPath)
      if (response.success && response.data) {
        // Guard against undefined/null content (matches Workspace.tsx logic)
        if (response.data.content === undefined || response.data.content === null) {
          setFileContent('')
          setShowFileContent(true)
          setLoadingFileContent(false)
          return
        }

        let content = typeof response.data.content === 'string'
          ? response.data.content
          : String(response.data.content)

        // Images arrive as base64 data URLs — no processing needed
        if (!response.data.is_image) {
          content = content.replace(/\\n/g, '\n').replace(/\\t/g, '\t').replace(/\\r/g, '\r')
        }

        setFileContent(content || '')
        setShowFileContent(true)
      }
    } catch (error) {
      console.error('[MarkdownRenderer] Failed to open workspace link:', error)
    } finally {
      setLoadingFileContent(false)
    }
  }

  const processedContent = React.useMemo(() => {
    if (!content) return ""
    
    // 1. Tool definition processing (existing)
    // Replace YAML-like frontmatter/blocks with a custom code block
    // Matches content between --- and --- where the content looks like key-value pairs
    let processed = content.replace(/(^|\n)---\n([a-zA-Z0-9_-]+:[\s\S]+?)\n---(\n|$)/g, '$1\n```tool-definition\n$2\n```\n$3')

    // 2. Auto-link workspace paths (skip when disablePathLinking is true)
    if (disablePathLinking) return processed
    // Regex matches paths starting with specific prefixes: Chats/, Downloads/, Workflow/, Plans/, skills/
    // Optionally matches surrounding backticks to unwrap them
    // CRITICAL FIX: We must handle paths wrapped in backticks (e.g., `Chats/foo.md`) because LLMs often format them as code.
    // The regex captures:
    // Group 1: Optional opening backtick
    // Group 2: The actual path (starting with allowed prefixes)
    // \1: Matches the closing backtick if Group 1 matched (ensuring balanced quotes)
    const pathRegex = /(`?)\b((?:Chats|Downloads|Workflow|Plans|skills|knowledgebase)\/(?:[\w\-./]+(?:[ ]+[\w\-./]+)*\.\w+|[\w\-./]+))\1/g
    
    // Replace with custom link protocol "#workspace/" to avoid sanitization issues
    // CHALLENGE 1: ReactMarkdown sanitizes unknown protocols like "workspace://", stripping the href.
    // SOLUTION: Use "#workspace/" (hash-based) which is always allowed, then intercept it in the <a> tag renderer.
    // CHALLENGE 2: `replace` callback arguments in arrow functions vs `arguments` object.
    // SOLUTION: Use explicit `...args` rest parameter to reliably capture `offset` and `string` regardless of capture groups.
    processed = processed.replace(pathRegex, (match, backtick, path, ...args) => {
      try {
        // args contains [offset, string] because we have 2 capture groups
        let str = args[args.length - 1] as string
        let offset = args[args.length - 2] as number
        
        // Fallback for weird argument passing
        if (typeof str !== 'string') {
           const foundStr = args.find(a => typeof a === 'string')
           if (foundStr) str = foundStr
        }
        if (typeof offset !== 'number') {
           const foundOffset = args.find(a => typeof a === 'number')
           if (foundOffset !== undefined) offset = foundOffset
        }

        if (typeof str !== 'string' || typeof offset !== 'number') {
           console.error('[MarkdownWorkspace] Invalid args in replace callback:', args)
           return match
        }
        
        const before = str.substring(Math.max(0, offset - 1), offset)
        const after = str.substring(offset + match.length, offset + match.length + 1)
        
        // If capture group 1 (backtick) is present, we are unwrapping, so we don't skip.
        // If capture group 1 is empty, we check if we are inside OTHER code/links/formatting.
        if (!backtick) {
           if (before === '`' || before === '[' || before === '(' || before === '*' || after === ']' || after === '`' || after === '*') {
             return match
           }
        }
        
        // Handle trailing punctuation and spaces from the PATH (capture group 2)
        let cleanPath = path
        let suffix = ''
        
        // Trim trailing spaces, dots, commas, semi-colons
        const trimmed = cleanPath.replace(/[.,; ]+$/, '')
        if (trimmed !== cleanPath) {
           suffix = cleanPath.substring(trimmed.length)
           cleanPath = trimmed
        }
        
        // Use hash-based link to avoid protocol sanitization issues
        const result = `[${cleanPath}](#workspace/${encodeURIComponent(cleanPath)})${suffix}`
        return result
      } catch (err) {
        console.error('[MarkdownWorkspace] Error in regex replace:', err)
        return match
      }
    })
    
    return processed
  }, [content])

  return (
    <div className={`${containerClasses} ${scrollClasses} markdown-content`} style={containerStyle}>
      <style dangerouslySetInnerHTML={{
        __html: `
          .markdown-content ul ul {
            margin-top: 0.25rem;
            margin-bottom: 0.25rem;
          }
          .markdown-content ul ul ul {
            margin-top: 0.125rem;
            margin-bottom: 0.125rem;
          }
          .markdown-content ol ol {
            margin-top: 0.25rem;
            margin-bottom: 0.25rem;
          }
          .markdown-content ol ol ol {
            margin-top: 0.125rem;
            margin-bottom: 0.125rem;
          }
          .markdown-content li p {
            margin: 0;
            display: inline;
          }
          .markdown-content li ul,
          .markdown-content li ol {
            margin-top: 0.25rem;
            margin-bottom: 0.25rem;
          }
          /* Override prose table styles for dark theme */
          .markdown-content.prose table tbody tr {
            background-color: transparent !important;
          }
          .dark .markdown-content.prose table tbody tr,
          .dark-plus .markdown-content.prose table tbody tr {
            background-color: transparent !important;
          }
          .markdown-content.prose table tbody tr:hover {
            background-color: rgb(249 250 251) !important;
          }
          .dark .markdown-content.prose table tbody tr:hover,
          .dark-plus .markdown-content.prose table tbody tr:hover {
            background-color: rgb(31 41 55) !important;
          }
          .markdown-content.prose table tbody tr:focus,
          .markdown-content.prose table tbody tr:active,
          .markdown-content.prose table tbody tr.selected {
            background-color: rgb(249 250 251) !important;
          }
          .dark .markdown-content.prose table tbody tr:focus,
          .dark .markdown-content.prose table tbody tr:active,
          .dark .markdown-content.prose table tbody tr.selected,
          .dark-plus .markdown-content.prose table tbody tr:focus,
          .dark-plus .markdown-content.prose table tbody tr:active,
          .dark-plus .markdown-content.prose table tbody tr.selected {
            background-color: rgb(31 41 55) !important;
          }
          /* Ensure text color is correct in dark mode for hover/selected rows */
          .dark .markdown-content.prose table tbody tr:hover td,
          .dark .markdown-content.prose table tbody tr:focus td,
          .dark .markdown-content.prose table tbody tr:active td,
          .dark .markdown-content.prose table tbody tr.selected td,
          .dark-plus .markdown-content.prose table tbody tr:hover td,
          .dark-plus .markdown-content.prose table tbody tr:focus td,
          .dark-plus .markdown-content.prose table tbody tr:active td,
          .dark-plus .markdown-content.prose table tbody tr.selected td {
            color: rgb(243 244 246) !important;
          }
          /* Override any prose-invert styles that might be setting light backgrounds */
          .dark .markdown-content.prose-invert table tbody tr:hover,
          .dark-plus .markdown-content.prose-invert table tbody tr:hover {
            background-color: rgb(31 41 55) !important;
          }
          .dark .markdown-content.prose-invert table tbody tr:focus,
          .dark .markdown-content.prose-invert table tbody tr:active,
          .dark .markdown-content.prose-invert table tbody tr.selected,
          .dark-plus .markdown-content.prose-invert table tbody tr:focus,
          .dark-plus .markdown-content.prose-invert table tbody tr:active,
          .dark-plus .markdown-content.prose-invert table tbody tr.selected {
            background-color: rgb(31 41 55) !important;
          }
          /* Prevent browser text selection from causing light background issues */
          .dark .markdown-content.prose table tbody tr::selection,
          .dark-plus .markdown-content.prose table tbody tr::selection {
            background-color: rgb(59 130 246) !important;
            color: rgb(255 255 255) !important;
          }
          /* Ensure table cells maintain proper colors */
          .dark .markdown-content.prose table tbody td,
          .dark-plus .markdown-content.prose table tbody td {
            color: rgb(243 244 246) !important;
          }
          .dark .markdown-content.prose-invert table tbody td,
          .dark-plus .markdown-content.prose-invert table tbody td {
            color: rgb(243 244 246) !important;
          }
        `
      }} />
      <ReactMarkdown 
        remarkPlugins={[remarkGfm]}
        components={{
          p: ({ children }) => <p className="mb-2 last:mb-0 text-sm leading-6 text-gray-700 dark:text-gray-300 break-words overflow-wrap-anywhere">{children}</p>,
          h1: ({ children }) => <h1 className="text-2xl font-bold mb-2 mt-4 first:mt-0 text-gray-900 dark:text-gray-100 break-words overflow-wrap-anywhere border-b border-gray-200 dark:border-gray-700 pb-2">{children}</h1>,
          h2: ({ children }) => <h2 className="text-xl font-semibold mb-2 mt-3 first:mt-0 text-gray-900 dark:text-gray-100 break-words overflow-wrap-anywhere">{children}</h2>,
          h3: ({ children }) => <h3 className="text-lg font-semibold mb-1.5 mt-2 first:mt-0 text-gray-900 dark:text-gray-100 break-words overflow-wrap-anywhere">{children}</h3>,
          h4: ({ children }) => <h4 className="text-base font-semibold mb-1.5 mt-2 first:mt-0 text-gray-900 dark:text-gray-100 break-words overflow-wrap-anywhere">{children}</h4>,
          ul: ({ children }) => <ul className="list-disc mb-2 space-y-1 ml-6 pl-2 min-w-0">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal mb-2 space-y-1 ml-6 pl-2 min-w-0">{children}</ol>,
          li: ({ children }) => <li className="text-sm break-words overflow-wrap-anywhere leading-6 text-gray-700 dark:text-gray-300">{children}</li>,
          code: ({ className, children, inline, ...props }: React.HTMLAttributes<HTMLElement> & { inline?: boolean }) => {
            const match = /language-(\w+)/.exec(className || '')
            const isInline = typeof inline === 'boolean' ? inline : false
            
            if (!isInline && match && match[1] === 'tool-definition') {
              return <ToolDefinition content={String(children).replace(/\n$/, '')} />
            }

            if (!isInline && match && match[1] === 'chart') {
              return <ChartBlock content={String(children).replace(/\n$/, '')} />
            }

            if (!isInline && match && match[1] === 'mermaid') {
              return <MermaidDiagram content={String(children).replace(/\n$/, '')} />
            }

            // For inline code, use simple styling
            if (isInline || !match) {
              return (
                <code className="bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded text-xs font-mono break-all overflow-wrap-anywhere text-blue-600 dark:text-blue-400" {...props}>
                  {children}
                </code>
              )
            }
            
            // For code blocks, use SyntaxHighlighter
            const language = match[1]
            const codeString = String(children).replace(/\n$/, '')
            
            // Detect dark mode
            const isDark = document.documentElement.classList.contains('dark') || 
                          document.documentElement.classList.contains('dark-plus')
            
            return (
              <div className="my-4 min-w-0 max-w-full overflow-x-auto rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
                <SyntaxHighlighter
                  // @ts-expect-error: theme type mismatch is safe for SyntaxHighlighter
                  style={isDark ? (oneDark as { [key: string]: React.CSSProperties }) : (prism as { [key: string]: React.CSSProperties })}
                  language={language}
                  PreTag="div"
                  className="!m-0 !p-0"
                  customStyle={{
                    margin: 0,
                    padding: '1rem',
                    borderRadius: '0.5rem',
                    fontSize: '0.875rem',
                    lineHeight: '1.6',
                    overflowX: 'auto',
                    maxWidth: '100%',
                    width: '100%',
                    boxSizing: 'border-box',
                    background: isDark ? '#1f2937' : '#f9fafb',
                  }}
                  codeTagProps={{
                    style: {
                      display: 'block',
                      overflowX: 'auto',
                      maxWidth: '100%',
                    }
                  }}
                  {...props}
                >
                  {codeString}
                </SyntaxHighlighter>
              </div>
            )
          },
          pre: ({ children }) => {
            // Pre tag is handled by the code component above
            // This is a fallback for any pre tags that don't contain code
            return (
              <pre className="bg-gray-100 dark:bg-gray-800 p-2 rounded text-xs font-mono overflow-x-auto break-all min-w-0">
                {children}
              </pre>
            )
          },
          blockquote: ({ children }) => (
            <blockquote className="border-l-4 border-blue-500 dark:border-blue-400 pl-4 py-1.5 my-2 bg-blue-50 dark:bg-blue-900/20 rounded-r text-sm break-words overflow-wrap-anywhere text-gray-700 dark:text-gray-300 italic">
              {children}
            </blockquote>
          ),
          strong: ({ children }) => <strong className="font-semibold break-words overflow-wrap-anywhere text-gray-900 dark:text-gray-100">{children}</strong>,
          em: ({ children }) => <em className="italic break-words overflow-wrap-anywhere">{children}</em>,
          a: ({ href, children }) => {
            const workspacePrefixes = ['Chats/', 'Downloads/', 'Plans/', 'skills/', 'Workflow/', 'knowledgebase/']
            const isWorkspacePath = href && workspacePrefixes.some(p => href.startsWith(p))
            const workspaceFilepath = href?.startsWith('#workspace/')
              ? decodeURIComponent(href.replace('#workspace/', ''))
              : isWorkspacePath ? href : null

            if (workspaceFilepath) {
              return (
                <a
                  href={href}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    handleWorkspaceLink(workspaceFilepath)
                  }}
                  style={{ pointerEvents: 'auto', cursor: 'pointer' }}
                  className="text-purple-600 dark:text-purple-400 hover:text-purple-800 dark:hover:text-purple-300 underline cursor-pointer break-words overflow-wrap-anywhere font-medium transition-colors"
                  title={`Open ${workspaceFilepath} in workspace`}
                >
                  {children}
                </a>
              )
            }
            return (
              <a 
                href={href} 
                target="_blank" 
                rel="noopener noreferrer"
                className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline break-words overflow-wrap-anywhere"
              >
                {children}
              </a>
            )
          },
          img: ({ src, alt }) => {
            const workspacePrefixes = ['Chats/', 'Downloads/', 'Plans/', 'skills/', 'Workflow/', 'knowledgebase/']
            const isWorkspacePath = !!src && workspacePrefixes.some(p => src.startsWith(p))
            const resolvedSrc = isWorkspacePath
              ? `${getApiBaseUrl()}/api/public/file?path=${btoa(src!)}`
              : src
            console.log(`[IMAGE_RENDER] MarkdownRenderer src="${src}" isWorkspace=${isWorkspacePath} resolvedSrc="${resolvedSrc}"`)
            return (
              <img
                src={resolvedSrc}
                alt={alt}
                className="max-w-full h-auto rounded-lg shadow-md my-4 border border-gray-200 dark:border-gray-700"
              />
            )
          },
          hr: () => <hr className="my-6 border-gray-300 dark:border-gray-600" />,
          table: ({ children }) => (
            <div className="overflow-x-auto my-6 min-w-0 rounded-lg border border-gray-200 dark:border-gray-700 shadow-sm">
              <table className="min-w-full border-collapse">
                {children}
              </table>
            </div>
          ),
          thead: ({ children }) => (
            <thead className="bg-gray-50 dark:bg-gray-800 border-b border-gray-200 dark:border-gray-700">
              {children}
            </thead>
          ),
          tbody: ({ children }) => (
            <tbody className="bg-white dark:bg-gray-900 divide-y divide-gray-200 dark:divide-gray-700">
              {children}  
            </tbody>
          ),
          tr: ({ children }) => (
            <tr className="transition-colors [tbody_&:hover]:bg-gray-50 dark:[tbody_&:hover]:bg-gray-800 [tbody_&:focus]:bg-gray-50 dark:[tbody_&:focus]:bg-gray-800 [tbody_&:active]:bg-gray-100 dark:[tbody_&:active]:bg-gray-700">
              {children}
            </tr>
          ),
          th: ({ children }) => (
            <th className="px-4 py-3 text-left text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wider break-words overflow-wrap-anywhere">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="px-4 py-3 text-sm text-gray-900 dark:text-gray-100 break-words overflow-wrap-anywhere [tr:hover_&]:text-gray-900 [tr:hover_&]:dark:text-gray-100 [tr:focus_&]:text-gray-900 [tr:focus_&]:dark:text-gray-100 [tr:active_&]:text-gray-900 [tr:active_&]:dark:text-gray-100">
              {children}
            </td>
          ),
        }}
      >
        {processedContent}
      </ReactMarkdown>
    </div>
  )
}

// Specialized versions for different event types
export const LLMMarkdownRenderer: React.FC<{ content: string; maxHeight?: string }> = ({ content, maxHeight = "600px" }) => (
  <div className={`border border-gray-200 dark:border-gray-700 rounded-md bg-white dark:bg-gray-800 overflow-y-auto overflow-x-hidden min-w-0`} style={{ maxHeight }}>
    <div className="p-3 min-w-0">
      <MarkdownRenderer content={content} />
    </div>
  </div>
)

export const ToolMarkdownRenderer: React.FC<{ content: string; maxHeight?: string }> = ({ content, maxHeight = "400px" }) => (
  <div className={`border border-gray-200 dark:border-gray-700 rounded-md bg-white dark:bg-gray-800 overflow-y-auto overflow-x-hidden min-w-0`} style={{ maxHeight }}>
    <div className="p-3 min-w-0">
      <MarkdownRenderer content={content} />
    </div>
  </div>
)

export const SystemMarkdownRenderer: React.FC<{ content: string; maxHeight?: string }> = ({ content, maxHeight = "256px" }) => (
  <div className={`border border-gray-200 dark:border-gray-700 rounded-md bg-white dark:bg-gray-800 overflow-y-auto overflow-x-hidden min-w-0`} style={{ maxHeight }}>
    <div className="p-3 min-w-0">
      <MarkdownRenderer content={content} />
    </div>
  </div>
)

export const ConversationMarkdownRenderer: React.FC<{ content: string; maxHeight?: string; disablePathLinking?: boolean }> = ({ content, maxHeight = "384px", disablePathLinking }) => (
  <div className={`border border-gray-200 dark:border-gray-700 rounded-md bg-white dark:bg-gray-800 overflow-y-auto overflow-x-hidden min-w-0`} style={{ maxHeight }}>
    <div className="p-3 min-w-0">
      <MarkdownRenderer content={content} disablePathLinking={disablePathLinking} />
    </div>
  </div>
)
