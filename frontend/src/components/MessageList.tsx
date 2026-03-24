import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Prism as SyntaxHighlighter } from 'react-syntax-highlighter';
import { prism } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { Loader2, Bot, User, Zap, XCircle, CheckCircle, Clock } from 'lucide-react';
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from './ui/accordion';

export interface StreamingMessage {
  id: string;
  type: 'user' | 'assistant' | 'system' | 'tool' | 'error' | 'system_prompt' | 'tool_request' | 'tool_response';
  content: string;
  timestamp: string;
  metadata?: Record<string, unknown>;
}

interface MessageListProps {
  messages: StreamingMessage[];
  isStreaming: boolean;
}

const getMessageIcon = (type: string, content?: string) => {
  if (content && (content.includes('⏳') || content.includes('throttling') || content.includes('Waiting'))) {
    return <Clock className="w-4 h-4 text-yellow-600" />;
  }
  switch (type) {
    case 'user': return <User className="w-4 h-4" />;
    case 'assistant': return <Bot className="w-4 h-4" />;
    case 'system': return <Zap className="w-4 h-4" />;
    case 'error': return <XCircle className="w-4 h-4" />;
    case 'system_prompt': return <Zap className="w-4 h-4 text-purple-500" />;
    case 'tool_request': return <Zap className="w-4 h-4 text-blue-500" />;
    case 'tool_response': return <CheckCircle className="w-4 h-4 text-green-500" />;
    default: return <CheckCircle className="w-4 h-4" />;
  }
};

const getMessageColor = (type: string, content?: string) => {
  if (content && (content.includes('⏳') || content.includes('throttling') || content.includes('Waiting'))) {
    return 'bg-yellow-50 border-yellow-300 dark:bg-yellow-900/20 dark:border-yellow-700';
  }
  switch (type) {
    case 'user': return 'bg-blue-50 border-blue-200 dark:bg-blue-900/20 dark:border-blue-800';
    case 'assistant': return 'bg-gray-50 border-gray-200 dark:bg-gray-900/20 dark:border-gray-800';
    case 'system': return 'bg-green-50 border-green-200 dark:bg-green-900/20 dark:border-green-800';
    case 'error': return 'bg-red-50 border-red-200 dark:bg-red-900/20 dark:border-red-800';
    case 'system_prompt': return 'bg-purple-50 border-purple-200 dark:bg-purple-900/20 dark:border-purple-800';
    case 'tool_request': return 'bg-blue-50 border-blue-200 dark:bg-blue-900/20 dark:border-blue-800';
    case 'tool_response': return 'bg-green-50 border-green-200 dark:bg-green-900/20 dark:border-green-800';
    default: return 'bg-gray-50 border-gray-200 dark:bg-gray-900/20 dark:border-gray-800';
  }
};

const MessageList: React.FC<MessageListProps> = ({ messages, isStreaming }) => (
  <div className="flex-1 space-y-6 mb-6">
    {messages.length === 0 ? (
      <div className="text-center text-gray-500 dark:text-gray-400 py-8">
        <Bot className="w-12 h-12 mx-auto mb-4 opacity-50" />
        <p>Start a conversation with the AI agent</p>
        <p className="text-sm">Use the preset query buttons below or type your own question</p>
      </div>
    ) : (
      messages.map((message) => (
        <div
          key={message.id}
          className={`p-4 ${getMessageColor(message.type, message.content)}`}
        >
          <div className="flex items-start gap-4">
            <div className="flex-shrink-0 mt-1">
              {getMessageIcon(message.type, message.content)}
            </div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2 mb-2">
                <span className="text-base font-medium capitalize">
                  {message.type}
                </span>
                <span className="text-sm text-gray-500">
                  {new Date(message.timestamp).toLocaleTimeString()}
                </span>
              </div>
              <div className="text-base whitespace-pre-wrap break-words">
                {message.type === 'assistant' ? (
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    components={{
                      code(props: React.HTMLAttributes<HTMLElement> & { inline?: boolean }) {
                        const { className, children, inline, ...rest } = props;
                        const isInline = typeof inline === 'boolean' ? inline : false;
                        const match = /language-(\w+)/.exec(className || '');
                        return !isInline && match ? (
                          <SyntaxHighlighter
                            // @ts-expect-error: theme type mismatch is safe for SyntaxHighlighter
                            style={prism as { [key: string]: React.CSSProperties }}
                            language={match[1]}
                            PreTag="div"
                            {...rest}
                          >
                            {String(children).replace(/\n$/, '')}
                          </SyntaxHighlighter>
                        ) : (
                          <code className={className} {...rest}>
                            {children}
                          </code>
                        );
                      },
                      table: ({ children }) => (
                        <div className="overflow-x-auto my-4">
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
                        <th className="border border-gray-300 dark:border-gray-600 px-3 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-300 uppercase tracking-wider">
                          {children}
                        </th>
                      ),
                      td: ({ children }) => (
                        <td className="border border-gray-300 dark:border-gray-600 px-3 py-2 text-sm text-gray-900 dark:text-gray-100">
                          {children}
                        </td>
                      ),
                    }}
                  >
                    {message.content}
                  </ReactMarkdown>
                ) : message.type === 'system_prompt' || message.type === 'tool_request' ? (
                  <pre className="bg-gray-100 dark:bg-gray-800 rounded p-2 text-sm overflow-x-auto">
                    {message.content}
                  </pre>
                ) : message.type === 'user' ? (
                  <pre className="bg-blue-50 dark:bg-blue-900/20 rounded p-2 text-sm overflow-x-auto">
                    {message.content}
                  </pre>
                ) : message.type === 'tool_response' ? (
                  <div className="bg-green-50 dark:bg-green-900/20 rounded p-3 text-sm">
                    {/* Tool response formatting logic omitted for brevity; can be added as needed */}
                    {message.content}
                  </div>
                ) : (
                  message.content
                )}
              </div>
              {message.metadata && (
                <Accordion type="single" collapsible className="mt-2">
                  <AccordionItem value="metadata">
                    <AccordionTrigger className="text-xs text-gray-500 cursor-pointer">
                      Metadata
                    </AccordionTrigger>
                    <AccordionContent>
                      <pre className="text-xs text-gray-600 dark:text-gray-400 mt-1">
                        {JSON.stringify(message.metadata, null, 2)}
                      </pre>
                    </AccordionContent>
                  </AccordionItem>
                </Accordion>
              )}
            </div>
          </div>
        </div>
      ))
    )}
    {isStreaming && (
      <div className="flex items-center gap-2 text-sm text-gray-500">
        <Loader2 className="w-4 h-4 animate-spin" />
        Agent is thinking...
      </div>
    )}
  </div>
);

export default MessageList; 