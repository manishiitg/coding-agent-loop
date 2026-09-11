import { describe, expect, it } from 'vitest'
import { stripRetiredLLMFallbacks } from './retiredLLMFallbacks'

describe('retired LLM fallback configuration', () => {
  it('drops legacy chat and tier chains while preserving the chosen provider/model/options', () => {
    const selected = { provider: 'codex-cli', model_id: 'selected', options: { reasoning_effort: 'high' } }
    const legacy = {
      primary: selected,
      fallbacks: [{ provider: 'claude-code', model_id: 'backup' }],
      fallback_models: ['backup'],
      cross_provider_fallback: { provider: 'openai', models: ['backup'] },
      tiered_config: { tier_1: { ...selected, fallbacks: [{ provider: 'pi-cli', model_id: 'other' }] } },
    }
    expect(stripRetiredLLMFallbacks(legacy)).toEqual({ primary: selected, tiered_config: { tier_1: selected } })
    expect(legacy.fallbacks).toHaveLength(1)
  })

  it('preserves optional configs and opaque provider options', () => {
    expect(stripRetiredLLMFallbacks(null)).toBeNull()
    expect(stripRetiredLLMFallbacks(undefined)).toBeUndefined()
    expect(stripRetiredLLMFallbacks({ options: { fallbacks: 'provider-specific option' } })).toEqual({ options: { fallbacks: 'provider-specific option' } })
  })
})
