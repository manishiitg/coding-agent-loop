import { useEffect, useState } from 'react'
import { productWebhooksApi, type ProductAPITrigger } from '../../../api/productWebhooks'
import { agentApi } from '../../../services/api'

interface CrewAttachment {
  alias: string
  crew_profile_id: string
  crew_project_id: string
}

export function useCrewTrigger(profileId: string, projectId: string, triggerId: string): ProductAPITrigger | null | undefined {
  const [trigger, setTrigger] = useState<ProductAPITrigger | null | undefined>(undefined)
  useEffect(() => {
    let cancelled = false
    setTrigger(undefined)
    productWebhooksApi.list({ profileId, projectId }).then(
      (data) => {
        if (cancelled) return
        setTrigger(data.triggers.find((candidate) => candidate.id === triggerId) ?? null)
      },
      () => {
        if (!cancelled) setTrigger(null)
      },
    )
    return () => {
      cancelled = true
    }
  }, [profileId, projectId, triggerId])
  return trigger
}

export function useCrewAttachmentAlias(workspacePath: string | null, projectId: string): string | null | undefined {
  const [alias, setAlias] = useState<string | null | undefined>(undefined)
  useEffect(() => {
    let cancelled = false
    setAlias(undefined)
    if (!workspacePath) {
      setAlias(null)
      return
    }
    const manifestPath = `${workspacePath.replace(/\/+$/g, '')}/workflow.json`
    agentApi.getPlannerFileContent(manifestPath).then(
      (response: { success?: boolean; data?: { content?: unknown } }) => {
        if (cancelled) return
        try {
          const content = response?.success ? response.data?.content : undefined
          const parsed = (typeof content === 'string' ? JSON.parse(content) : content) as { crew_attachments?: CrewAttachment[] }
          const match = (parsed?.crew_attachments ?? []).find((attachment) => attachment.crew_project_id === projectId)
          setAlias(match ? match.alias : null)
        } catch {
          setAlias(null)
        }
      },
      () => {
        if (!cancelled) setAlias(null)
      },
    )
    return () => {
      cancelled = true
    }
  }, [workspacePath, projectId])
  return alias
}
