import React from 'react'
import { Loader2, Upload } from 'lucide-react'
import ModalPortal from './ModalPortal'

interface ImportProgressDialogProps {
  isOpen: boolean
  progress: number
  fileName?: string
}

export default function ImportProgressDialog({ isOpen, progress, fileName }: ImportProgressDialogProps) {
  if (!isOpen) return null

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-4 bg-black/50 backdrop-blur-sm">
      <div className="bg-card rounded-md shadow-md border border-border w-full max-w-md overflow-hidden animate-in fade-in zoom-in duration-200">
        <div className="p-6">
          <div className="flex items-center gap-3 mb-6">
            <div className="p-2 bg-primary/10 rounded-full">
              <Upload className="w-6 h-6 text-primary" />
            </div>
            <div>
              <h3 className="text-sm font-semibold text-foreground">
                Importing Workspace Backup
              </h3>
              {fileName && (
                <p className="text-sm text-muted-foreground truncate max-w-[300px]">
                  {fileName}
                </p>
              )}
            </div>
          </div>

          <div className="space-y-4">
            <div className="flex justify-between items-end mb-1">
              <span className="text-sm font-medium text-foreground">
                {progress < 100 ? 'Uploading files...' : 'Extracting and processing...'}
              </span>
              <span className="text-sm font-bold text-primary">
                {progress}%
              </span>
            </div>
            
            <div className="w-full bg-muted rounded-full h-3 overflow-hidden">
              <div 
                className="bg-primary h-full transition-all duration-300 ease-out rounded-full"
                style={{ width: `${progress}%` }}
              />
            </div>

            <div className="flex items-center justify-center gap-2 pt-2">
              <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
              <p className="text-xs text-muted-foreground">
                Please do not close this window
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
    </ModalPortal>
  )
}
