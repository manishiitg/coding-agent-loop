import { describe, expect, it } from 'vitest'

import {
  normalizeServerAlias,
  serverNamesMatch,
  isSelectedServer,
  toolBelongsToServer,
  hasServerTool,
  dedupeServerNames,
  toggleServerSelection,
} from './mcpServerAlias'

describe('normalizeServerAlias', () => {
  it('collapses underscores to hyphens', () => {
    expect(normalizeServerAlias('google_sheets')).toBe('google-sheets')
  })

  it('leaves an already-canonical hyphenated name unchanged', () => {
    expect(normalizeServerAlias('google-sheets')).toBe('google-sheets')
  })

  it('trims whitespace', () => {
    expect(normalizeServerAlias('  google_sheets  ')).toBe('google-sheets')
  })
})

describe('serverNamesMatch', () => {
  it('matches a hyphenated name against its underscore alias', () => {
    expect(serverNamesMatch('google-sheets', 'google_sheets')).toBe(true)
  })

  it('does not match genuinely different server names', () => {
    expect(serverNamesMatch('google-sheets', 'google-drive')).toBe(false)
  })
})

describe('isSelectedServer', () => {
  it('recognizes the canonical spelling as selected when only the legacy spelling is stored (PLAT-169 root cause)', () => {
    expect(isSelectedServer(['google_sheets'], 'google-sheets')).toBe(true)
  })

  it('returns false when the server is not present in any spelling', () => {
    expect(isSelectedServer(['slack'], 'google-sheets')).toBe(false)
  })
})

describe('toolBelongsToServer', () => {
  it('matches a tool whose prefix is the legacy spelling against the canonical server name', () => {
    expect(toolBelongsToServer('google_sheets:*', 'google-sheets')).toBe(true)
  })

  it('does not match a tool with no server prefix at all', () => {
    expect(toolBelongsToServer('no-colon-here', 'google-sheets')).toBe(false)
  })

  it('does not match a genuinely different server prefix', () => {
    expect(toolBelongsToServer('slack:send_message', 'google-sheets')).toBe(false)
  })
})

describe('hasServerTool', () => {
  it('finds an exact tool under a legacy-spelled server prefix', () => {
    expect(hasServerTool(['google_sheets:read_range'], 'google-sheets', 'read_range')).toBe(true)
  })

  it('does not match the same server with a different tool name', () => {
    expect(hasServerTool(['google_sheets:read_range'], 'google-sheets', 'write_range')).toBe(false)
  })
})

describe('dedupeServerNames', () => {
  it('collapses an alias-equivalent duplicate, keeping the first occurrence — the exact case that blocked the reported save', () => {
    expect(dedupeServerNames(['google-sheets', 'google_sheets'])).toEqual(['google-sheets'])
  })

  it('keeps the first spelling even when the legacy one is listed first', () => {
    expect(dedupeServerNames(['google_sheets', 'google-sheets'])).toEqual(['google_sheets'])
  })

  it('leaves a list with no duplicates unchanged', () => {
    expect(dedupeServerNames(['google-sheets', 'slack', 'notion'])).toEqual(['google-sheets', 'slack', 'notion'])
  })

  it('is a no-op on an empty list', () => {
    expect(dedupeServerNames([])).toEqual([])
  })
})

describe('toggleServerSelection', () => {
  it('restores one wildcard when a removed server has stale duplicate tool markers', () => {
    expect(toggleServerSelection('Linear', ['Resend'], ['Linear:*', 'Resend:*', 'Linear:*'])).toEqual({
      servers: ['Resend', 'Linear'],
      tools: ['Resend:*', 'Linear:*'],
    })
  })

  it('removes the server and its tools together', () => {
    expect(toggleServerSelection('Linear', ['Resend', 'Linear'], ['Resend:*', 'Linear:*'])).toEqual({
      servers: ['Resend'],
      tools: ['Resend:*'],
    })
  })
})
