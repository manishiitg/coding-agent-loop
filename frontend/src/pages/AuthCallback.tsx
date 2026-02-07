import { useEffect, useState } from 'react'
import { useAuthStore } from '../stores/useAuthStore'

export function AuthCallback() {
  const { handleOAuthCallback, error, isAuthenticated } = useAuthStore()
  const [callbackError, setCallbackError] = useState<string | null>(null)

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const code = params.get('code')
    const state = params.get('state')
    const errorParam = params.get('error')
    const errorDescription = params.get('error_description')

    // Handle error from OAuth provider
    if (errorParam) {
      setCallbackError(errorDescription || errorParam)
      return
    }

    // Validate required parameters
    if (!code || !state) {
      setCallbackError('Missing authorization code or state parameter')
      return
    }

    // Process the callback
    handleOAuthCallback(code, state)
      .catch((err) => {
        console.error('OAuth callback failed:', err)
        setCallbackError(err.message || 'Authentication failed')
      })
  }, [handleOAuthCallback])

  // Redirect on successful authentication
  useEffect(() => {
    if (isAuthenticated) {
      window.location.href = '/'
    }
  }, [isAuthenticated])

  const displayError = callbackError || error

  const handleBackToLogin = () => {
    window.location.href = '/'
  }

  if (displayError) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="max-w-md w-full space-y-8 p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg">
          <div className="text-center">
            <h2 className="text-xl font-bold text-red-600 dark:text-red-400">
              Authentication Failed
            </h2>
            <p className="mt-4 text-gray-600 dark:text-gray-400">
              {displayError}
            </p>
            <button
              onClick={handleBackToLogin}
              className="mt-6 px-4 py-2 bg-blue-600 text-white rounded hover:bg-blue-700 transition-colors"
            >
              Back to Login
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
      <div className="max-w-md w-full space-y-8 p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg">
        <div className="text-center">
          <h2 className="text-xl font-bold text-gray-900 dark:text-white">
            Completing sign in...
          </h2>
          <div className="mt-4 flex justify-center">
            <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600"></div>
          </div>
        </div>
      </div>
    </div>
  )
}
