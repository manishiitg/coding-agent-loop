import { agentApi } from '../../services/api'
import type { PresetLLMConfig } from '../../services/api-types'
import { dedupeByFilepath, flattenFiles, responseContent, responseFiles, slugifyTitle } from '../../utils/plannerFiles'

export type ProductProject<P extends string = string> = {
  schemaVersion: 1
  product: P
  id: string
  title: string
  description: string
  identity?: ProductIdentity
  sessionId: string
  workspacePath: string
  createdAt: string
  updatedAt: string
  llmConfig?: PresetLLMConfig
  selectedServers: string[]
  selectedSkills: string[]
  selectedSecrets: string[]
  workflowContextPaths: string[]
  selectionConfigInitialized: boolean
  secretSelectionInitialized: boolean
  runtimeConfigInitialized: boolean
}

export type ProductIdentity = {
  icon?: string
  name?: string
  role?: string
  instructions?: string
}

type ProductManifest = {
  schema_version?: unknown
  product?: unknown
  id?: unknown
  title?: unknown
  description?: unknown
  identity?: unknown
  session_id?: unknown
  created_at?: unknown
  updated_at?: unknown
  capabilities?: unknown
  workflow_context_paths?: unknown
}

const asString = (value: unknown): string => typeof value === 'string' ? value.trim() : ''

function parseProductIdentity(value: unknown): ProductIdentity | undefined {
  if (!value || typeof value !== 'object') return undefined
  const raw = value as Record<string, unknown>
  const identity = {
    icon: asString(raw.icon),
    name: asString(raw.name),
    role: asString(raw.role),
    instructions: asString(raw.instructions),
  }
  return Object.values(identity).some(Boolean) ? identity : undefined
}

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

function manifestStringList(raw: ProductManifest, key: 'selected_servers' | 'selected_skills' | 'selected_secrets' | 'workflow_context_paths'): string[] {
  if (key === 'workflow_context_paths' && Array.isArray(raw.workflow_context_paths)) {
    return [...new Set(raw.workflow_context_paths.map(asString).filter(Boolean))]
  }
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

function manifestHasSecretSelection(raw: ProductManifest): boolean {
  return !!raw.capabilities
    && typeof raw.capabilities === 'object'
    && Array.isArray((raw.capabilities as Record<string, unknown>).selected_secrets)
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
    identity: parseProductIdentity(raw.identity),
    sessionId,
    workspacePath,
    createdAt,
    updatedAt: lastModified || asString(raw.updated_at) || createdAt,
    llmConfig: manifestLLMConfig(raw),
    selectedServers: manifestStringList(raw, 'selected_servers'),
    selectedSkills: manifestStringList(raw, 'selected_skills'),
    selectedSecrets: manifestStringList(raw, 'selected_secrets'),
    workflowContextPaths: manifestStringList(raw, 'workflow_context_paths'),
    selectionConfigInitialized: manifestHasSelectionConfig(raw),
    secretSelectionInitialized: manifestHasSecretSelection(raw),
    runtimeConfigInitialized: true,
  }
}

type ProductProjectStorageOptions = {
  runtimeManifestName?: string
}

function applyRuntimeManifest<P extends string>(project: ProductProject<P>, content: string): ProductProject<P> {
  let raw: ProductManifest
  try {
    raw = JSON.parse(content) as ProductManifest
  } catch {
    return project
  }
  return {
    ...project,
    llmConfig: manifestLLMConfig(raw),
    selectedServers: manifestStringList(raw, 'selected_servers'),
    selectedSkills: manifestStringList(raw, 'selected_skills'),
    selectedSecrets: manifestStringList(raw, 'selected_secrets'),
    workflowContextPaths: manifestStringList(raw, 'workflow_context_paths'),
    selectionConfigInitialized: manifestHasSelectionConfig(raw),
    secretSelectionInitialized: manifestHasSecretSelection(raw),
    runtimeConfigInitialized: true,
  }
}

