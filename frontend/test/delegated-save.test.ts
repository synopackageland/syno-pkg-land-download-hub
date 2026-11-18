import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {appCopy} from '../src/app-copy'
import {lifecycleCopy} from '@synopackageland/sdk'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<20;i++){await Promise.resolve();await nextTick()}}
async function fixture(){const host=mockHost();host.tasks.splice(0,host.tasks.length,...['queued','uncertain','interrupted','committed'].map((state,i)=>({id:'d'+i,status:'done',publication:{delegated:true,state,uncertain:state==='uncertain',published:state==='committed',filename:state+'.zip',folder:'/home'}})))
 const original=host.ctx.call;host.ctx.call=async(method:string,params:any)=>{if(method==='delegate.status')return {state:'ready',disabled:false,lifetime:'until-revoked',destinations:['save']};if(method==='delegate.list')return {delegations:[]};if(method==='delegate.operations.list')return {operations:[]};return original(method,params)}
 const {root}=mountApp(host.ctx,clean);await settle();return {root,host}}
test('delegated completion is observed without browser publication or replay',async()=>{const {root,host}=await fixture();expect(root.textContent).toContain('Saved');expect(root.textContent).toContain('Interrupted');expect(root.textContent).toContain('Unconfirmed');expect(root.textContent).toContain('The save result is uncertain.');expect(host.calls.filter(c=>c.method==='gopeed.publish')).toEqual([])})
test.each(['DELEGATION_DENIED'])('revoked access explains retained staging and fresh destination selection: %s',async code=>{
 const {root,host}=await fixture()
 host.tasks[0].publication={delegated:true,state:'access_denied',error:code,published:false,uncertain:false}
 root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 expect(root.textContent).toContain('Background saving was denied. The download remains in private staging and will not be retried.')
 expect(root.textContent).toContain('After an administrator restores access, choose your destination again for new downloads. This task will not be replayed')
 expect(root.textContent).not.toContain(code)
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toHaveLength(0)
 expect([...root.querySelectorAll('button')].some(b=>/Retry save/.test(b.textContent??''))).toBe(false)
})
test.each(['en','zh-TW','zh-CN','ja','ko','de','fr','it','es'])('access refusal has translated copy in %s',locale=>{
 expect(appCopy(locale,'save.accessDenied')).not.toBe('')
 if(locale!=='en')expect(appCopy(locale,'save.accessDenied')).not.toBe(appCopy('en','save.accessDenied'))
})
test('canceled background saves show a translated reason without raw diagnostics or retry',async()=>{
 const {root,host}=await fixture()
 host.tasks[0].publication={delegated:true,state:'canceled',reason:'integration-disabled',published:false}
 host.tasks[1].publication={delegated:true,state:'canceled',reason:'future-reason',published:false}
 const refresh=root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!;refresh.click();await settle()
 expect(root.textContent).toContain('Save canceled')
 expect(root.textContent).toContain('Background saving was disabled')
 expect(root.textContent).toContain('The background save was canceled. Check its access settings')
 expect(root.textContent).not.toContain('integration-disabled');expect(root.textContent).not.toContain('future-reason')
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toEqual([])
 expect([...root.querySelectorAll('button')].some(b=>b.textContent?.includes('Retry save'))).toBe(false)
})
test('admin management keeps SDK revoke without a kill switch or replay action',async()=>{
 const {root,host}=await fixture();let revoked=false
 const original=host.ctx.call
 host.ctx.call=async(method:string,params:any)=>{
  host.calls.push({method,params})
  if(method==='delegate.status')return {state:'ready',disabled:false}
  if(method==='delegate.list')return {delegations:revoked?[]:[{delegation_id:'grant1',destination:'save',path:'/home/Downloads',subject:{username:'fixture-admin'},lifetime:'until-revoked',revoked:false}]}
  if(method==='downloads.location')return {key:'save',selection:{scope:'home',root:'/home',generation:1},destination:revoked?null:{id:'grant1',key:'save',path:'/home/Downloads',username:'fixture-admin',lifetime:'until-revoked'},status:revoked?'reselection-required':'active'}
  if(method==='delegate.operations.list')return {operations:[]}
  if(method==='delegate.revoke'){expect(params).toEqual({delegation_id:'grant1'});revoked=true;return {revoked:true}}
  return original(method,params)
 }
 const click=async(label:string)=>{const b=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!;expect(b,label).toBeTruthy();b.click();await settle()}
 await click('Settings');expect(root.textContent).toContain('/home/Downloads');await click('Revoke access');expect(root.textContent).not.toContain('/home/Downloads')
 expect([...root.querySelectorAll('button')].some(b=>b.textContent?.trim()==='Disable background saves')).toBe(false)
 expect(host.calls.some(c=>c.method==='delegate.disable')).toBe(false)
 expect(root.textContent).not.toContain('Retry save')
})

