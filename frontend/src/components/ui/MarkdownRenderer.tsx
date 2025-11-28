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
          code: ({ className, children, inline, ...props }: React.HTMLAttributes<HTMLElement> & { inline?: boolean }) => {
            const match = /language-(\w+)/.exec(className || '')
            const isInline = typeof inline === 'boolean' ? inline : false
            
            // For inline code, use simple styling
            if (isInline || !match) {
              return (
                <code className="bg-gray-100 dark:bg-gray-800 px-1 py-0.5 rounded text-xs font-mono break-all overflow-wrap-anywhere" {...props}>
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
              <div className="my-2 min-w-0 max-w-full overflow-x-auto">
                <SyntaxHighlighter
                  // @ts-expect-error: theme type mismatch is safe for SyntaxHighlighter
                  style={isDark ? (oneDark as { [key: string]: React.CSSProperties }) : (prism as { [key: string]: React.CSSProperties })}
                  language={language}
                  PreTag="div"
                  className="!m-0 !p-0"
                  customStyle={{
                    margin: 0,
                    padding: '0.75rem',
                    borderRadius: '0.375rem',
                    fontSize: '0.75rem',
                    lineHeight: '1.5',
                    overflowX: 'auto',
                    maxWidth: '100%',
                    width: '100%',
                    boxSizing: 'border-box',
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