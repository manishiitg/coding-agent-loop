import React from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

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
        `
      }} />
      <ReactMarkdown 
        remarkPlugins={[remarkGfm]}
        components={{
          p: ({ children }) => <p className="mb-2 last:mb-0 text-sm leading-relaxed break-words overflow-wrap-anywhere">{children}</p>,
          h1: ({ children }) => <h1 className="text-lg font-bold mb-2 break-words overflow-wrap-anywhere">{children}</h1>,
          h2: ({ children }) => <h2 className="text-base font-semibold mb-2 break-words overflow-wrap-anywhere">{children}</h2>,
          h3: ({ children }) => <h3 className="text-sm font-semibold mb-1 break-words overflow-wrap-anywhere">{children}</h3>,
          ul: ({ children }) => <ul className="list-disc mb-2 space-y-1 ml-4 pl-2 min-w-0">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal mb-2 space-y-1 ml-4 pl-2 min-w-0">{children}</ol>,
          li: ({ children }) => <li className="text-sm break-words overflow-wrap-anywhere leading-relaxed mb-1 last:mb-0">{children}</li>,
          code: ({ children }) => (
            <code className="bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded text-xs font-mono break-all overflow-wrap-anywhere">
              {children}
            </code>
          ),
          pre: ({ children }) => (
            <pre className="bg-gray-100 dark:bg-gray-800 p-2 rounded text-xs font-mono overflow-x-auto break-all min-w-0">
              {children}
            </pre>
          ),
          blockquote: ({ children }) => (
            <blockquote className="border-l-4 border-gray-300 pl-3 italic text-sm break-words overflow-wrap-anywhere">
              {children}
            </blockquote>
          ),
          strong: ({ children }) => <strong className="font-semibold break-words overflow-wrap-anywhere">{children}</strong>,
          em: ({ children }) => <em className="italic break-words overflow-wrap-anywhere">{children}</em>,
          table: ({ children }) => (
            <div className="overflow-x-auto my-4 min-w-0">
              <table className="min-w-full border-collapse border border-gray-300 dark:border-gray-600">
                {children}
              </table>
            </div>
          ),
          thead: ({ children }) => (
            <thead className="bg-gray-50 dark:bg-gray-700">
              {children}
            </thead>
          ),
          tbody: ({ children }) => (
            <tbody className="bg-white dark:bg-gray-800">
              {children}
            </tbody>
          ),
          tr: ({ children }) => (
            <tr className="border-b border-gray-200 dark:border-gray-600">
              {children}
            </tr>
          ),
          th: ({ children }) => (
            <th className="border border-gray-300 dark:border-gray-600 px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider break-words overflow-wrap-anywhere">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="border border-gray-300 dark:border-gray-600 px-3 py-2 text-sm text-gray-900 dark:text-gray-100 break-words overflow-wrap-anywhere">
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