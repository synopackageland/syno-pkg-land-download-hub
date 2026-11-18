import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const clean:(()=>void)[]=[]
afterEach(()=>{clean.splice(0).forEach(fn=>fn());vi.useRealTimers()})
async function settle(){for(let i=0;i<25;i++){await Promise.resolve();await nextTick()}}
test('pending cancellation explanation clears when a canceled row is removed',async()=>{
 vi.useFakeTimers()
 const host=mockHost(false,'normal1');host.tasks.splice(1);let finished=false;const original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{
  if(method==='gopeed.request'&&p.method==='DELETE'){host.tasks[0].publication={deleting:true,state:'canceling'};return {code:0,data:{deleting:true}}}
  if(method==='gopeed.request'&&p.path==='/api/v1/removals')return {code:0,data:finished?[{id:'active',fileKept:false}]:[]}
  return original(method,p)
 }
 const {root}=mountApp(host.ctx,clean);await settle()
 root.querySelector<HTMLButtonElement>('button[aria-label="Clear download record"]')!.click();await settle()
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Confirm')!.click();await settle()
 expect(root.textContent).toContain('Canceling');finished=true;host.tasks.splice(0)
 await vi.advanceTimersByTimeAsync(5000);await settle()
 expect(root.textContent).not.toContain('Canceling')
})
test('durable cancellation is shown without failed save or replay; committed removal warns that file remains',async()=>{
 const host=mockHost(false,'normal1');host.tasks.splice(0,host.tasks.length,{id:'deleting',status:'done',meta:{opts:{name:'Cancel this.bin'}},publication:{delegated:true,state:'canceling',deleting:true,error:'',published:false}})
 let removed=false;const original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{if(method==='gopeed.request'&&p.path==='/api/v1/removals')return {code:0,data:removed?[{id:'deleting',filename:'Cancel this.bin',fileKept:true}]:[]};return original(method,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).toContain('Canceling');expect(root.textContent).not.toContain('Save failed')
 expect(root.querySelector<HTMLButtonElement>('button[aria-label="Clear download record"]')?.disabled).toBe(true)
 removed=true;host.tasks.splice(0);root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 expect(root.textContent).toContain('The record was cleared. Its saved file stays on the NAS');expect(root.textContent).not.toContain('Canceling')
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toEqual([])
})

test('a removal from before the App opened shows no file-kept notice',async()=>{
 const host=mockHost(false,'normal1');const original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{if(method==='gopeed.request'&&p.path==='/api/v1/removals')return {code:0,data:[{id:'old',filename:'jq-macos-arm64',fileKept:true}]};return original(method,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).not.toContain('has not been deleted')
})

test('the clear-record confirmation says the saved files stay as its description',async()=>{
 const host=mockHost(false,'normal1');host.tasks.splice(1)
 const {root}=mountApp(host.ctx,clean);await settle()
 root.querySelector<HTMLButtonElement>('button[aria-label="Clear download record"]')!.click();await settle()
 const dialog=document.querySelector('[data-ui-dialog]')!
 expect(document.getElementById(dialog.getAttribute('aria-describedby')!)!.textContent?.trim()).toBe('Only the record is cleared. Files already saved to the NAS stay there; delete them in File Station if you no longer need them. A download that is not saved yet stops and its partial data is discarded')
})

test('changed executor pins guide administrators through the SDK reapproval form',async()=>{
 const host=mockHost();let approved=false
 host.ctx.executor.status=async()=>({state:approved?'ready':'needs-admin-approval',reason:'pins-changed'})
 host.ctx.executor.authorize=async()=>{approved=true;return {state:'ready'}}
 const original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{
  if(method==='delegate.status'&&!approved)return {state:'needs-admin-approval',scope_mode:'install-selected',preselection:'home'}
  return original(method,p)
 }
 const {root}=mountApp(host.ctx,clean);await settle()
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Settings')!.click();await settle()
 expect(root.querySelector('input[type="password"]:not([data-github-token])')).not.toBeNull()
 const password=root.querySelector<HTMLInputElement>('input[type="password"]:not([data-github-token])')!;password.value='fixture-only';password.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 const submit=root.querySelector<HTMLButtonElement>('button[type="submit"]')!;expect(submit.disabled).toBe(false)
 root.querySelector('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 expect(approved).toBe(true);expect(root.querySelector('input[type="password"]:not([data-github-token])')).toBeNull();expect(root.querySelector('[data-save-row="folder"]')).not.toBeNull()
})

// the engine may take a while to drop a stalled download; the
// confirmed row leaves the list at once, and returns if the removal fails.
async function removeFirst(root:HTMLElement){
 root.querySelector<HTMLButtonElement>('button[aria-label="Clear download record"]')!.click();await settle()
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Confirm')!.click();await settle()
}
test('a confirmed removal leaves the list before the engine answers',async()=>{
 const host=mockHost(false,'normal1');host.tasks.splice(1);const original=host.ctx.call;let answer=(_:any)=>{}
 host.ctx.call=async(method:string,p:any)=>method==='gopeed.request'&&p.method==='DELETE'?new Promise(resolve=>answer=resolve):original(method,p)
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).toContain('Example download.zip')
 await removeFirst(root)
 expect(root.textContent).not.toContain('Example download.zip')
 host.tasks.splice(0);answer({code:0,data:{deleting:false}});await settle()
 expect(root.textContent).not.toContain('Example download.zip')
})
test('a refused removal brings the row back',async()=>{
 const host=mockHost(false,'normal1');host.tasks.splice(1);const original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{if(method==='gopeed.request'&&p.method==='DELETE')throw {code:'engine_unavailable'};return original(method,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 await removeFirst(root)
 expect(root.textContent).toContain('The download engine is not responding')
 expect(root.textContent).toContain('Example download.zip')
})
