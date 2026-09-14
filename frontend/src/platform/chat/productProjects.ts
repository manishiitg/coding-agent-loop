import { agentApi } from '../../services/api'
import type { PresetLLMConfig } from '../../services/api-types'
import { dedupeByFilepath, flattenFiles, responseContent, responseFiles, slugifyTitle } from '../../utils/plannerFiles'

export type ProductProject<P extends string = string> = {
  schemaVersion: 1
  product: P
  id: string
  title: string
  description: string
  sessionId: string
  workspacePath: string
  createdAt: string
  updatedAt: string
  llmConfig?: PresetLLMConfig
  selectedServers: string[]
  selectedSkills: string[]
  workflowContextPaths: string[]
  selectionConfigInitialized: boolean
}

type ProductManifest = {
  schema_version?: unknown
  product?: unknown
  id?: unknown
  title?: unknown
  description?: unknown
  session_id?: unknown
  created_at?: unknown
  updated_at?: unknown
  capabilities?: unknown
}

const asString = (value: unknown): string => typeof value === 'string' ? value.trim() : ''

function parseProductLLMConfig(value: unknown): PresetLLMConfig | undefined {
  if (!value || typeof value !== 'object') return undefined
  const config = value as Partial<PresetLLMConfig>
  if (config.schema_version !== 2 || (config.mode !== 'provider_profile' && config.mode !== 'explicit')) return undefined
  return config as PresetLLMConfig
}

function manifestLLMConfig(raw: ProductManifest): PresetLLMConfig | undefined {
  if (!raw.capabilities || typeof raw.capabilities !== 'object') return undefined
  return parseProductLLMConfig((raw.capabilities as { llm_config?: unknown }).llm_config)
}

function manifestStringList(raw: ProductManifest, key: 'selected_servers' | 'selected_skills' | 'workflow_context_paths'): string[] {
  if (!raw.capabilities || typeof raw.capabilities !== 'object') return []
  const value = (raw.capabilities as Record<string, unknown>)[key]
  if (!Array.isArray(value)) return []
  return [...new Set(value.map(asString).filter(Boolean))]
}

function manifestHasSelectionConfig(raw: ProductManifest): boolean {
  if (!raw.capabilities || typeof raw.capabilities !== 'object') return false
  const capabilities = raw.capabilities as Record<string, unknown>
  return Array.isArray(capabilities.selected_servers) && Array.isArray(capabilities.selected_skills)
}

export function parseProductProjectManifest<P extends string>(
  content: string,
  workspacePath: string,
  product: P,
  lastModified?: string,
): ProductProject<P> | null {
  let raw: ProductManifest
  try {
    raw = JSON.parse(content) as ProductManifest
  } catch {
    return null
  }
  const id = asString(raw.id)
  const title = asString(raw.title)
  const sessionId = asString(raw.session_id)
  if (raw.schema_version !== 1 || raw.product !== product || !id || !title || !sessionId) return null
  const createdAt = asString(raw.created_at) || lastModified || new Date(0).toISOString()
  return {
    schemaVersion: 1,
    product,
    id,
    title,
    description: asString(raw.description),
    sessionId,
    workspacePath,
    createdAt,
    updatedAt: lastModified || asString(raw.updated_at) || createdAt,
    llmConfig: manifestLLMConfig(raw),
    selectedServers: manifestStringList(raw, 'selected_servers'),
    selectedSkills: manifestStringList(raw, 'selected_skills'),
    workflowContextPaths: manifestStringList(raw, 'workflow_context_paths'),
    selectionConfigInitialized: manifestHasSelectionConfig(raw),
  }
}

export async function loadProductProjects<P extends string>(root: string, product: P): Promise<ProductProject<P>[]> {
  const response = await agentApi.getPlannerFiles(root, -1, 2)
  const manifests = dedupeByFilepath(
    flattenFiles(responseFiles(response)).filter((file) => file.type !== 'folder' && file.filepath.endsWith('/product.json')),
  )
  const projects = await Promise.all(manifests.map(async (file) => {
    try {
      const response = await agentApi.getPlannerFileContent(file.filepath)
      const document = responseContent(response)
      if (!document) return null
      return parseProductProjectManifest(
        document.content,
        file.filepath.replace(/\/product\.json$/, ''),
        product,
        document.lastModified || file.last_modified,
      )
    } catch {
      return null
    }
  }))
  return projects
    .filter((project): project is ProductProject<P> => project !== null)
    .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
}

