import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {appCopy} from '../src/app-copy'
import {saveReasonMessage} from '../src/save-reason'
const clean:(()=>void)[]=[]
afterEach(()=>{clean.splice(0).forEach(f=>f());vi.useRealTimers()})
async function settle(){for(let i=0;i<40;i++){await Promise.resolve();await nextTick()}}
async function fixture(){
 const host=mockHost(),original=host.ctx.call;let listBusy=false
 host.ctx.call=async(m:string,p:any)=>{if(listBusy&&m==='gopeed.request'&&p.path==='/api/v1/tasks')throw {code:'engine_busy'};return original(m,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 const click=async(label:string)=>{[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!.click();await settle()}
 return {host,root,click,busy:(v:boolean)=>listBusy=v}
}
test('the settings page does not misreport unrelated engine polling as confirmation failure',async()=>{
 vi.useFakeTimers();const {root,click,busy}=await fixture();await click('Settings');busy(true)
 await vi.advanceTimersByTimeAsync(28000);await settle()
 expect(root.textContent).not.toContain('Could not complete the operation');expect(root.textContent).not.toContain('The core is updating')
})
test('a transient download-list failure is pending and clears after successful readback',async()=>{
 vi.useFakeTimers();const {root,busy}=await fixture();busy(true);await vi.advanceTimersByTimeAsync(5000);await settle()
 expect(root.textContent).not.toContain('Could not complete the operation')
 expect(root.textContent).toContain('Refreshing download status. Refresh to confirm; do not resubmit changes')
 busy(false);await vi.advanceTimersByTimeAsync(5000);await settle()
 expect(root.textContent).not.toContain('Refreshing download status')
})
test('interrupted copying tells its owner to select a destination again',async()=>{
 const host=mockHost(false);host.tasks.splice(0,host.tasks.length,{id:'stopped',status:'done',publication:{state:'save_failed',error:'WRITE_FAILED',delegated:true}})
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).toContain('Save failed');expect(root.textContent).toContain('Choose your destination again before starting a new download. This task will not be replayed')
})

test.each(['scope-changed','grant-revoked','integration-disabled'])('settings cancellation %s guides destination reselection without a failure or engine busy',async(reason)=>{
 const host=mockHost(false);host.tasks.splice(0,host.tasks.length,{id:'canceled',status:'done',publication:{state:'canceled',error:'OPERATION_CANCELED',reason,delegated:true}})
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).toContain('Background saving was canceled by a settings change. Choose your destination again');expect(root.textContent).not.toContain('Save failed');expect(root.textContent).not.toContain('The core is updating')
})
test('paused and cancel-requested protected results explain original-reference handling',async()=>{
 const host=mockHost(false);host.tasks.splice(0,host.tasks.length,{id:'paused',status:'done',publication:{state:'interrupted',error:'EXECUTOR_PAUSED',delegated:true}},{id:'canceling',status:'done',publication:{state:'copying',error:'CANCEL_REQUESTED',delegated:true}})
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).toContain('The background executor stopped. This copy was interrupted and will not be replayed. Ask the administrator to restore background saving before starting a new download')
 expect(root.textContent).toContain('Canceling background saving. Waiting for the original operation; do not submit again');expect(root.textContent).not.toContain('The core is updating')
})
// SDK 0.9.1 removed grant-inactive; nothing produces it.
test.each(['en','zh-TW','zh-CN','ja','ko','de','fr','it','es'])('grant-inactive has no own message in %s',locale=>{
 expect(saveReasonMessage('grant-inactive',locale)).toBe(appCopy(locale,'reason.unknown'))
})
