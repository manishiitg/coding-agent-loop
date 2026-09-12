// @vitest-environment happy-dom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { agentApi } from '../../../services/api'
import { usePlanData } from './usePlanData'
vi.mock('../../../services/api',()=>({agentApi:{getPlannerFileContent:vi.fn()}}))
Object.assign(globalThis,{IS_REACT_ACT_ENVIRONMENT:true})
it('loads plan/config concurrently, shares requests and ignores a late previous workflow',async()=>{
 const pending=new Map<string,(value:unknown)=>void>()
 vi.mocked(agentApi.getPlannerFileContent).mockImplementation(path=>new Promise(resolve=>pending.set(path,resolve)))
 const results:Record<string,ReturnType<typeof usePlanData>>={}
 function Probe({id,path}:{id:string,path:string}) {results[id]=usePlanData(path);return null}
 const root=createRoot(document.createElement('div'))
 const response=(steps:unknown[])=>({success:true,data:{content:JSON.stringify({steps})}})
 try {
  await act(async()=>root.render(<><Probe id="one" path="Workflow/slow-test"/><Probe id="two" path="Workflow/slow-test"/></>))
  expect(agentApi.getPlannerFileContent).toHaveBeenCalledTimes(2)
  expect(pending.has('Workflow/slow-test/planning/step_config.json')).toBe(true)
  await act(async()=>root.render(<><Probe id="one" path="Workflow/fast-test"/><Probe id="two" path="Workflow/slow-test"/></>))
  await act(async()=>{pending.get('Workflow/fast-test/planning/plan.json')!(response([{id:'fast',type:'regular',title:'Fast'}]));pending.get('Workflow/fast-test/planning/step_config.json')!(response([]))})
  expect(results.one.loading).toBe(false)
  expect(results.one.plan?.steps[0].id).toBe('fast')
  await act(async()=>{pending.get('Workflow/slow-test/planning/plan.json')!(response([{id:'slow',type:'regular',title:'Slow'}]));pending.get('Workflow/slow-test/planning/step_config.json')!(response([]))})
  expect(results.one.plan?.steps[0].id).toBe('fast')
  expect(results.two.loading).toBe(false)
  expect(results.two.plan?.steps[0].id).toBe('slow')
 }finally{act(()=>root.unmount())}
})