export async function createProductProject<P extends string>(options: {
  root: string
  product: P
  title: string
  description: string
  sessionPrefix: string
  slugFallback: string
  commitLabel: string
  llmConfig?: PresetLLMConfig
}): Promise<ProductProject<P>> {
  const id = globalThis.crypto.randomUUID()
  const title = options.title.trim()
  const description = options.description.trim()
  const sessionId = `${options.sessionPrefix}:${id}`
  const workspacePath = `${options.root}/${slugifyTitle(title, options.slugFallback)}-${id.slice(0, 8)}`
  const now = new Date().toISOString()
  const manifest = {
    schema_version: 1,
    product: options.product,
    id,
    title,
    description,
    session_id: sessionId,
    created_at: now,
    updated_at: now,
    capabilities: {
      selected_servers: [],
      selected_skills: [],
      workflow_context_paths: [],
      ...(options.llmConfig ? { llm_config: options.llmConfig } : {}),
    },
  }
  await agentApi.updatePlannerFile(
    `${workspacePath}/product.json`,
    `${JSON.stringify(manifest, null, 2)}\n`,
    `${options.commitLabel} ${title}`,
  )
  return {
    schemaVersion: 1,
    product: options.product,
    id,
    title,
    description,
    sessionId,
    workspacePath,
    createdAt: now,
    updatedAt: now,
    llmConfig: options.llmConfig,
    selectedServers: [],
    selectedSkills: [],
    workflowContextPaths: [],
    selectionConfigInitialized: true,
  }
}

export async function updateProductProjectLLMConfig<P extends string>(
  project: ProductProject<P>,
  llmConfig: PresetLLMConfig,
  commitLabel: string,
): Promise<ProductProject<P>> {
  const manifestPath = `${project.workspacePath}/product.json`
  const response = await agentApi.getPlannerFileContent(manifestPath)
  const document = responseContent(response)
  if (!document) throw new Error('Project configuration was not found.')

  let manifest: Record<string, unknown>
  try {
    manifest = JSON.parse(document.content) as Record<string, unknown>
  } catch {
    throw new Error('Project configuration is invalid JSON.')
  }
  const capabilities = manifest.capabilities && typeof manifest.capabilities === 'object'
    ? { ...(manifest.capabilities as Record<string, unknown>) }
    : {}
  capabilities.llm_config = llmConfig
  const updatedAt = new Date().toISOString()
  manifest.capabilities = capabilities
  manifest.updated_at = updatedAt
  await agentApi.updatePlannerFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, commitLabel)
  return { ...project, llmConfig, updatedAt }
}

export async function updateProductProjectSelections<P extends string>(
  project: ProductProject<P>,
  patch: { selectedServers?: string[]; selectedSkills?: string[]; workflowContextPaths?: string[] },
  commitLabel: string,
): Promise<ProductProject<P>> {
  const manifestPath = `${project.workspacePath}/product.json`
  const response = await agentApi.getPlannerFileContent(manifestPath)
  const document = responseContent(response)
  if (!document) throw new Error('Project configuration was not found.')

  let manifest: Record<string, unknown>
  try {
    manifest = JSON.parse(document.content) as Record<string, unknown>
  } catch {
    throw new Error('Project configuration is invalid JSON.')
  }
  const capabilities = manifest.capabilities && typeof manifest.capabilities === 'object'
    ? { ...(manifest.capabilities as Record<string, unknown>) }
    : {}
  const normalize = (values: string[]) => [...new Set(values.map(value => value.trim()).filter(Boolean))]
  const selectedServers = patch.selectedServers === undefined ? project.selectedServers : normalize(patch.selectedServers)
  const selectedSkills = patch.selectedSkills === undefined ? project.selectedSkills : normalize(patch.selectedSkills)
  const workflowContextPaths = patch.workflowContextPaths === undefined ? project.workflowContextPaths : normalize(patch.workflowContextPaths)
  capabilities.selected_servers = selectedServers
  capabilities.selected_skills = selectedSkills
  capabilities.workflow_context_paths = workflowContextPaths
  const updatedAt = new Date().toISOString()
  manifest.capabilities = capabilities
  manifest.updated_at = updatedAt
  await agentApi.updatePlannerFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, commitLabel)
  return { ...project, selectedServers, selectedSkills, workflowContextPaths, selectionConfigInitialized: true, updatedAt }
}