export async function loadProductProjects<P extends string>(root: string, product: P, storage: ProductProjectStorageOptions = {}): Promise<ProductProject<P>[]> {
  const response = await agentApi.getPlannerFiles(root, -1, 2)
  const manifests = dedupeByFilepath(
    flattenFiles(responseFiles(response)).filter((file) => file.type !== 'folder' && file.filepath.endsWith('/product.json')),
  )
  const projects = await Promise.all(manifests.map(async (file) => {
    try {
      const response = await agentApi.getPlannerFileContent(file.filepath)
      const document = responseContent(response)
      if (!document) return null
      const project = parseProductProjectManifest(
        document.content,
        file.filepath.replace(/\/product\.json$/, ''),
        product,
        document.lastModified || file.last_modified,
      )
      if (!project || !storage.runtimeManifestName) return project
      try {
        const runtimeResponse = await agentApi.getPlannerFileContent(`${project.workspacePath}/${storage.runtimeManifestName}`)
        const runtimeDocument = responseContent(runtimeResponse)
        return runtimeDocument ? applyRuntimeManifest(project, runtimeDocument.content) : { ...project, runtimeConfigInitialized: false }
      } catch {
        return { ...project, runtimeConfigInitialized: false }
      }
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
  identity?: ProductIdentity
  runtimeManifestName?: string
}): Promise<ProductProject<P>> {
  const id = globalThis.crypto.randomUUID()
  const title = options.title.trim()
  const description = options.description.trim()
  const sessionId = `${options.sessionPrefix}:${id}`
  const workspacePath = `${options.root}/${slugifyTitle(title, options.slugFallback)}-${id.slice(0, 8)}`
  const now = new Date().toISOString()
  const capabilities: Record<string, unknown> = {
    selected_servers: [],
    selected_tools: [],
    selected_skills: [],
    selected_secrets: [],
    selected_global_secret_names: [],
    browser_mode: 'auto',
    use_code_execution_mode: false,
    ...(options.llmConfig ? { llm_config: options.llmConfig } : {}),
  }
  if (!options.runtimeManifestName) capabilities.workflow_context_paths = []
  const manifest: Record<string, unknown> = {
    schema_version: 1,
    product: options.product,
    id,
    title,
    description,
    session_id: sessionId,
    created_at: now,
    updated_at: now,
    ...(options.identity ? { identity: options.identity } : {}),
  }
  if (!options.runtimeManifestName) manifest.capabilities = capabilities
  if (options.runtimeManifestName) {
    const runtimeManifest = {
      schema_version: 1,
      id,
      label: title,
      capabilities,
      workflow_context_paths: [],
      schedules: [],
      triggers: [],
      created_at: now,
      updated_at: now,
    }
    await agentApi.updatePlannerFile(
      `${workspacePath}/${options.runtimeManifestName}`,
      `${JSON.stringify(runtimeManifest, null, 2)}\n`,
      `${options.commitLabel} runtime ${title}`,
    )
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
    identity: options.identity,
    sessionId,
    workspacePath,
    createdAt: now,
    updatedAt: now,
    llmConfig: options.llmConfig,
    selectedServers: [],
    selectedSkills: [],
    selectedSecrets: [],
    workflowContextPaths: [],
    selectionConfigInitialized: true,
    secretSelectionInitialized: true,
    runtimeConfigInitialized: true,
  }
}

async function readProjectRuntimeManifest<P extends string>(project: ProductProject<P>, runtimeManifestName?: string): Promise<{ path: string; manifest: Record<string, unknown> }> {
  const path = `${project.workspacePath}/${runtimeManifestName || 'product.json'}`
  try {
    const response = await agentApi.getPlannerFileContent(path)
    const document = responseContent(response)
    if (document) return { path, manifest: JSON.parse(document.content) as Record<string, unknown> }
  } catch (cause) {
    if (!runtimeManifestName) throw cause
  }
  const productResponse = await agentApi.getPlannerFileContent(`${project.workspacePath}/product.json`)
  const productDocument = responseContent(productResponse)
  if (!productDocument) throw new Error('Project configuration was not found.')
  const legacy = JSON.parse(productDocument.content) as Record<string, unknown>
  const legacyCapabilities = legacy.capabilities && typeof legacy.capabilities === 'object'
    ? { ...(legacy.capabilities as Record<string, unknown>) }
    : {}
  const legacyWorkflowContextPaths = Array.isArray(legacy.workflow_context_paths)
    ? legacy.workflow_context_paths
    : (Array.isArray(legacyCapabilities.workflow_context_paths) ? legacyCapabilities.workflow_context_paths : [])
  delete legacyCapabilities.workflow_context_paths
  if (!Array.isArray(legacyCapabilities.selected_tools)) legacyCapabilities.selected_tools = []
  if (!Array.isArray(legacyCapabilities.selected_global_secret_names)) legacyCapabilities.selected_global_secret_names = []
  if (typeof legacyCapabilities.browser_mode !== 'string') legacyCapabilities.browser_mode = 'auto'
  if (typeof legacyCapabilities.use_code_execution_mode !== 'boolean') legacyCapabilities.use_code_execution_mode = false
  return {
    path,
    manifest: {
      schema_version: 1,
      id: project.id,
      label: project.title,
      capabilities: legacyCapabilities,
      workflow_context_paths: legacyWorkflowContextPaths,
      schedules: Array.isArray(legacy.schedules) ? legacy.schedules : [],
      triggers: Array.isArray(legacy.triggers) ? legacy.triggers : [],
      created_at: project.createdAt,
      updated_at: project.updatedAt,
    },
  }
}

async function stripLegacyRuntimeFromProduct<P extends string>(project: ProductProject<P>, commitLabel: string): Promise<void> {
  const path = `${project.workspacePath}/product.json`
  const response = await agentApi.getPlannerFileContent(path)
  const document = responseContent(response)
  if (!document) return
  const manifest = JSON.parse(document.content) as Record<string, unknown>
  const hadRuntime = ['capabilities', 'schedules', 'triggers', 'workflow_context_paths'].some(key => key in manifest)
  if (!hadRuntime) return
  delete manifest.capabilities
  delete manifest.schedules
  delete manifest.triggers
  delete manifest.workflow_context_paths
  manifest.updated_at = new Date().toISOString()
  await agentApi.updatePlannerFile(path, `${JSON.stringify(manifest, null, 2)}\n`, `${commitLabel} metadata migration`)
}

export async function updateProductProjectLLMConfig<P extends string>(
  project: ProductProject<P>,
  llmConfig: PresetLLMConfig,
  commitLabel: string,
  runtimeManifestName?: string,
): Promise<ProductProject<P>> {
  try {
    const { path: manifestPath, manifest } = await readProjectRuntimeManifest(project, runtimeManifestName)
    const capabilities = manifest.capabilities && typeof manifest.capabilities === 'object'
      ? { ...(manifest.capabilities as Record<string, unknown>) }
      : {}
    capabilities.llm_config = llmConfig
    const updatedAt = new Date().toISOString()
    manifest.capabilities = capabilities
    manifest.updated_at = updatedAt
    await agentApi.updatePlannerFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, commitLabel)
    if (runtimeManifestName && !project.runtimeConfigInitialized) await stripLegacyRuntimeFromProduct(project, commitLabel)
    return { ...project, llmConfig, runtimeConfigInitialized: true, updatedAt }
  } catch {
    throw new Error('Project configuration is invalid JSON.')
  }
}

export async function updateProductProjectSelections<P extends string>(
  project: ProductProject<P>,
  patch: { selectedServers?: string[]; selectedSkills?: string[]; selectedSecrets?: string[]; workflowContextPaths?: string[] },
  commitLabel: string,
  runtimeManifestName?: string,
): Promise<ProductProject<P>> {
  try {
    const { path: manifestPath, manifest } = await readProjectRuntimeManifest(project, runtimeManifestName)
    const capabilities = manifest.capabilities && typeof manifest.capabilities === 'object'
      ? { ...(manifest.capabilities as Record<string, unknown>) }
      : {}
    const normalize = (values: string[]) => [...new Set(values.map(value => value.trim()).filter(Boolean))]
    const selectedServers = patch.selectedServers === undefined ? project.selectedServers : normalize(patch.selectedServers)
    const selectedSkills = patch.selectedSkills === undefined ? project.selectedSkills : normalize(patch.selectedSkills)
    const selectedSecrets = patch.selectedSecrets === undefined ? project.selectedSecrets : normalize(patch.selectedSecrets)
    const workflowContextPaths = patch.workflowContextPaths === undefined ? project.workflowContextPaths : normalize(patch.workflowContextPaths)
    capabilities.selected_servers = selectedServers
    capabilities.selected_skills = selectedSkills
    capabilities.selected_secrets = selectedSecrets
    if (runtimeManifestName) manifest.workflow_context_paths = workflowContextPaths
    else capabilities.workflow_context_paths = workflowContextPaths
    const updatedAt = new Date().toISOString()
    manifest.capabilities = capabilities
    manifest.updated_at = updatedAt
    await agentApi.updatePlannerFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, commitLabel)
    if (runtimeManifestName && !project.runtimeConfigInitialized) await stripLegacyRuntimeFromProduct(project, commitLabel)
    return { ...project, selectedServers, selectedSkills, selectedSecrets, workflowContextPaths, selectionConfigInitialized: true, secretSelectionInitialized: true, runtimeConfigInitialized: true, updatedAt }
  } catch {
    throw new Error('Project configuration is invalid JSON.')
  }
}
