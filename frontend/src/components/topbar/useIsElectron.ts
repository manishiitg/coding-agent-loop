import { useEffect, useState } from 'react'

/** True when running inside the Electron desktop shell (preload API present). */
export function useIsElectron(): boolean {
  const [isElectron, setIsElectron] = useState(false)

  useEffect(() => {
    setIsElectron(!!(window as any).electronAPI)
  }, [])

  return isElectron
}
