import { describe, it, expect } from 'vitest'
import { buildRows, filterRows, pickPopular } from './connectionsTableUtils'
import type { CatalogEntry, Connection } from '../../services/connectionsApi'

function entry(over: Partial<CatalogEntry> & { id: string; name: string }): CatalogEntry {
  return {
    server_name: over.id,
    auth: 'dcr',
    transport: 'web',
    setup_required: false,
    ...over,
  }
}

function connection(
  over: Partial<Connection> & { id: string; name: string }
): Connection {
  return {
    server_name: over.id,
    auth: 'dcr',
    transport: 'web',
    health: 'connected',
    custom: false,
    ...over,
  }
}

describe('buildRows', () => {
  it('joins a connection onto its catalog entry instead of listing it twice', () => {
    const rows = buildRows(
      [entry({ id: 'notion', name: 'Notion' })],
      [connection({ id: 'notion', name: 'Notion' })]
    )

    expect(rows).toHaveLength(1)
    expect(rows[0].entry).toBeDefined()
    expect(rows[0].connection).toBeDefined()
  })

  it('lists catalog entries that have not been connected yet', () => {
    const rows = buildRows([entry({ id: 'github', name: 'GitHub' })], [])

    expect(rows).toHaveLength(1)
    expect(rows[0].entry?.id).toBe('github')
    expect(rows[0].connection).toBeUndefined()
  })

  it('appends provisioned servers that are not in the catalog', () => {
    // A server added through Custom MCP must still be visible and manageable.
    const rows = buildRows(
      [entry({ id: 'notion', name: 'Notion' })],
      [connection({ id: 'my-internal-tool', name: 'my-internal-tool', custom: true })]
    )

    expect(rows.map(r => r.id).sort()).toEqual(['my-internal-tool', 'notion'])
    const custom = rows.find(r => r.id === 'my-internal-tool')
    expect(custom?.entry).toBeUndefined()
    expect(custom?.connection?.custom).toBe(true)
  })

  it('sorts rows by name so the table order is stable across refreshes', () => {
    const rows = buildRows(
      [
        entry({ id: 'slack', name: 'Slack' }),
        entry({ id: 'github', name: 'GitHub' }),
        entry({ id: 'notion', name: 'Notion' }),
      ],
      []
    )

    expect(rows.map(r => r.name)).toEqual(['GitHub', 'Notion', 'Slack'])
  })

  it('returns nothing when there is no catalog and nothing connected', () => {
    expect(buildRows([], [])).toEqual([])
  })
})

describe('filterRows', () => {
  const rows = buildRows(
    [
      entry({ id: 'notion', name: 'Notion', tagline: 'Pages and databases' }),
      entry({ id: 'github', name: 'GitHub', tagline: 'Repos and issues' }),
      entry({ id: 'slack', name: 'Slack', category: 'communication' }),
    ],
    [
      connection({ id: 'notion', name: 'Notion', health: 'connected' }),
      connection({ id: 'github', name: 'GitHub', health: 'needs_reconnect' }),
    ]
  )

  it('shows everything under All', () => {
    expect(filterRows(rows, 'all', '')).toHaveLength(3)
  })

  it('counts only healthy connections as Connected', () => {
    const result = filterRows(rows, 'connected', '')
    expect(result.map(r => r.id)).toEqual(['notion'])
  })

  it('puts an expired sign-in under Not connected, where Reconnect lives', () => {
    const result = filterRows(rows, 'not_connected', '')
    expect(result.map(r => r.id).sort()).toEqual(['github', 'slack'])
  })

  it('searches by name, case-insensitively', () => {
    expect(filterRows(rows, 'all', 'notI').map(r => r.id)).toEqual(['notion'])
  })

  it('searches by tagline and category, not just name', () => {
    expect(filterRows(rows, 'all', 'repos').map(r => r.id)).toEqual(['github'])
    expect(filterRows(rows, 'all', 'communication').map(r => r.id)).toEqual(['slack'])
  })

  it('ignores surrounding whitespace in the query', () => {
    expect(filterRows(rows, 'all', '   ').map(r => r.id)).toHaveLength(3)
    expect(filterRows(rows, 'all', '  slack  ').map(r => r.id)).toEqual(['slack'])
  })

  it('combines the tab and the query rather than letting one override the other', () => {
    // "notion" matches, but it is connected, so the Not connected tab excludes it.
    expect(filterRows(rows, 'not_connected', 'notion')).toEqual([])
  })

  it('returns nothing when the query matches no row', () => {
    expect(filterRows(rows, 'all', 'zzzz')).toEqual([])
  })
})

describe('pickPopular', () => {
  const catalog = [
    entry({ id: 'notion', name: 'Notion' }),
    entry({ id: 'slack', name: 'Slack' }),
  ]

  it('keeps the requested order rather than catalog order', () => {
    expect(pickPopular(catalog, ['slack', 'notion']).map(e => e.id)).toEqual([
      'slack',
      'notion',
    ])
  })

  it('skips ids that are not in the catalog instead of rendering gaps', () => {
    expect(pickPopular(catalog, ['slack', 'not-in-catalog', 'notion'])).toHaveLength(2)
  })

  it('returns nothing when the catalog is empty', () => {
    expect(pickPopular([], ['slack'])).toEqual([])
  })
})
