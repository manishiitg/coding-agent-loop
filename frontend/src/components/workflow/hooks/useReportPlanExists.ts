// Lightweight existence check for reports/report_plan.json. Used to conditionally hide
// Report entry points when no plan file exists,
// since the Report view would render the empty-state anyway.

import { useEffect, useState } from 'react'
import { agentApi } from '../../../services/api'

export function useReportPlanExists(workspacePath: string | null): boolean {
  const [exists, setExists] = useState(false)

  useEffect(() => {
    if (!workspacePath) {
      setExists(false)
      return
    }
    let cancelled = false
    agentApi
      .getPlannerFileContent(`${workspacePath}/reports/report_plan.json`)
      .then(jsonResp => {
        if (cancelled) return
        const has =
          !!jsonResp &&
          jsonResp.success === true &&
          !!jsonResp.data &&
          typeof jsonResp.data.content === 'string' &&
          jsonResp.data.content.trim().length > 0
        setExists(has)
      })
      .catch(() => {
        if (!cancelled) setExists(false)
      })
    return () => {
      cancelled = true
    }
  }, [workspacePath])

  return exists
}
