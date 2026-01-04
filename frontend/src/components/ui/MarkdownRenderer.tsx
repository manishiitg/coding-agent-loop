import React from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter'
import { prism } from 'react-syntax-highlighter/dist/esm/styles/prism'
import { oneDark } from 'react-syntax-highlighter/dist/esm/styles/prism'

interface MarkdownRendererProps {
  content: string
  className?: string
  maxHeight?: string
  showScrollbar?: boolean
}

export const MarkdownRenderer: React.FC<MarkdownRendererProps> = ({ 
  content, 
  className = "", 
  maxHeight = "none",
  showScrollbar = false 
}) => {
  const containerClasses = `prose prose-sm max-w-none dark:prose-invert ${className}`
  const scrollClasses = showScrollbar ? `max-h-[${maxHeight}] overflow-y-auto overflow-x-auto` : ""

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
          a: ({ href, children }) => (
            <a 
              href={href} 
              target="_blank" 
              rel="noopener noreferrer"
              className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline break-words overflow-wrap-anywhere"
            >
              {children}
            </a>
          ),
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
            <tr className="transition-colors [&:hover]:bg-gray-50 dark:[&:hover]:bg-gray-800 [&:focus]:bg-gray-50 dark:[&:focus]:bg-gray-800 [&:active]:bg-gray-100 dark:[&:active]:bg-gray-700">
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
        {content}
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