import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { CodingAgentQuestionCard } from './CodingAgentQuestionCard'
import type { CodingAgentQuestionPrompt } from '../utils/cleanConversation'

const base: CodingAgentQuestionPrompt = {
  provider: 'muse-cli', promptId: 'prompt-1', state: 'pending', answers: [],
  questions: [{ id: 'scope', header: 'Scope', question: 'Choose a scope', multiSelect: false,
    options: [{ label: 'One', description: 'Small' }, { label: 'Two', description: 'Wide' }] }],
}

describe('CodingAgentQuestionCard', () => {
  it('shows Muse single-choice options as radios', () => {
    const html = renderToStaticMarkup(<CodingAgentQuestionCard prompt={base} onAnswer={async () => {}} />)
    expect(html).toContain('Muse needs your choice')
    expect(html).toContain('type="radio"')
    expect(html).toContain('Wide')
  })

  it('shows Claude multi-select options as checkboxes', () => {
    const prompt: CodingAgentQuestionPrompt = {
      ...base, provider: 'claude-code', promptId: 'toolu_123',
      questions: [{ ...base.questions[0], multiSelect: true }],
    }
    const html = renderToStaticMarkup(<CodingAgentQuestionCard prompt={prompt} onAnswer={async () => {}} />)
    expect(html).toContain('Claude needs your choice')
    expect(html).toContain('Select all that apply.')
    expect(html).toContain('type="checkbox"')
  })
})
