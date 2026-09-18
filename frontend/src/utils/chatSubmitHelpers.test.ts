import { describe, expect, it } from 'vitest'
import type { AgentQueryRequest } from '../services/api-types'
import type { ChatTab } from '../stores/useChatStore'
import { applyAgentProfileBinding, buildAgentProfileChatRequest, remainingWorkflowContextAfterSubmission } from './chatSubmitHelpers'

describe('one-shot workflow references', () => {
  const hdfc = { presetId: 'hdfc', label: 'HDFC', workspacePath: 'Workflow/HDFC' }
  const icici = { presetId: 'icici', label: 'ICICI', workspacePath: 'Workflow/ICICI' }

  it('consumes references captured by an accepted submission', () => {
    expect(remainingWorkflowContextAfterSubmission([hdfc, icici], [hdfc, icici])).toEqual([])
  })

  it('preserves references added while the accepted submission was starting', () => {
    expect(remainingWorkflowContextAfterSubmission([hdfc, icici], [hdfc])).toEqual([icici])
  })

  it('does nothing before a submission has been accepted', () => {
    expect(remainingWorkflowContextAfterSubmission([hdfc], [])).toEqual([hdfc])
  })
})

describe('agent profile query binding', () => {
  it('pins a product query to its profile version and workspace', () => {
    const payload: AgentQueryRequest = { query: 'Make a launch film', agent_mode: 'multi-agent' }
    const tab = {
      name: 'Launch film',
      metadata: {
        mode: 'multi-agent',
        agentProfileId: 'video-studio',
        agentProfileVersion: 1,
        agentProfileWorkspace: 'Chats/Video Studio/projects/launch-film',
        agentProfileProjectTitle: 'Launch film',
        agentProfileWorkspaceDescription: 'A 30 second product launch film.',
      },
    } as ChatTab

    expect(applyAgentProfileBinding(payload, tab)).toEqual({
      ...payload,
      agent_profile_id: 'video-studio',
      agent_profile_version: 1,
      selected_folder: 'Chats/Video Studio/projects/launch-film',
      agent_profile_context: {
        project_title: 'Launch film',
        workspace_description: 'A 30 second product launch film.',
      },
    })
    expect(payload.agent_profile_id).toBeUndefined()
  })

  it('keeps only the product-supported chat extras from the broad request', () => {
    const payload = {
      query: 'Explain today\'s portfolio changes',
      agent_mode: 'multi-agent',
      provider: 'codex-cli',
      model_id: 'gpt-5.6-sol',
      selected_folder: 'a/browser/chosen/path',
      enabled_servers: ['workspace_advanced'],
      selected_skills: ['builder-reference'],
      workflow_context_paths: ['Workflow/customer-research'],
      restored_conversation_path: 'Chats/dominion-history.json',
    } as unknown as AgentQueryRequest

    expect(buildAgentProfileChatRequest(payload, 'project-123')).toEqual({
      message: 'Explain today\'s portfolio changes',
      conversation_key: 'project-123',
      enabled_servers: ['workspace_advanced'],
      selected_skills: ['builder-reference'],
      workflow_context_paths: ['Workflow/customer-research'],
    })
  })

  it('carries the product user\'s chosen engine, and nothing else from the broad payload', () => {
    const payload = { query: 'hello', provider: 'codex-cli', model_id: 'gpt-5.4' } as unknown as AgentQueryRequest
    expect(buildAgentProfileChatRequest(payload, undefined, 'codex-cli')).toEqual({ message: 'hello', engine: 'codex-cli' })
    expect(buildAgentProfileChatRequest(payload, 'key', '')).toEqual({ message: 'hello', conversation_key: 'key' })
  })
})

it('carries a private account into product chat requests',()=>{
 expect(buildAgentProfileChatRequest({query:'hello',connection_id:'account-B'} as Parameters<typeof buildAgentProfileChatRequest>[0],'conversation','codex-cli','gpt-test')).toMatchObject({connection_id:'account-B',engine:'codex-cli'})
})
