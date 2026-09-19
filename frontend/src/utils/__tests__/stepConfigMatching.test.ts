import { describe, expect, it } from 'vitest'
import {
  effectiveExecutionMode,
  effectiveExecutionModeReason,
  effectiveMessageSequenceItems,
  runsAsMessageSequence,
  type PlanStep,
} from '../stepConfigMatching'

// PLAT-287: a step's execution model is decided by its plan type, not by the
// retired agent_configs.declared_execution_mode key. The key may still be
// present until a workflow's v1.0.39 migration strips it, and only one reading
// of it survives: a `regular` step still declaring "agentic" is a legacy
// agentic step the runtime runs as a sequence.
describe('effectiveExecutionMode', () => {
  it('treats a regular step as scripted when no legacy key is present', () => {
    const step: PlanStep = { id: 'fetch', type: 'regular', title: 'Fetch', description: 'Fetch prices.' }
    expect(effectiveExecutionMode(step)).toBe('scripted')
    expect(runsAsMessageSequence(step)).toBe(false)
    expect(effectiveMessageSequenceItems(step)).toEqual([])
  })

  it('keeps a regular step with the legacy "agentic" key as a sequence-run agentic step', () => {
    const step: PlanStep = {
      id: 'judge',
      type: 'regular',
      title: 'Judge',
      description: 'Judge the result.',
      agent_configs: { declared_execution_mode: 'agentic', declared_execution_mode_reason: 'needs judgment' },
    }
    expect(effectiveExecutionMode(step)).toBe('agentic')
    expect(runsAsMessageSequence(step)).toBe(true)
    expect(effectiveMessageSequenceItems(step).map(item => item.id)).toEqual(['execute-and-verify'])
    expect(effectiveExecutionModeReason(step)).toBe('needs judgment')
  })

  it('canonicalises the pre-rename legacy values the way the backend did', () => {
    const codeExec: PlanStep = { id: 'a', type: 'regular', title: 'A', description: 'a', agent_configs: { declared_execution_mode: 'code_exec' } }
    const learnCode: PlanStep = { id: 'b', type: 'regular', title: 'B', description: 'b', agent_configs: { declared_execution_mode: 'learn_code' } }
    expect(effectiveExecutionMode(codeExec)).toBe('agentic')
    expect(effectiveExecutionMode(learnCode)).toBe('scripted')
  })

  it('treats a message_sequence as agentic, whatever the legacy key says', () => {
    const step: PlanStep = {
      id: 'talk',
      type: 'message_sequence',
      title: 'Talk',
      description: 'Talk.',
      items: [],
      agent_configs: { declared_execution_mode: 'scripted' },
    }
    expect(effectiveExecutionMode(step)).toBe('agentic')
    expect(runsAsMessageSequence(step)).toBe(true)
  })

  it('has no execution mode for routing, branch, orchestrator and human-input steps', () => {
    const routing = { id: 'r', type: 'routing', title: 'R', description: 'r', routing_question: 'q', routes: [] } as unknown as PlanStep
    const human = { id: 'h', type: 'human_input', title: 'H', description: 'h', question: 'q' } as unknown as PlanStep
    expect(effectiveExecutionMode(routing)).toBeUndefined()
    expect(effectiveExecutionMode(human)).toBeUndefined()
    expect(effectiveExecutionModeReason(routing)).toBeUndefined()
  })

  it('treats a plan step without a type as the legacy default, regular', () => {
    expect(effectiveExecutionMode({ agent_configs: {} })).toBe('scripted')
    expect(effectiveExecutionMode(undefined)).toBeUndefined()
  })
})
