import { useState, useCallback } from 'react'

export const useExpandable = (defaultCollapsedInMicro = true) => {
  // Items start collapsed by default (micro mode behavior)
  const shouldBeExpanded = defaultCollapsedInMicro ? false : true

  // Local state
  const [isExpanded, setIsExpanded] = useState(shouldBeExpanded)

  // Toggle function
  const toggle = useCallback(() => {
    setIsExpanded(prev => !prev)
  }, [])

  return { isExpanded, toggle }
}
