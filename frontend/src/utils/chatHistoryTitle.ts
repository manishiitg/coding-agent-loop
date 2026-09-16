import type { ChatHistorySession } from '../services/api-types'

export function chatHistorySessionTitle(session: ChatHistorySession, maxLength = 110): string {
  const customTitle = session.title?.replace(/\s+/g, ' ').trim()
  if (customTitle) return customTitle.length > maxLength ? `${customTitle.slice(0, maxLength)}...` : customTitle
  const query = session.query?.replace(/\s+/g, ' ').trim()
  if (query) return query.length > maxLength ? `${query.slice(0, maxLength)}...` : query
  return `${(session.agent_mode || 'chat').replace(/_/g, ' ')} ${session.session_id.slice(0, 8)}`
}
