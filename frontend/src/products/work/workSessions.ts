import { createProductProject, loadProductProjects, parseProductProjectManifest, updateProductProjectSelections, type ProductProject } from '../../platform/chat/productProjects'
import { agentApi } from '../../services/api'
import { secretsApi } from '../../api/secrets'
import type { LLMProvider, PresetLLMConfig } from '../../services/api-types'
import { slugifyTitle } from '../../utils/plannerFiles'
import { loadAgentProfileProviderOptions } from '../../utils/agentProfileCapabilities'
import { WORK_PROFILE_ID, WORK_PROJECTS_ROOT } from './workData'

export type WorkSession = ProductProject<typeof WORK_PROFILE_ID>

export type WorkLLMSelection = {
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
  return { provider: builder.provider, modelId: builder.model_id, reasoningEffort }
}

export function sessionSlug(title: string): string {
  return slugifyTitle(title, 'workspace')
}

export function parseSessionManifest(content: string, workspacePath: string, lastModified?: string): WorkSession | null {
  return parseProductProjectManifest(content, workspacePath, WORK_PROFILE_ID, lastModified)
}

export async function loadWorkSessions(): Promise<WorkSession[]> {
  const sessions = await loadProductProjects(WORK_PROJECTS_ROOT, WORK_PROFILE_ID, { runtimeManifestName: 'workflow.json' })
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

export async function createWorkSession(title: string, description: string): Promise<WorkSession> {
  const options = await loadAgentProfileProviderOptions(WORK_PROFILE_ID)
  const selected = options.find(option => option.default) || options[0]
  const reasoningEffort = typeof selected?.options?.reasoning_effort === 'string'
    ? selected.options.reasoning_effort
    : selected?.reasoning_efforts?.[0]
  const llmConfig = selected?.provider && selected.model_id
    ? workLLMConfigFromSelection({ provider: selected.provider, modelId: selected.model_id, reasoningEffort })
    : undefined
  const project = await createProductProject({
    root: WORK_PROJECTS_ROOT,
    product: WORK_PROFILE_ID,
    title,
    description,
    sessionPrefix: 'work:project',
    slugFallback: 'workspace',
    commitLabel: 'Create Work project',
    llmConfig,
    runtimeManifestName: 'workflow.json',
  })
  await agentApi.createPlannerFolder(
    `${project.workspacePath}/code`,
    `Initialize Work project code folder ${project.title}`,
  )
  return project
}
