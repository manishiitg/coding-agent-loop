import React, { useEffect, useRef, useState, useCallback } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { prism } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { useWorkspaceStore } from '../../stores/useWorkspaceStore'
import { useAppStore } from '../../stores/useAppStore'
import { workspaceApi } from '../../services/api'
import { agentApi } from '../../services/api'
import mermaid from 'mermaid'

interface MarkdownRendererProps {
  content: string
  className?: string
  maxHeight?: string
  showScrollbar?: boolean
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
  showScrollbar = false
}) => {
  const containerClasses = `prose prose-sm max-w-none dark:prose-invert ${className}`
  const scrollClasses = showScrollbar ? `max-h-[${maxHeight}] overflow-y-auto overflow-x-auto` : ""

  const highlightFile = useWorkspaceStore(state => state.highlightFile)
  const expandFoldersForFile = useWorkspaceStore(state => state.expandFoldersForFile)
  const setSelectedFile = useWorkspaceStore(state => state.setSelectedFile)
  const setShowFileContent = useWorkspaceStore(state => state.setShowFileContent)
  const setFileContent = useWorkspaceStore(state => state.setFileContent)
  const setLoadingFileContent = useWorkspaceStore(state => state.setLoadingFileContent)
  const setBinaryFileData = useWorkspaceStore(state => state.setBinaryFileData)
  const setWorkspaceMinimized = useAppStore(state => state.setWorkspaceMinimized)

  // Standard file opening logic extracted from Workspace.tsx
  const handleWorkspaceLink = async (filepath: string) => {
    console.log('[MarkdownWorkspace] handleWorkspaceLink called for:', filepath)
    try {
      // Auto-resolve folder paths to their default file (e.g. Plans/foo -> Plans/foo/plan.md)
      let resolvedPath = filepath
      if (filepath.startsWith('Plans/') && !filepath.includes('.')) {
        resolvedPath = filepath + '/plan.md'
      }

      // 1. Ensure workspace is visible
      setWorkspaceMinimized(false)

      // 2. Expand folders and highlight in explorer
      expandFoldersForFile(resolvedPath)
      highlightFile(resolvedPath)

      // 3. Select file and start loading content
      const fileName = resolvedPath.split('/').pop() || resolvedPath
      setSelectedFile({ name: fileName, path: resolvedPath })
      setLoadingFileContent(true)
      
      // Check if binary viewable file (simplified check for link handler)
      const ext = fileName.split('.').pop()?.toLowerCase() || ''
      const isViewableBinary = ['xls', 'xlsx', 'docx'].includes(ext)

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

      // Normal text file
      setBinaryFileData(null)
      const response = await agentApi.getPlannerFileContent(resolvedPath)
      if (response.success && response.data) {
        let content = typeof response.data.content === 'string'
          ? response.data.content
          : String(response.data.content)
        
        // Basic normalization
        content = content.replace(/\\n/g, '\n').replace(/\\t/g, '\t').replace(/\\r/g, '\r')
        
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

    // 2. Auto-link workspace paths
    // Regex matches paths starting with specific prefixes: Chats/, Workflow/, Plans/, skills/
    // Optionally matches surrounding backticks to unwrap them
    // CRITICAL FIX: We must handle paths wrapped in backticks (e.g., `Chats/foo.md`) because LLMs often format them as code.
    // The regex captures:
    // Group 1: Optional opening backtick
    // Group 2: The actual path (starting with allowed prefixes)
    // \1: Matches the closing backtick if Group 1 matched (ensuring balanced quotes)
    const pathRegex = /(`?)\b((?:Chats|Workflow|Plans|skills)\/[\w\-./]+)\1/g
    
    // Replace with custom link protocol "#workspace/" to avoid sanitization issues
    // CHALLENGE 1: ReactMarkdown sanitizes unknown protocols like "workspace://", stripping the href.
    // SOLUTION: Use "#workspace/" (hash-based) which is always allowed, then intercept it in the <a> tag renderer.
    // CHALLENGE 2: `replace` callback arguments in arrow functions vs `arguments` object.
    // SOLUTION: Use explicit `...args` rest parameter to reliably capture `offset` and `string` regardless of capture groups.
    processed = processed.replace(pathRegex, (match, backtick, path, ...args) => {
      try {
        console.log('[MarkdownWorkspace] Regex match found:', match)
        
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
        
        console.log(`[MarkdownWorkspace] Match: "${match}", Path: "${path}", Backtick: "${backtick}", Before: "${before}", After: "${after}"`)

        // If capture group 1 (backtick) is present, we are unwrapping, so we don't skip.
        // If capture group 1 is empty, we check if we are inside OTHER code/links/formatting.
        if (!backtick) {
           if (before === '`' || before === '[' || before === '(' || before === '*' || after === ']' || after === '`' || after === '*') {
             console.log('[MarkdownWorkspace] Skipping match (inside link or code)')
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
        console.log('[MarkdownWorkspace] Replacing with:', result)
        return result
      } catch (err) {
        console.error('[MarkdownWorkspace] Error in regex replace:', err)
        return match
      }
    })
    
    const idx = processed.indexOf('#workspace/')
    if (idx !== -1) {
      console.log('[MarkdownWorkspace] Final processed markdown (around link):', processed.slice(Math.max(0, idx - 50), idx + 100))
    } else {
      console.log('[MarkdownWorkspace] No #workspace/ link found in processed markdown (start):', processed.slice(0, 200))
    }
    
    return processed
  }, [content])

  return (
    <div className={`${containerClasses} ${scrollClasses} markdown-content`}>
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
            console.log('[MarkdownWorkspace] Rendering link with href:', href)
            
            if (href?.startsWith('#workspace/')) {
              const filepath = decodeURIComponent(href.replace('#workspace/', ''))
              return (
                <a 
                  href={href}
                  onClick={(e) => {
                    console.log('[MarkdownWorkspace] Clicked link (onClick):', filepath)
                    e.preventDefault()
                    e.stopPropagation()
                    handleWorkspaceLink(filepath)
                  }}
                  onMouseDown={() => {
                     console.log('[MarkdownWorkspace] Mouse down on link:', filepath)
                  }}
                  style={{ pointerEvents: 'auto', cursor: 'pointer' }}
                  className="text-purple-600 dark:text-purple-400 hover:text-purple-800 dark:hover:text-purple-300 underline cursor-pointer break-words overflow-wrap-anywhere font-medium transition-colors"
                  title={`Open ${filepath} in workspace`}
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
                onClick={() => console.log('[MarkdownWorkspace] Clicked default link:', href)}
                className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline break-words overflow-wrap-anywhere"
              >
                {children}
              </a>
            )
          },
          img: ({ src, alt }) => (
            <img 
              src={src} 
              alt={alt} 
              className="max-w-full h-auto rounded-lg shadow-md my-4 border border-gray-200 dark:border-gray-700"
            />
          ),
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

export const ConversationMarkdownRenderer: React.FC<{ content: string; maxHeight?: string }> = ({ content, maxHeight = "384px" }) => (
  <div className={`border border-gray-200 dark:border-gray-700 rounded-md bg-white dark:bg-gray-800 overflow-y-auto overflow-x-hidden min-w-0`} style={{ maxHeight }}>
    <div className="p-3 min-w-0">
      <MarkdownRenderer content={content} />
    </div>
  </div>
)