import { expect, it } from 'vitest'
import { nativeAgentToolsEnabled } from './nativeAgentTools'

it('treats native agent tools as on unless explicitly turned off', () => {
  expect(nativeAgentToolsEnabled(undefined)).toBe(true)
  expect(nativeAgentToolsEnabled(true)).toBe(true)
  expect(nativeAgentToolsEnabled(false)).toBe(false)
})
