import { useEffect, useState } from 'react'

type ElectronVersionAPI = {
  getAppVersion?: () => Promise<string>
}

/**
 * useAppVersion returns the desktop app version (from the Electron main process
 * via the `get-app-version` IPC). Returns '' in the browser or before it
 * resolves, so callers can simply skip rendering when it's empty.
 */
export function useAppVersion(): string {
  const [version, setVersion] = useState('')

  useEffect(() => {
    const electronAPI = (window as unknown as { electronAPI?: ElectronVersionAPI }).electronAPI
    if (electronAPI?.getAppVersion) {
      electronAPI.getAppVersion().then(setVersion).catch(() => {})
    }
  }, [])

  return version
}
