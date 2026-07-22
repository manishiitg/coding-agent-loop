// Shared types used across LearningApp.tsx and the Zustand stores.

export type Screen = 'engine' | 'child' | 'pin' | 'parent' | 'tutor'
export type DrawerTab = 'assets' | 'map' | 'progress' | 'files'

export type ApiEngine = {
  id: string
  name: string
  runtime_command?: string
  runtime_available: boolean
  auth_configured: boolean
  usable: boolean
  setup_hint?: string
  deprecated?: boolean
}

export type ConvMeta = { id: string; title: string; when: string; scope: 'parent' | 'child'; updated: string }

export type ParentMsg = { role: 'user' | 'assistant' | 'tool'; text?: string; tool?: string; name?: string; grade?: string; board?: string; stars?: number; reason?: string; source?: string }
export type StoredMsg = { role: string; text?: string; tool?: string; stars?: number; reason?: string; source?: string }

export type TreeNode = { name: string; path: string; type: 'dir' | 'file'; children?: TreeNode[] }

export type WsFile = { path: string; name: string; scope: string; subject: string; topic: string }

export type ChildSuggestion = { label: string; message: string; emoji?: string; tone?: string; html?: string }

export type LearningPackage = { manifest: string; title: string; items: string[]; guide_note?: string; created_at?: string }

// toParentMsg reconstructs a persisted transcript entry (incl. a celebrate
// event) into what the UI renders — so reloading a conversation replays star
// moments exactly where they happened, not just the surrounding text.
export function toParentMsg(m: StoredMsg): ParentMsg {
  return { role: m.role as ParentMsg['role'], text: m.text, tool: m.tool, stars: m.stars, reason: m.reason, source: m.source }
}
