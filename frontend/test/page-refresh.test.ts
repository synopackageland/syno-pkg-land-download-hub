import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {lifecycleCopy} from '@synopackageland/sdk'
const clean:(()=>void)[]=[]
afterEach(()=>{clean.splice(0).forEach(f=>f());vi.useRealTimers()})
async function settle(){for(let i=0;i<40;i++){await Promise.resolve();await nextTick()}}
function button(root:HTMLElement,label:string){return [...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!}
test('the footer carries no engine jargon',async()=>{
 const host=mockHost(),{root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).not.toContain('Package account engine')
 expect(root.querySelector('.app-footer')).toBeNull()
})
// SDK 0.9.9 made the example save opt-in; Gopeed has nothing to show with it.
test('background saving offers no example file',async()=>{
 const host=mockHost(),{root}=mountApp(host.ctx,clean);await settle()
 button(root,'Settings').click();await settle()
 expect(root.querySelector('.background-save-settings')).not.toBeNull()
 expect(root.textContent).not.toContain('Save example file')
 expect(root.textContent).not.toContain('example file')
})
// a package stop refuses every read; the next poll after the
// restart clears the refusal and rereads the page without a reload.
function stoppableEngine(){
 const host=mockHost(),original=host.ctx.call;let stopped=false,maxRunning=2
 host.ctx.call=async(m:string,p:any)=>{
  if(stopped&&(m==='gopeed.request'||m.startsWith('gopeed.settings')))throw {code:'engine_unavailable'}
  if(m==='gopeed.settings.get')return {...await original(m,p),maxRunning}
  return original(m,p)
 }
 return {host,stop(){stopped=true},start(value=maxRunning){stopped=false;maxRunning=value}}
}
test('the engine refusal clears on its own once the engine answers again',async()=>{
 vi.useFakeTimers()
 const engine=stoppableEngine(),{root}=mountApp(engine.host.ctx,clean);await settle()
 engine.stop();await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).toContain('The download engine is not responding')
 engine.start();await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).not.toContain('The download engine is not responding')
 expect(root.textContent).toContain('Example download.zip')
})
test('settings read while the engine was stopped are read again once it answers',async()=>{
 vi.useFakeTimers()
 const engine=stoppableEngine(),{root}=mountApp(engine.host.ctx,clean);await settle()
 engine.stop();button(root,'Settings').click();await settle()
 engine.start(5);await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.querySelector<HTMLInputElement>('[data-section="core"] input[type="number"]')!.value).toBe('5')
})
test('an action refusal stays until the next action',async()=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call,{root}=mountApp(host.ctx,clean);await settle()
 host.ctx.call=async(m:string,p:any)=>{if(m==='gopeed.request'&&p.method==='PUT')throw {code:'engine_unavailable'};return original(m,p)}
 root.querySelector<HTMLButtonElement>('button[aria-label="Pause"]')!.click();await settle()
 await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).toContain('The download engine is not responding')
})
// Extensions opened while the engine starts (engine_busy) reads the
// list, its update checks and the catalog again once the engine answers.
test('the extensions page reads everything again once a starting engine answers',async()=>{
 vi.useFakeTimers()
 const host=mockHost(true),original=host.ctx.call;let starting=true
 host.ctx.call=async(m:string,p:any)=>{if(starting&&(m==='gopeed.request'||m.startsWith('gopeed.plugins')))throw {code:'engine_busy'};return original(m,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 button(root,'Extensions').click();await settle()
 expect(root.textContent).not.toContain('Available version: 1.1.0')
 starting=false;await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).toContain('Current version: 1.0.0')
 expect(root.textContent).toContain('Available version: 1.1.0')
})
// SDK 0.10.6: a stopped or starting package is told in the SDK's words as
// information, not as a failure, and read again on the SDK's schedule
// (2 s, doubling) instead of every poll.
function lifecycleEngine(code:string){
 const host=mockHost(),original=host.ctx.call;let refusing=false,reads=0
 host.ctx.call=async(m:string,p:any)=>{
  if(m==='gopeed.request'&&p.path==='/api/v1/tasks')reads++
  if(refusing&&m==='gopeed.request')throw {code}
  return original(m,p)
 }
 return {host,get reads(){return reads},refuse(){refusing=true},answer(){refusing=false}}
}
test.each([['APP_STOPPED','stopped'],['APP_STARTING','starting'],['EXECUTOR_STARTING','starting']] as const)('a %s package says so in the SDK\'s words and reads again by itself',async(code,lifecycle)=>{
 vi.useFakeTimers()
 const engine=lifecycleEngine(code),{root}=mountApp(engine.host.ctx,clean);await settle()
 engine.refuse();await vi.advanceTimersByTimeAsync(4000);await settle()
 const status=[...root.querySelectorAll('[role="status"]')].map(x=>x.textContent)
 expect(status).toContain(lifecycleCopy(lifecycle,'en'))
 expect(root.querySelector('[role="alert"]')).toBeNull()
 engine.answer();await vi.advanceTimersByTimeAsync(2000);await settle()
 expect(root.textContent).not.toContain(lifecycleCopy(lifecycle,'en'))
 expect(root.textContent).toContain('Example download.zip')
})
test('a stopped package is read again after 2 s, then 4 s, not on every poll',async()=>{
 vi.useFakeTimers()
 const engine=lifecycleEngine('APP_STOPPED'),{root}=mountApp(engine.host.ctx,clean);await settle()
 engine.refuse();await vi.advanceTimersByTimeAsync(4000);await settle()
 const refused=engine.reads
 await vi.advanceTimersByTimeAsync(1999);expect(engine.reads).toBe(refused)
 await vi.advanceTimersByTimeAsync(1);await settle();expect(engine.reads).toBe(refused+1)
 await vi.advanceTimersByTimeAsync(3999);expect(engine.reads).toBe(refused+1)
 await vi.advanceTimersByTimeAsync(1);await settle();expect(engine.reads).toBe(refused+2)
 expect(root.textContent).toContain(lifecycleCopy('stopped','en'))
})
// a refusal that is not the package's lifecycle replaces the
// stopped copy; the page never says both that the App is stopped and that
// the engine does not respond.
test('an engine refusal after a stop replaces the stopped copy',async()=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call;let code=''
 host.ctx.call=async(m:string,p:any)=>{if(code&&m==='gopeed.request')throw {code};return original(m,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 code='APP_STOPPED';await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).toContain(lifecycleCopy('stopped','en'))
 code='engine_unavailable';await vi.advanceTimersByTimeAsync(2000);await settle()
 expect(root.querySelector('[role="alert"]')?.textContent).toContain('The download engine is not responding')
 expect(root.textContent).not.toContain(lifecycleCopy('stopped','en'))
})
// SDK 0.10.7: an action a stopped package refused says to try again once it
// runs; only the page's own reads say the page updates by itself.
test('a pause refused by a stopped package uses the SDK action wording',async()=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call,{root}=mountApp(host.ctx,clean);await settle()
 host.ctx.call=async(m:string,p:any)=>{if(m==='gopeed.request'&&p.method==='PUT')throw {code:'APP_STOPPED'};return original(m,p)}
 root.querySelector<HTMLButtonElement>('button[aria-label="Pause"]')!.click();await settle()
 expect(root.textContent).toContain(lifecycleCopy('stopped','en',{for:'action'}))
 expect(root.textContent).not.toContain(lifecycleCopy('stopped','en'))
})
// A starting package is read again on the SDK's starting schedule: 2 s,
// then at most 4 s, not up to 15 s as for a stopped one.
test('a starting package is read again at most every 4 s',async()=>{
 vi.useFakeTimers()
 const engine=lifecycleEngine('APP_STARTING'),{root}=mountApp(engine.host.ctx,clean);await settle()
 engine.refuse();await vi.advanceTimersByTimeAsync(4000);await settle()
 const refused=engine.reads
 await vi.advanceTimersByTimeAsync(2000);await settle();expect(engine.reads).toBe(refused+1)
 await vi.advanceTimersByTimeAsync(4000);await settle();expect(engine.reads).toBe(refused+2)
 await vi.advanceTimersByTimeAsync(3999);expect(engine.reads).toBe(refused+2)
 await vi.advanceTimersByTimeAsync(1);await settle();expect(engine.reads).toBe(refused+3)
 expect(root.textContent).toContain(lifecycleCopy('starting','en'))
})

