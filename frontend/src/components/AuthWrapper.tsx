import { useEffect, useState } from 'react'
import { useAuthStore } from '../stores/useAuthStore'
import { Login } from '../pages/Login'
import { AuthCallback } from '../pages/AuthCallback'
import { SharedFile } from '../pages/SharedFile'
import { SharedFolder } from '../pages/SharedFolder'
import { ReportPage } from '../pages/ReportPage'
import { Loader2 } from 'lucide-react'
import { WorkspaceConnectionSwitcher } from './WorkspaceConnectionSwitcher'
import { DesktopAppOnlyGate } from './DesktopAppOnlyGate'
import { isDesktopAppOnlyMode } from '../services/api'

interface AuthWrapperProps {
  children: React.ReactNode
}

export function AuthWrapper({ children }: AuthWrapperProps) {
  const {
    isAuthenticated,
    isMultiUserMode,
    isMultiUserModeChecked,
    isLoading,
    checkAuthMode,
    checkAuth,
    login
  } = useAuthStore()

  const [sharedFilePath, setSharedFilePath] = useState<string | null>(null)
  const [sharedFolderPath, setSharedFolderPath] = useState<string | null>(null)
  const [reportWorkspacePath, setReportWorkspacePath] = useState<string | null>(null)
  const [sharedUid, setSharedUid] = useState<string | null>(null)
  const [isAuthCallback, setIsAuthCallback] = useState(false)
  const [singleUserAuthAttempted, setSingleUserAuthAttempted] = useState(false)

  // Check for shared file/folder URL or OAuth callback
  useEffect(() => {
    const path = window.location.pathname
    const params = new URLSearchParams(window.location.search)
    const uidParam = params.get('uid')

    // Check for shared file: /file?path=BASE64&uid=OWNER_ID
    if (path === '/file') {
      const encodedPath = params.get('path')
      if (encodedPath) {
        setSharedFilePath(encodedPath)
        if (uidParam) setSharedUid(uidParam)
        return
      }
    }

    // Check for shared folder: /folder?path=BASE64&uid=OWNER_ID
    if (path === '/folder') {
      const encodedPath = params.get('path')
      if (encodedPath) {
        setSharedFolderPath(encodedPath)
        if (uidParam) setSharedUid(uidParam)
        return
      }
    }

    // Check for dedicated workflow report URL: /report?path=BASE64_WORKSPACE_PATH
    if (path === '/report') {
      const encodedPath = params.get('path')
      if (encodedPath) {
        setReportWorkspacePath(encodedPath)
        return
      }
    }

    // Check for OAuth callback
    if (path === '/auth/callback') {
      setIsAuthCallback(true)
      return
    }
  }, [])

  // Initialize auth state
  useEffect(() => {
    checkAuthMode()
    checkAuth()
  }, [checkAuthMode, checkAuth])

  useEffect(() => {
    if (!isMultiUserModeChecked || isMultiUserMode || isAuthenticated || isLoading || singleUserAuthAttempted) {
      return
    }
    setSingleUserAuthAttempted(true)
    login('', '').catch((error) => {
      console.error('[AUTH] Failed to initialize single-user token:', error)
    })
  }, [isMultiUserModeChecked, isMultiUserMode, isAuthenticated, isLoading, singleUserAuthAttempted, login])

  // If viewing a shared file, render it directly
  if (sharedFilePath) {
    return (
      <SharedFile
        encodedPath={sharedFilePath}
        uid={sharedUid || undefined}
        onBack={() => {
          setSharedFilePath(null)
          window.history.pushState({}, '', '/')
        }}
      />
    )
  }

  // If viewing a shared folder, render it directly
  if (sharedFolderPath) {
    return (
      <SharedFolder
        encodedPath={sharedFolderPath}
        uid={sharedUid || undefined}
        onBack={() => {
          setSharedFolderPath(null)
          window.history.pushState({}, '', '/')
        }}
      />
    )
  }

  // If handling OAuth callback, render callback component
  if (isAuthCallback) {
    return <AuthCallback />
  }

  // Still loading auth mode or local single-user token
  if (!isMultiUserModeChecked || isLoading || (!isMultiUserMode && !isAuthenticated && !singleUserAuthAttempted)) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <WorkspaceConnectionSwitcher placement="auth" />
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto text-blue-500" />
          <p className="mt-4 text-gray-600 dark:text-gray-400">Loading...</p>
        </div>
      </div>
    )
  }

  // Single-user mode: no auth required, render children directly
  if (!isMultiUserMode) {
    if (isDesktopAppOnlyMode() && !(window as any).electronAPI) {
      return <DesktopAppOnlyGate />
    }
    if (reportWorkspacePath) {
      return (
        <ReportPage
          encodedPath={reportWorkspacePath}
          onBack={() => {
            setReportWorkspacePath(null)
            window.history.pushState({}, '', '/')
          }}
        />
      )
    }
    return <>{children}</>
  }

  // Multi-user mode: require authentication (login only, no registration)
  if (!isAuthenticated) {
    return (
      <>
        <WorkspaceConnectionSwitcher placement="auth" />
        <Login />
      </>
    )
  }

  // Authenticated: render children
  if (isDesktopAppOnlyMode() && !(window as any).electronAPI) {
    return <DesktopAppOnlyGate />
  }

  if (reportWorkspacePath) {
    return (
      <ReportPage
        encodedPath={reportWorkspacePath}
        onBack={() => {
          setReportWorkspacePath(null)
          window.history.pushState({}, '', '/')
        }}
      />
    )
  }

  return <>{children}</>
}