for(const [state,code,key] of [['pending','ADMISSION_PENDING','ui.state.savePending'],['pending','EXECUTOR_UNAVAILABLE','ui.state.savePending'],['save_failed','ADMISSION_EXHAUSTED','save.failedHelp'],['save_failed','operation-not-found','save.failedHelp']])test(`admission ${code} is explained without a manual replay`,async()=>{
 const {root,host}=await fixture();host.tasks.splice(1);host.tasks[0].publication={delegated:true,state,error:code,published:false,uncertain:false};root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 expect(root.textContent).toContain(appCopy('en',key));expect(root.textContent).not.toContain(code);expect(root.textContent).not.toContain('Unconfirmed');expect(host.calls.filter(c=>c.method==='gopeed.publish')).toHaveLength(0)
})
// SDK 0.10.3: an executor still starting refuses the save for now; the
// backend retries it, so the row says to wait, not that something failed.
// SDK 0.10.6 says so in its own words.
test('a save the starting executor refused says it will be retried',async()=>{
 const {root,host}=await fixture();host.tasks.splice(1);host.tasks[0].publication={delegated:true,state:'copying',error:'EXECUTOR_STARTING',published:false,uncertain:false};root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 expect(root.textContent).toContain(lifecycleCopy('starting','en'));expect(root.textContent).not.toContain(appCopy('en','error.unknown'))
})
test.each(['en','zh-TW','zh-CN','ja','ko','de','fr','it','es'])('admission recovery messages are translated in %s',locale=>{
 for(const key of ['ui.state.savePending','save.pendingHelp','save.failedHelp']){expect(appCopy(locale,key)).not.toBe('');if(locale!=='en')expect(appCopy(locale,key)).not.toBe(appCopy('en',key))}
})
// SDK 0.9.1 File Station refusals name their cause; the next step follows the
// task's retryable flag, so no text promises a retry the SDK would refuse.
// SDK 0.9.4 keeps the grant on PERMISSION_DENIED and not-found and revokes
// it on SCOPE_DENIED; the text says which. SDK 0.9.6 keeps it on APP_DENIED.
for(const [code,text] of [['not-found','background folder stays authorized'],['name-too-long','name is too long'],['PERMISSION_DENIED','background folder stays authorized'],['destination-exists','same name already exists'],['SCOPE_DENIED','access to it was revoked'],['APP_DENIED','Application Privileges do not allow you to use Download Hub from this address. Your background folder stays authorized']])for(const retryable of [true,false])test(`save refusal ${code} names its cause (retryable ${retryable})`,async()=>{
 const {root,host}=await fixture();host.tasks.splice(1);host.tasks[0].publication={delegated:true,state:'save_failed',error:code,retryable,published:false,uncertain:false};root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 expect(root.textContent).toContain(text);expect(root.textContent).not.toContain(code)
 expect(root.textContent).toContain(appCopy('en',retryable?'save.retryStep':'save.noRetryStep'))
 expect(root.textContent?.includes('retry the save')).toBe(retryable)
 expect(!!root.querySelector('button[aria-label="Retry save"]')).toBe(retryable)
})
test.each(['en','zh-TW','zh-CN','ja','ko','de','fr','it','es'])('File Station refusals are translated in %s',locale=>{
 for(const key of ['error.not-found','error.name-too-long','error.PERMISSION_DENIED','error.SCOPE_DENIED','error.APP_DENIED','error.destination-exists','save.retryStep','save.noRetryStep']){expect(appCopy(locale,key)).not.toBe('');if(locale!=='en')expect(appCopy(locale,key)).not.toBe(appCopy('en',key))}
})
// A retry DSM still refuses explains the rule, not a lost destination.
test('a retried save DSM still refuses names Application Privileges',async()=>{
 const {root,host}=await fixture();host.tasks.splice(1);host.tasks[0].publication={delegated:true,state:'save_failed',error:'APP_DENIED',retryable:true,published:false,uncertain:false}
 const original=host.ctx.call;host.ctx.call=async(method:string,params:any)=>{if(method==='gopeed.publish')throw {code:'APP_DENIED'};return original(method,params)}
 root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 root.querySelector<HTMLButtonElement>('button[aria-label="Retry save"]')!.click();await settle()
 expect(root.textContent).not.toContain(appCopy('en','save.accessDenied'))
 expect(root.textContent?.split(appCopy('en','error.APP_DENIED')).length).toBe(3)
})
// A recreated folder at the same path is a different folder: SDK 0.9.4 answers
// SCOPE_DENIED and revokes the grant. Only moving the original folder back, or
// choosing another destination, recovers, as the SDK background panel says.
for(const [locale,moveBack,another,recreate] of [['en','move the folder back to its original location','choose another destination','recreate'],['zh-TW','移回原位置','改選其他目的地','重新建立'],['zh-CN','移回原位置','改选其他目标位置','重新创建'],['ja','元の場所に戻す','別の保存先を選択','作り直'],['ko','원래 위치로 되돌리','다른 대상을 선택','다시 만드'],['de','ursprünglichen Ort zurück','anderes Ziel','neu an'],['fr','emplacement d’origine','autre destination','recréez'],['es','ubicación original','otro destino','vuelva a crear'],['it','posizione originale','un’altra destinazione','ricrea']])test(`not-found asks to move the folder back, not recreate it, in ${locale}`,()=>{
 const text=appCopy(locale,'error.not-found');expect(text).toContain(moveBack);expect(text).toContain(another);expect(text).not.toContain(recreate)
})