test.each([{code:'executor_starting'},{code:'APP_STARTING'},{reason:'APP_STARTING: the executor status request failed.'}])('the page follows the SDK executor lifecycle %j even while download polls still say stopped',async(starting)=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call,listeners:((status:any)=>void)[]=[]
 let stopped=false
 host.ctx.executor.subscribe=(listener:(status:any)=>void)=>listeners.push(listener)
 host.ctx.call=async(m:string,p:any)=>{if(stopped&&m==='gopeed.request')throw {code:'APP_STOPPED'};return original(m,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 stopped=true;await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).toContain(lifecycleCopy('stopped','en'))
 listeners.forEach(listener=>listener({state:'blocked',...starting}));await settle()
 expect(root.textContent).toContain(lifecycleCopy('starting','en'))
 expect(root.textContent).not.toContain(lifecycleCopy('stopped','en'))
 await vi.advanceTimersByTimeAsync(8000);await settle()
 expect(root.textContent).not.toContain(lifecycleCopy('stopped','en'))
 stopped=false;listeners.forEach(listener=>listener({state:'ready'}));await settle()
 expect(root.textContent).not.toContain(lifecycleCopy('starting','en'))
 expect(root.textContent).not.toContain(lifecycleCopy('stopped','en'))
 await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).not.toContain(lifecycleCopy('starting','en'))
 expect(root.textContent).toContain('Example download.zip')
})

