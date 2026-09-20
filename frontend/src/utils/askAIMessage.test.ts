import { describe, expect, it } from 'vitest'
import { askAIDisplayText, buildAskAIMessage, hasAskAIMessage, parseAskAIMessages } from './askAIMessage'

const BLOCK = buildAskAIMessage({
  view: 'Dashboard',
  summary: 'Help me understand my results page.',
  instructions: 'First read the guide with read_skill(skills=[{"name":"builder-reference"}]).',
})

describe('askAIMessage', () => {
  it('builds a marker block Builder receives in full', () => {
    expect(BLOCK).toContain('[ASK-AI view="Dashboard"]')
    expect(BLOCK).toContain('Help me understand my results page.')
    expect(BLOCK).toContain('read_skill')
  })

  it('parses the user-facing summary away from builder instructions', () => {
    const blocks = parseAskAIMessages(BLOCK)
    expect(blocks).toHaveLength(1)
    expect(blocks[0]).toEqual({
      view: 'Dashboard',
      summary: 'Help me understand my results page.',
      instructions: 'First read the guide with read_skill(skills=[{"name":"builder-reference"}]).',
    })
  })

  it('collapses a block to a friendly one-liner for display', () => {
    expect(askAIDisplayText(BLOCK)).toBe('Ask AI · Dashboard — Help me understand my results page.')
  })

  it('collapses each block independently in a combined queue message', () => {
    const combined = `${BLOCK}\n\n${buildAskAIMessage({ view: 'Costs', summary: 'Why is this expensive?', instructions: 'Use the cost ledger.' })}`
    expect(askAIDisplayText(combined)).toBe(
      'Ask AI · Dashboard — Help me understand my results page.\n\nAsk AI · Costs — Why is this expensive?',
    )
  })

  it('preserves surrounding human text', () => {
    expect(askAIDisplayText(`Wait, one more thing\n\n${BLOCK}`)).toBe(
      'Wait, one more thing\n\nAsk AI · Dashboard — Help me understand my results page.',
    )
  })

  it('leaves ordinary messages unchanged', () => {
    expect(askAIDisplayText('Just a normal message')).toBe('Just a normal message')
    expect(hasAskAIMessage('Just a normal message')).toBe(false)
    expect(parseAskAIMessages('Just a normal message')).toEqual([])
    expect(hasAskAIMessage(BLOCK)).toBe(true)
  })
})
