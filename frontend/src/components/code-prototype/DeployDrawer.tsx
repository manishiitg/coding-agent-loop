import React, { useEffect, useRef } from 'react'
import { X, ExternalLink, Loader2 } from 'lucide-react'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'

export const DeployDrawer: React.FC = () => {
  const { isDeploying, deployOutput, lastDeployedUrl, clearDeployLog } = useCodePrototypeStore()
  const isOpen = isDeploying || deployOutput.length > 0
  const logEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [deployOutput])

  if (!isOpen) return null

  return (
    <div className="absolute bottom-0 left-0 right-0 z-20 bg-gray-950 border-t border-gray-700 flex flex-col"
      style={{ height: '280px' }}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-gray-800">
        <div className="flex items-center gap-2">
          {isDeploying && <Loader2 className="w-4 h-4 text-emerald-400 animate-spin" />}
          <span className="text-sm font-medium text-gray-200">
            {isDeploying ? 'Deploying…' : 'Deploy complete'}
          </span>
          {lastDeployedUrl && !isDeploying && (
            <a
              href={lastDeployedUrl}
              target="_blank"
              rel="noreferrer"
              className="flex items-center gap-1 text-xs text-emerald-400 hover:text-emerald-300 ml-2"
            >
              {lastDeployedUrl}
              <ExternalLink className="w-3 h-3" />
            </a>
          )}
        </div>
        <button
          onClick={clearDeployLog}
          className="text-gray-400 hover:text-gray-200"
          title="Close"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Log output */}
      <div className="flex-1 overflow-y-auto p-3 font-mono text-xs text-green-400 bg-gray-950">
        {deployOutput.map((line, i) => (
          <div key={i}>{line}</div>
        ))}
        <div ref={logEndRef} />
      </div>
    </div>
  )
}
