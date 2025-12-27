import React from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface NodeMarkdownProps {
  content: string
  className?: string
  textSize?: 'xs' | 'sm' | 'tiny'
}

/**
 * Compact markdown renderer for node text fields (descriptions, success criteria, etc.)
 * Styled to match node text sizes and colors
 */
export const NodeMarkdown: React.FC<NodeMarkdownProps> = ({ 
  content, 
  className = '',
  textSize = 'xs'
}) => {
  const sizeClasses = {
    xs: 'text-xs',
    sm: 'text-sm',
    tiny: 'text-[10px]'
  }

  const baseTextColor = 'text-gray-600 dark:text-gray-400'
  const headingColor = 'text-gray-900 dark:text-white'
  const codeBg = 'bg-gray-100 dark:bg-gray-800'
  const codeText = 'text-gray-800 dark:text-gray-200'

  return (
    <div className={`${className} ${sizeClasses[textSize]} ${baseTextColor} leading-relaxed`}>
      <ReactMarkdown 
        remarkPlugins={[remarkGfm]}
        components={{
          // Paragraphs - remove default margins, use inline
          p: ({ children }) => (
            <span className="block mb-1 last:mb-0 break-words">
              {children}
            </span>
          ),
          // Headings - smaller and compact
          h1: ({ children }) => (
            <span className={`block font-bold mb-1 ${headingColor} ${textSize === 'xs' ? 'text-sm' : 'text-base'}`}>
              {children}
            </span>
          ),
          h2: ({ children }) => (
            <span className={`block font-semibold mb-1 ${headingColor} ${textSize === 'xs' ? 'text-xs' : 'text-sm'}`}>
              {children}
            </span>
          ),
          h3: ({ children }) => (
            <span className={`block font-semibold mb-0.5 ${headingColor} ${textSize === 'xs' ? 'text-xs' : 'text-sm'}`}>
              {children}
            </span>
          ),
          // Lists - compact spacing
          ul: ({ children }) => (
            <ul className="list-disc ml-4 mb-1 space-y-0.5">
              {children}
            </ul>
          ),
          ol: ({ children }) => (
            <ol className="list-decimal ml-4 mb-1 space-y-0.5">
              {children}
            </ol>
          ),
          li: ({ children }) => (
            <li className="break-words">
              {children}
            </li>
          ),
          // Inline code
          code: ({ children, inline, ...props }: React.HTMLAttributes<HTMLElement> & { inline?: boolean }) => {
            if (inline) {
              return (
                <code 
                  className={`${codeBg} ${codeText} px-1 py-0.5 rounded text-[10px] font-mono`}
                  {...props}
                >
                  {children}
                </code>
              )
            }
            // Code blocks - simplified for nodes
            return (
              <code 
                className={`${codeBg} ${codeText} block p-1 rounded text-[10px] font-mono mb-1 overflow-x-auto`}
                {...props}
              >
                {children}
              </code>
            )
          },
          // Block quotes
          blockquote: ({ children }) => (
            <blockquote className={`border-l-2 border-gray-300 dark:border-gray-600 pl-2 ml-2 mb-1 italic ${baseTextColor}`}>
              {children}
            </blockquote>
          ),
          // Links
          a: ({ href, children }) => (
            <a 
              href={href} 
              target="_blank" 
              rel="noopener noreferrer"
              className="text-blue-600 dark:text-blue-400 hover:underline break-all"
            >
              {children}
            </a>
          ),
          // Bold and italic
          strong: ({ children }) => (
            <strong className="font-semibold text-gray-900 dark:text-gray-200">
              {children}
            </strong>
          ),
          em: ({ children }) => (
            <em className="italic">
              {children}
            </em>
          ),
          // Horizontal rule
          hr: () => (
            <hr className="border-gray-300 dark:border-gray-600 my-1" />
          ),
          // Tables - simplified for compact display
          table: ({ children }) => (
            <div className="overflow-x-auto my-1">
              <table className="min-w-full border-collapse border border-gray-300 dark:border-gray-600 text-[10px]">
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
            <th className="border border-gray-300 dark:border-gray-600 px-1 py-0.5 text-left font-medium text-gray-700 dark:text-gray-300">
              {children}
            </th>
          ),
          td: ({ children }) => (
            <td className="border border-gray-300 dark:border-gray-600 px-1 py-0.5 text-gray-900 dark:text-gray-100">
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

