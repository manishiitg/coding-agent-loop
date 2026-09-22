import type { ProductIdentity } from '../../platform/chat/productProjects'

// Role and purpose are required for every Crew project: the agent asks
// for them on first chat, and the identity panel refuses to save without
// them. Icon and name stay optional. Purpose lives in the top-level
// project description, not in the identity block.
export function isWorkIdentityComplete(identity?: ProductIdentity | null, purpose?: string | null): boolean {
  return Boolean(identity?.role?.trim() && purpose?.trim())
}
