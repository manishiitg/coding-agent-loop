import type { CatalogEntry, Connection } from '../../services/connectionsApi'

export type ConnectionFilter = 'all' | 'connected' | 'not_connected'

/** One table row, whether it comes from the catalog, a connection, or both. */
export interface ConnectionRowModel {
  id: string
  name: string
  entry?: CatalogEntry
  connection?: Connection
}

/**
 * Merges the curated catalog with the servers the user has actually
 * provisioned. The catalog defines the shelf; anything provisioned outside it
 * (added through Custom MCP) is appended so nothing the agent can reach is
 * hidden from the person responsible for it.
 */
export function buildRows(
  catalog: CatalogEntry[],
  connections: Connection[]
): ConnectionRowModel[] {
  const connectionById = new Map(connections.map(c => [c.id, c]))
  const catalogIds = new Set(catalog.map(e => e.id))

  const fromCatalog: ConnectionRowModel[] = catalog.map(entry => ({
    id: entry.id,
    name: entry.name,
    entry,
    connection: connectionById.get(entry.id),
  }))

  const custom: ConnectionRowModel[] = connections
    .filter(c => !catalogIds.has(c.id))
    .map(c => ({ id: c.id, name: c.name, connection: c }))

  return [...fromCatalog, ...custom].sort((a, b) => a.name.localeCompare(b.name))
}

/**
 * Applies the All / Connected / Not connected tabs and the search box.
 * "Connected" means healthy right now — an expired sign-in belongs under
 * "Not connected", where the Reconnect action lives.
 */
export function filterRows(
  rows: ConnectionRowModel[],
  filter: ConnectionFilter,
  query: string
): ConnectionRowModel[] {
  const q = query.trim().toLowerCase()

  return rows.filter(row => {
    const connected = row.connection?.health === 'connected'
    if (filter === 'connected' && !connected) return false
    if (filter === 'not_connected' && connected) return false
    if (!q) return true
    return (
      row.name.toLowerCase().includes(q) ||
      (row.entry?.tagline ?? '').toLowerCase().includes(q) ||
      (row.entry?.category ?? '').toLowerCase().includes(q)
    )
  })
}

/** Picks the featured entries, keeping the requested order and skipping gaps. */
export function pickPopular(catalog: CatalogEntry[], ids: string[]): CatalogEntry[] {
  const byId = new Map(catalog.map(e => [e.id, e]))
  return ids.map(id => byId.get(id)).filter((e): e is CatalogEntry => Boolean(e))
}
