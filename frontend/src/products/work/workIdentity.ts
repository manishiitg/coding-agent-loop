import type { ProductIdentity } from '../../platform/chat/productProjects'

// Role and instructions are required for every Crew project: the agent asks
// for them on first chat, and the identity panel refuses to save without
// them. Icon and name stay optional.
export function isWorkIdentityComplete(identity?: ProductIdentity | null): boolean {
  return Boolean(identity?.role?.trim() && identity?.instructions?.trim())
}
