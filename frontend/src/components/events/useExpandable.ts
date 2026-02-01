import { useState, useEffect, useCallback } from 'react'
import { useEventMode } from './useEventMode'

export const useExpandable = (defaultCollapsedInMicro = true) => {
  const { mode, setMode } = useEventMode()
  
  // Determine if we should be expanded based on mode
  // Collapsed by default only in 'micro' mode
  const isMicro = mode === 'micro'
  const shouldBeExpanded = defaultCollapsedInMicro ? !isMicro : true
  
  // Local state
  const [isExpanded, setIsExpanded] = useState(shouldBeExpanded)

  // Sync with mode changes
  useEffect(() => {
    setIsExpanded(shouldBeExpanded)
  }, [mode, shouldBeExpanded])

  // Toggle function that handles modifier keys for global switching
  const toggle = useCallback((e?: React.MouseEvent) => {
    if (e && (e.altKey || e.metaKey)) {
      // Global toggle via modifier key
      // If currently collapsed (and we want to expand), switch to 'basic' (expanded)
      // If currently expanded (and we want to collapse), switch to 'micro' (collapsed)
      const newMode = !isExpanded ? 'basic' : 'micro'
      setMode(newMode)
    } else {
      // Local toggle
      setIsExpanded(prev => !prev)
    }
  }, [isExpanded, setMode])

  return { isExpanded, toggle, mode }
}
