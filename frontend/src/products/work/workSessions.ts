import { applyRuntimeManifest, parseProductProjectManifest, updateProductProjectIdentity, updateProductProjectSelections, type ProductIdentityPatch, type ProductProject } from '../../platform/chat/productProjects'
import { agentApi } from '../../services/api'
import { secretsApi } from '../../api/secrets'
import type { LLMProvider, OwnCrewProjectManifests, PresetLLMConfig, SharedProjectSummary } from '../../services/api-types'
import { slugifyTitle } from '../../utils/plannerFiles'
import { loadAgentProfileProviderOptions } from '../../utils/agentProfileCapabilities'
import { WORK_PROFILE_ID } from './workData'

export type WorkSession = ProductProject<typeof WORK_PROFILE_ID>

export type WorkLLMSelection = {
  connectionId?: string
  provider: string
  modelId: string
  reasoningEffort?: string
}

export function workLLMConfigFromSelection(selection: WorkLLMSelection): PresetLLMConfig {
  return {
    schema_version: 2,
    mode: 'explicit',
    builder_llm: {
      provider: selection.provider as LLMProvider,
      connection_id: selection.connectionId,
      model_id: selection.modelId,
      ...(selection.reasoningEffort ? { options: { reasoning_effort: selection.reasoningEffort } } : {}),
    },
  }
}

export function workLLMSelectionFromConfig(config?: PresetLLMConfig): WorkLLMSelection | null {
  const builder = config?.builder_llm
  if (!builder?.provider || !builder.model_id) return null
  const reasoningEffort = typeof builder.options?.reasoning_effort === 'string'
    ? builder.options.reasoning_effort
    : undefined
  return { connectionId: builder.connection_id, provider: builder.provider, modelId: builder.model_id, reasoningEffort }
}

export function sessionSlug(title: string): string {
  return slugifyTitle(title, 'workspace')
}

export function parseSessionManifest(content: string, workspacePath: string, lastModified?: string): WorkSession | null {
  return parseProductProjectManifest(content, workspacePath, WORK_PROFILE_ID, lastModified)
}

// The caller's own crews come from the server: they live at the shared Crew/
// root, which the file proxy does not list (it holds every owner's crews).
export async function loadOwnWorkProjects(): Promise<WorkSession[]> {
  const { projects } = await agentApi.listOwnCrewProjects(WORK_PROFILE_ID)
  return (projects ?? [])
    .map(entry => workSessionFromManifests(entry))
    .filter((session): session is WorkSession => session !== null)
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
}

function workSessionFromManifests(entry: OwnCrewProjectManifests): WorkSession | null {
  const project = parseProductProjectManifest(entry.product_json, entry.workspace_path, WORK_PROFILE_ID)
  if (!project) return null
  return entry.runtime_json ? applyRuntimeManifest(project, entry.runtime_json) : { ...project, runtimeConfigInitialized: false }
}

export async function loadWorkSessions(): Promise<WorkSession[]> {
  const sessions = await loadOwnWorkProjects()
  return Promise.all(sessions.map(async original => {
    let session = original
    if (!session.runtimeConfigInitialized || !session.selectionConfigInitialized) {
      session = await updateProductProjectSelections(session, {
        ...(!session.selectionConfigInitialized ? { selectedServers: [], selectedSkills: [] } : {}),
      }, `Initialize Work project runtime ${session.title}`, 'workflow.json')
    }
    if (!original.secretSelectionInitialized) {
      try {
        const stored = await secretsApi.listWorkflowSecrets(session.workspacePath)
        session = await updateProductProjectSelections(session, { selectedSecrets: stored.map(secret => secret.name) }, `Initialize Work project secret attachments ${session.title}`, 'workflow.json')
      } catch {
        // Keep the project usable during a transient secret-store failure. The
        // server performs the same migration before the next agent turn.
      }
    }
    return session
  }))
}

