import { useState, useEffect } from 'react'
import { KeyRound } from 'lucide-react'
import { useSecretsStore } from '../../stores'
import SecretsManagerModal from './SecretsManagerModal'

export default function SecretsSection() {
  const [showManager, setShowManager] = useState(false)
  const secrets = useSecretsStore((s) => s.secrets)
  const globalSecrets = useSecretsStore((s) => s.globalSecrets)
  const fetchGlobalSecrets = useSecretsStore((s) => s.fetchGlobalSecrets)

  useEffect(() => {
    if (globalSecrets.length === 0) {
      fetchGlobalSecrets()
    }
  }, [])

  const totalCount = secrets.length + globalSecrets.length

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <KeyRound className="w-4 h-4 text-amber-600 dark:text-amber-400" />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">Secrets</span>
        </div>
        <span className="px-2 py-0.5 text-xs bg-amber-100 dark:bg-amber-900 text-amber-700 dark:text-amber-300 rounded-full">
          {totalCount}
        </span>
      </div>

      <button
        onClick={() => setShowManager(true)}
        className="w-full p-2 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors text-left"
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="w-2 h-2 bg-amber-500 rounded-full"></span>
            <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
              {totalCount} {totalCount === 1 ? 'Secret' : 'Secrets'}
            </span>
          </div>
          <span className="text-xs text-gray-500">Manage</span>
        </div>
        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
          {globalSecrets.length > 0
            ? `${globalSecrets.length} global, ${secrets.length} user`
            : 'Click to manage secrets'}
        </div>
      </button>

      {showManager && (
        <SecretsManagerModal onClose={() => setShowManager(false)} />
      )}
    </div>
  )
}
