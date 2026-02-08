import React from 'react'
import { Loader2, Upload } from 'lucide-react'

interface ImportProgressDialogProps {
  isOpen: boolean
  progress: number
  fileName?: string
}

export default function ImportProgressDialog({ isOpen, progress, fileName }: ImportProgressDialogProps) {
  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/50 backdrop-blur-sm">
      <div className="bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 w-full max-w-md overflow-hidden animate-in fade-in zoom-in duration-200">
        <div className="p-6">
          <div className="flex items-center gap-3 mb-6">
            <div className="p-2 bg-blue-100 dark:bg-blue-900/30 rounded-full">
              <Upload className="w-6 h-6 text-blue-600 dark:text-blue-400" />
            </div>
            <div>
              <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                Importing Workspace Backup
              </h3>
              {fileName && (
                <p className="text-sm text-gray-500 dark:text-gray-400 truncate max-w-[300px]">
                  {fileName}
                </p>
              )}
            </div>
          </div>

          <div className="space-y-4">
            <div className="flex justify-between items-end mb-1">
              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                {progress < 100 ? 'Uploading files...' : 'Extracting and processing...'}
              </span>
              <span className="text-sm font-bold text-blue-600 dark:text-blue-400">
                {progress}%
              </span>
            </div>
            
            <div className="w-full bg-gray-200 dark:bg-gray-700 rounded-full h-3 overflow-hidden">
              <div 
                className="bg-blue-600 h-full transition-all duration-300 ease-out rounded-full"
                style={{ width: `${progress}%` }}
              />
            </div>

            <div className="flex items-center justify-center gap-2 pt-2">
              <Loader2 className="w-4 h-4 animate-spin text-gray-400" />
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Please do not close this window
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
