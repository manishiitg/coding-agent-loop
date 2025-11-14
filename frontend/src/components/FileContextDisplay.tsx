import { X } from 'lucide-react'

interface FileContextItem {
  name: string
  path: string
  type: 'file' | 'folder'
}

interface FileContextDisplayProps {
  files: FileContextItem[]
  onRemoveFile: (path: string) => void
  onClearAll: () => void
  agentMode: 'simple' | 'workflow'
  isRequiredFolderSelected: boolean
}

export default function FileContextDisplay({ files, onRemoveFile, onClearAll, agentMode, isRequiredFolderSelected }: FileContextDisplayProps) {
  if (files.length === 0) {
    return null
  }

  return (
    <div className={`border rounded px-1.5 py-0.5 mb-1 ${
      agentMode === 'workflow' && !isRequiredFolderSelected
        ? 'bg-orange-50 dark:bg-orange-900/20 border-orange-200 dark:border-orange-800'
        : 'bg-gray-50 dark:bg-gray-800 border-gray-200 dark:border-gray-700'
    }`}>
      <div className="flex items-center gap-1.5 flex-wrap">
        <span className={`text-xs font-medium ${
          agentMode === 'workflow' && !isRequiredFolderSelected
            ? 'text-orange-600 dark:text-orange-400'
            : 'text-gray-600 dark:text-gray-400'
        }`}>
          {agentMode === 'workflow' && !isRequiredFolderSelected ? '📁 Context (Select Workflow folder):' : '📁 Context:'}
        </span>
        {files.map((file, index) => (
          <div key={file.path} className="flex items-center gap-0.5">
            <span className="text-xs text-gray-700 dark:text-gray-300 font-mono">
              {file.path}
            </span>
            <button
              onClick={() => onRemoveFile(file.path)}
              className="p-0.5 hover:bg-red-100 dark:hover:bg-red-900/20 rounded text-red-500 hover:text-red-700 dark:hover:text-red-400"
              title={`Remove ${file.type} from context`}
            >
              <X className="w-2 h-2" />
            </button>
            {index < files.length - 1 && (
              <span className="text-xs text-gray-400">•</span>
            )}
          </div>
        ))}
        <button
          onClick={onClearAll}
          className="text-xs text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 hover:underline ml-0.5"
        >
          Clear
        </button>
      </div>
    </div>
  )
}