// A finished download waits for its save under one name, before and after the
// save is queued.
test.each(['en','zh-TW','zh-CN','ja','ko','de','fr','it','es'])('the waiting-to-save phase has one name in %s',async locale=>{
 const host=mockHost();host.ctx.locale=locale
 host.tasks.splice(0,host.tasks.length,{id:'before',status:'done',name:'before.zip',publication:{delegated:true,published:false}},{id:'queued',status:'done',name:'queued.zip',publication:{delegated:true,state:'pending',published:false}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const statuses=[...root.querySelectorAll('[data-row-status]')].map(s=>s.textContent?.trim())
 expect(statuses).toEqual([appCopy(locale,'ui.state.savePending'),appCopy(locale,'ui.state.savePending')])
})
// Waiting to save is one state whatever its phase: same words, same blue dot.
test('both waiting-to-save phases show the info tone',async()=>{
 const host=mockHost()
 host.tasks.splice(0,host.tasks.length,{id:'before',status:'done',name:'before.zip',publication:{delegated:true,published:false}},{id:'queued',status:'done',name:'queued.zip',publication:{delegated:true,state:'pending',published:false}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const tones=[...root.querySelectorAll('[data-row-status]')].map(s=>s.getAttribute('data-tone'))
 expect(tones).toEqual(['info','info'])
})
