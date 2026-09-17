import { getWorkspaceScopedStorageKey } from '../stores/useWorkspaceConnectionStore'

// Captured by every asynchronous chat operation. An identity change invalidates
// callbacks even when the next account happens to have a tab with the same ID.
let generation = 0
export function captureChatIdentity(): number { return generation }
export function isChatIdentityCurrent(captured: number): boolean { return captured === generation }
export function invalidateChatIdentity(): void { generation += 1 }
export function assertChatIdentityCurrent(captured: number): void {
  if (!isChatIdentityCurrent(captured)) throw new Error('Chat account changed; operation cancelled')
}

export function readPersistedChatOwner(): string | null {
  try {
    const raw = globalThis.localStorage?.getItem(getWorkspaceScopedStorageKey('auth-storage'))
    const auth = raw ? JSON.parse(raw).state : null
    return auth?.isAuthenticated && typeof auth.user?.id === 'string' ? auth.user.id : null
  } catch { return null }
}
export function getAccountChatStorageKey(owner: string | null): string {
  return `${getWorkspaceScopedStorageKey('chat-store')}:owner:${owner === null ? 'anonymous' : `user:${encodeURIComponent(owner)}`}`
}