test('a starting refusal from an action also updates the page banner',async()=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call,{root}=mountApp(host.ctx,clean);await settle()
 host.ctx.call=async(m:string,p:any)=>{if(m==='gopeed.request'&&p.method==='PUT')throw {code:'APP_STARTING'};return original(m,p)}
 root.querySelector<HTMLButtonElement>('button[aria-label="Pause"]')!.click();await settle()
 expect(root.textContent).toContain(lifecycleCopy('starting','en',{for:'action'}))
 expect(root.textContent).toContain(lifecycleCopy('starting','en'))
 await vi.advanceTimersByTimeAsync(4000);await settle()
 expect(root.textContent).not.toContain(lifecycleCopy('starting','en'))
})

// The settings SDK handles this error internally; the page still observes it.
test('a starting refusal in an SDK settings read updates the page banner',async()=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call,{root}=mountApp(host.ctx,clean);await settle()
 // Release the SDK read after navigation's independent engine refresh ends.
 let refuse:((error:unknown)=>void)|undefined
 host.ctx.call=async(m:string,p:any)=>m==='downloads.location'?new Promise((_,reject)=>{refuse=reject}):original(m,p)
 button(root,'Settings').click();await settle()
 expect(refuse).toBeDefined();refuse!({code:'APP_STARTING'});await settle()
 expect(root.textContent).toContain(lifecycleCopy('starting','en'))
 expect(root.textContent).not.toContain(lifecycleCopy('stopped','en'))
})

test('a starting refusal in the SDK approval dialog also updates the page banner',async()=>{
 vi.useFakeTimers()
 const host=mockHost(),original=host.ctx.call
 host.ctx.executor.status=async()=>({state:'needs-admin-approval'})
 host.ctx.call=async(m:string,p:any)=>{if(m==='delegate.status')throw {code:'APP_STARTING'};return original(m,p)}
 const {root}=mountApp(host.ctx,clean);await settle()
 button(root,'Approve').click();await settle()
 expect(root.querySelector('[role="dialog"]')).not.toBeNull()
 expect(root.querySelector('main')?.textContent).toContain(lifecycleCopy('starting','en'))
})
