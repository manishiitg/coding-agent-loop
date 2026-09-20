// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { SecretSelectionSection } from './SecretSelectionSection'
import { secretsApi } from '../../api/secrets'

const mocks = vi.hoisted(() => ({ admin: true, fetch: vi.fn().mockResolvedValue(undefined) }))
vi.mock('../../stores/useAuthStore', () => ({ useAuthStore: (selector: (state: unknown) => unknown) => selector({user:{is_admin:mocks.admin},isMultiUserMode:true,isMultiUserModeChecked:true}) }))
vi.mock('../../hooks/useCanWriteWorkflow', () => ({useCanWriteWorkflow:()=>true, READ_ONLY_TITLE:'Read only'}))
vi.mock('../../api/secrets', () => ({secretsApi:{promoteWorkflowSecret:vi.fn(),saveGlobalSecret:vi.fn(),deleteGlobalSecret:vi.fn(),decrypt:vi.fn(),revealGlobalSecret:vi.fn()}}))
vi.mock('../../stores', () => {
 const state={secrets:[],globalSecrets:[{name:'GLOBAL_TOKEN',managed:true},{name:'ENV_TOKEN'}],storedUserSecrets:[],workflowSecretsByPath:{'Workflow/test':[{name:'LOCAL_TOKEN',encrypted_value:'cipher'}]},fetchGlobalSecrets:mocks.fetch,fetchStoredUserSecrets:mocks.fetch,fetchWorkflowSecrets:mocks.fetch,addWorkflowSecret:vi.fn(),removeWorkflowSecret:vi.fn()}
 return {useSecretsStore:(selector:(state:unknown)=>unknown)=>selector(state)}
})
Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true})
const cleanups:(()=>void)[]=[]
beforeEach(()=>{mocks.admin=true})
afterEach(()=>{cleanups.splice(0).forEach(fn=>fn());vi.clearAllMocks()})
async function mount(extraProps: Record<string, unknown> = {}){
 const host=document.createElement('div');document.body.append(host);const root=createRoot(host)
 await act(async()=>root.render(<SecretSelectionSection selectedSecrets={['LOCAL_TOKEN']} onSecretChange={()=>{}} onGlobalSecretChange={()=>{}} workflowPath="Workflow/test" {...extraProps}/>))
 cleanups.push(()=>{act(()=>root.unmount());host.remove()});return host
}
// The shared confirmation dialog portals to document.body, outside the mount
// host. Its buttons carry no aria-label, unlike the row action buttons.
function dialogButton(text: string){
 const found=Array.from(document.querySelectorAll('button')).find(b=>b.textContent===text && !b.getAttribute('aria-label'))
 expect(found).not.toBeUndefined()
 return found as HTMLButtonElement
}
it('promotes by name and source without revealing the value',async()=>{
 const host=await mount()
 const button=host.querySelector('button[aria-label="Make LOCAL_TOKEN global"]') as HTMLButtonElement
 expect(button).not.toBeNull()
 await act(async()=>button.click())
 expect(document.body.textContent).toContain('Make LOCAL_TOKEN global?')
 expect(document.body.textContent).toContain('available server-wide to all users and workflows')
 await act(async()=>dialogButton('Make global').click())
 expect(secretsApi.promoteWorkflowSecret).toHaveBeenCalledWith('Workflow/test','LOCAL_TOKEN')
 expect(secretsApi.decrypt).not.toHaveBeenCalled()
 expect(host.textContent).toContain('LOCAL_TOKEN is global')
 expect(host.querySelector('button[aria-label="Delete global GLOBAL_TOKEN"]')).not.toBeNull()
 expect(host.querySelector('button[aria-label="Delete global ENV_TOKEN"]')).toBeNull()
})
it('hides server-wide management from ordinary workflow owners',async()=>{
 mocks.admin=false;const host=await mount()
 expect(host.textContent).toContain('GLOBAL_TOKEN')
 expect(host.querySelector('button[aria-label="Make LOCAL_TOKEN global"]')).toBeNull()
 expect(host.querySelector('button[aria-label="Delete global GLOBAL_TOKEN"]')).toBeNull()
 expect(host.querySelector('button[aria-label="Reveal global GLOBAL_TOKEN"]')).toBeNull()
})
it('reveals a global value inline for admins',async()=>{
 const host=await mount()
 vi.mocked(secretsApi.revealGlobalSecret).mockResolvedValue({value:'shh-global'})
 expect(host.querySelector('button[aria-label="Reveal global ENV_TOKEN"]')).not.toBeNull()
 await act(async()=> (host.querySelector('button[aria-label="Reveal global GLOBAL_TOKEN"]') as HTMLButtonElement).click())
 expect(secretsApi.revealGlobalSecret).toHaveBeenCalledWith('GLOBAL_TOKEN')
 expect(host.textContent).toContain('shh-global')
 await act(async()=> (host.querySelector('button[aria-label="Reveal global GLOBAL_TOKEN"]') as HTMLButtonElement).click())
 expect(host.textContent).not.toContain('shh-global')
})
it('does not promote when the admin cancels the scope confirmation',async()=>{
 const host=await mount()
 await act(async()=> (host.querySelector('button[aria-label="Make LOCAL_TOKEN global"]') as HTMLButtonElement).click())
 await act(async()=>dialogButton('Cancel').click())
 expect(secretsApi.promoteWorkflowSecret).not.toHaveBeenCalled()
 expect(document.body.textContent).not.toContain('Make LOCAL_TOKEN global?')
})
it('shows a badge only on global rows',async()=>{
 const host=await mount({workspaceSecretHeading:'Box secrets'})
 expect(host.textContent).toContain('Box secrets')
 expect(host.textContent).toContain('Global')
 expect(host.textContent).not.toContain('Automation')
 expect(host.textContent).not.toContain('Project')
})
