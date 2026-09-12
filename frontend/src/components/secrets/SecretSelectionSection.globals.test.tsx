// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SecretSelectionSection } from './SecretSelectionSection'
import { secretsApi } from '../../api/secrets'

const mocks = vi.hoisted(() => ({ admin: true, fetch: vi.fn().mockResolvedValue(undefined) }))
vi.mock('../../stores/useAuthStore', () => ({ useAuthStore: (selector: (state: unknown) => unknown) => selector({user:{is_admin:mocks.admin},isMultiUserMode:true,isMultiUserModeChecked:true}) }))
vi.mock('../../hooks/useCanWriteWorkflow', () => ({useCanWriteWorkflow:()=>true, READ_ONLY_TITLE:'Read only'}))
vi.mock('../../api/secrets', () => ({secretsApi:{promoteWorkflowSecret:vi.fn(),saveGlobalSecret:vi.fn(),deleteGlobalSecret:vi.fn(),decrypt:vi.fn()}}))
vi.mock('../../stores', () => {
 const state={secrets:[],globalSecrets:[{name:'GLOBAL_TOKEN',managed:true},{name:'ENV_TOKEN'}],storedUserSecrets:[],workflowSecretsByPath:{'Workflow/test':[{name:'LOCAL_TOKEN',encrypted_value:'cipher'}]},fetchGlobalSecrets:mocks.fetch,fetchStoredUserSecrets:mocks.fetch,fetchWorkflowSecrets:mocks.fetch,addWorkflowSecret:vi.fn(),removeWorkflowSecret:vi.fn()}
 return {useSecretsStore:(selector:(state:unknown)=>unknown)=>selector(state)}
})
Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true})
const cleanups:(()=>void)[]=[]
beforeEach(()=>{mocks.admin=true;vi.stubGlobal('confirm',vi.fn().mockReturnValue(true))})
afterEach(()=>{cleanups.splice(0).forEach(fn=>fn());vi.unstubAllGlobals();vi.clearAllMocks()})
async function mount(){
 const host=document.createElement('div');document.body.append(host);const root=createRoot(host)
 await act(async()=>root.render(<SecretSelectionSection selectedSecrets={['LOCAL_TOKEN']} onSecretChange={()=>{}} onGlobalSecretChange={()=>{}} workflowPath="Workflow/test"/>))
 cleanups.push(()=>{act(()=>root.unmount());host.remove()});return host
}
it('promotes by name and source without revealing the value',async()=>{
 const host=await mount()
 const button=host.querySelector('button[aria-label="Make LOCAL_TOKEN global"]') as HTMLButtonElement
 expect(button).not.toBeNull()
 await act(async()=>button.click())
 expect(window.confirm).toHaveBeenCalledWith(expect.stringContaining('all users and workflows'))
 expect(secretsApi.promoteWorkflowSecret).toHaveBeenCalledWith('Workflow/test','LOCAL_TOKEN')
 expect(secretsApi.decrypt).not.toHaveBeenCalled()
 expect(host.textContent).toContain('LOCAL_TOKEN is global')
 expect(host.querySelector('button[aria-label="Update global GLOBAL_TOKEN"]')).not.toBeNull()
 expect(host.querySelector('button[aria-label="Update global ENV_TOKEN"]')).toBeNull()
})
it('hides server-wide management from ordinary workflow owners',async()=>{
 mocks.admin=false;const host=await mount()
 expect(host.textContent).toContain('GLOBAL_TOKEN')
 expect(host.querySelector('button[aria-label="Make LOCAL_TOKEN global"]')).toBeNull()
 expect(host.querySelector('button[aria-label="Delete global GLOBAL_TOKEN"]')).toBeNull()
})
it('does not promote when the admin cancels the scope confirmation',async()=>{
 vi.mocked(window.confirm).mockReturnValue(false);const host=await mount()
 await act(async()=> (host.querySelector('button[aria-label="Make LOCAL_TOKEN global"]') as HTMLButtonElement).click())
 expect(secretsApi.promoteWorkflowSecret).not.toHaveBeenCalled()
})
