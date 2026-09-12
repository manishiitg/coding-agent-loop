import { describe, expect, it } from 'vitest'
import { mergeRemoteCapabilities } from './workflowCapabilitiesSync'

describe('workflow capability refresh', () => {
  it('replaces the deleted duplicate with catalog Notion from the server', () => {
    const loaded = { selected_servers: ['Jam', 'NotionReconnect'], selected_tools: ['NotionReconnect:*'], selected_skills: [] }
    const remote = { ...loaded, selected_servers: ['Jam', 'Notion'], selected_tools: ['Notion:*'] }
    expect(mergeRemoteCapabilities({ ...loaded }, loaded, remote)).toEqual(remote)
    expect(loaded.selected_servers).toEqual(['Jam', 'NotionReconnect'])
  })
  it('keeps unsaved edits while refreshing fields changed by the agent', () => {
    const loaded = { selected_servers: ['Jam', 'NotionReconnect'], selected_tools: [], selected_skills: ['old-skill'] }
    const draft = { ...loaded, selected_skills: ['new-skill'] }
    const remote = { ...loaded, selected_servers: ['Jam', 'Notion'] }
    expect(mergeRemoteCapabilities(draft, loaded, remote)).toEqual({ ...remote, selected_skills: ['new-skill'] })
  })
  it('preserves the user’s unsaved server selection', () => {
    const loaded = { selected_servers: ['Jam'], selected_tools: [] }
    const draft = { ...loaded, selected_servers: ['Jam', 'Notion'] }
    const remote = { ...loaded, selected_servers: ['Jam', 'Canva'] }
    expect(mergeRemoteCapabilities(draft, loaded, remote).selected_servers).toEqual(['Jam', 'Notion'])
  })
})