export async function createWorkSession(title: string, description: string, icon?: string): Promise<WorkSession> {
  const options = await loadAgentProfileProviderOptions(WORK_PROFILE_ID)
  const selected = options.find(option => option.default) || options[0]
  const reasoningEffort = typeof selected?.options?.reasoning_effort === 'string'
    ? selected.options.reasoning_effort
    : selected?.reasoning_efforts?.[0]
  const llmConfig = selected?.provider && selected.model_id
    ? workLLMConfigFromSelection({ provider: selected.provider, modelId: selected.model_id, reasoningEffort })
    : undefined
  // Created server-side: the server records this user as the crew's owner.
  const created = await agentApi.createOwnCrewProject(WORK_PROFILE_ID, {
    title: title.trim(),
    description: description.trim(),
    identity: {
      name: title.trim(),
      icon: icon?.trim() || Array.from(title.trim())[0]?.toLocaleUpperCase() || 'C',
    },
    llm_config: llmConfig,
  })
  const project = workSessionFromManifests(created)
  if (!project) throw new Error('The new Crew could not be read back.')
  await agentApi.createPlannerFolder(
    `${project.workspacePath}/code`,
    `Initialize Work project code folder ${project.title}`,
  )
  return project
}

export async function deleteWorkSession(session: WorkSession): Promise<void> {
  if (session.shared) throw new Error('Shared Crew projects can only be deleted by their owner.')
  await agentApi.deleteAgentProfileProject(WORK_PROFILE_ID, session.id)
}

export function sharedProjectToWorkSession(row: SharedProjectSummary): WorkSession {
  const llm = row.llm?.provider && row.llm.model_id
    ? {
      schema_version: 2,
      mode: 'explicit',
      builder_llm: {
        provider: row.llm.provider,
        model_id: row.llm.model_id,
        ...(row.llm.reasoning_effort ? { options: { reasoning_effort: row.llm.reasoning_effort } } : {}),
      },
    } as PresetLLMConfig
    : undefined
  return {
    schemaVersion: 1,
    product: WORK_PROFILE_ID,
    id: row.id,
    title: row.title || 'Untitled Crew',
    description: row.description || '',
    identity: row.icon || row.name ? { icon: row.icon || undefined, name: row.name || undefined } : undefined,
    // No session binding: opening a shared Crew resolves the reader's own
    // conversation server-side, never the owner's live session.
    sessionId: '',
    workspacePath: row.workspace_path,
    createdAt: row.created_at || '',
    updatedAt: row.updated_at || '',
    llmConfig: llm,
    selectedServers: row.selected_servers || [],
    selectedSkills: row.selected_skills || [],
    selectedSecrets: row.selected_secrets || [],
    selectedGlobalSecrets: row.selected_global_secrets || [],
    workflowContextPaths: row.workflow_context_paths || [],
    // Shared rows arrive fully formed. Marking every config initialized
    // keeps the owned-project migration path from ever writing to
    // another owner's manifests.
    selectionConfigInitialized: true,
    secretSelectionInitialized: true,
    runtimeConfigInitialized: true,
    shared: {
      ownerId: row.owner_id,
      ownerUsername: row.owner_username || undefined,
      triggers: row.triggers || [],
      schedules: row.schedules || [],
    },
  }
}

export async function loadSharedWorkSessions(): Promise<WorkSession[]> {
  const response = await agentApi.listSharedProjects(WORK_PROFILE_ID)
  return (response?.projects || []).map(sharedProjectToWorkSession)
}

/**
 * Owned Crews first, then other owners' Crews (Crew Run mode). A shared row
 * whose id collides with an owned Crew loses: the owned project is the one
 * the reader can open and change. Shared-listing failures degrade to
 * owned-only rather than failing the whole Crew surface.
 */
export async function loadWorkSessionsIncludingShared(): Promise<WorkSession[]> {
  const [owned, shared] = await Promise.all([
    loadWorkSessions(),
    loadSharedWorkSessions().catch(() => [] as WorkSession[]),
  ])
  const ownedIds = new Set(owned.map(session => session.id))
  return [...owned, ...shared.filter(session => !ownedIds.has(session.id))]
}

export async function updateWorkSessionIdentity(session: WorkSession, patch: ProductIdentityPatch): Promise<WorkSession> {
  if (session.shared) throw new Error('Only the Crew owner can change this.')
  return updateProductProjectIdentity(session, patch, `Update Crew project identity ${session.title}`)
}
