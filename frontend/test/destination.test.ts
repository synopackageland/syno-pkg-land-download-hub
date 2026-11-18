import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {appCopy} from '../src/app-copy'
import {createDestinationAdapter} from '../src/destination'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<30;i++){await Promise.resolve();await nextTick()}}
async function fixture(status:string,scope='shared:downloads'){
 const host=mockHost(),original=host.ctx.call
 host.ctx.call=async(method:string,params:any={})=>{
  if(method==='downloads.location'){host.calls.push({method,params});if(status==='unavailable')throw {code:'EXECUTOR_UNAVAILABLE'};return {key:'save',status,selection:status==='needs-scope-confirmation'?null:{scope,root:scope==='home'?'/home':'/downloads',generation:2},destination:status==='active'?{id:'share-grant',key:'save',path:'/downloads/Exports',username:'fixture-admin',lifetime:'until-revoked'}:null}}
  if(method==='fixture.dsm.info'){host.calls.push({method,params});return {name:'downloads',path:params.path,isdir:true}}
  return original(method,params)
 }
 const {root}=mountApp(host.ctx,clean);await settle();
 const click=async(label:string)=>{const button=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!;expect(button,label).toBeTruthy();button.click();await settle()}
 await click('New download');return {root,host,click,dialog:root.querySelector<HTMLElement>('[role="dialog"]')!}
}
test('shared destination comes from typed SDK lookup and scopes the picker',async()=>{
 const {root,dialog,host}=await fixture('active');expect(dialog.textContent).toContain('/downloads/Exports')
 choose(dialog,'Change').click();await settle();choose(root,'Use another folder').click();await settle()
 expect(host.calls.some(c=>c.method==='fixture.dsm.info'&&c.params.path==='/downloads')).toBe(true)
 expect(host.calls.some(c=>c.method==='fixture.dsm.info'&&c.params.path==='/home')).toBe(false)
})
test.each(['needs-scope-confirmation','disabled','unavailable'])('unverified scope refuses a picker: %s',async status=>{
 const {dialog}=await fixture(status)
 expect(choose(dialog,'Choose again').disabled).toBe(true);expect(dialog.textContent).not.toContain('/downloads/Exports')
})
test.each(['missing','reselection-required','revoked'])('scope without an active grant offers reselection: %s',async status=>{
 const {dialog}=await fixture(status)
 expect(choose(dialog,'Choose again').disabled).toBe(false);expect(dialog.textContent).not.toContain('/downloads/Exports')
})
const choose=(root:ParentNode,label:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!
// The new download dialog is the App's own place for the save folder; the
// settings section is the SDK's.
// SDK 0.9.1 reports revoked when the only grant was revoked.
test('a revoked grant is explained in every language',async()=>{
 const {dialog}=await fixture('revoked');expect(dialog.textContent).toContain(appCopy('en','destination.revoked'))
 for(const locale of ['en','zh-TW','zh-CN','ja','ko','de','fr','it','es']){expect(appCopy(locale,'destination.revoked')).not.toBe('');if(locale!=='en')expect(appCopy(locale,'destination.revoked')).not.toBe(appCopy('en','destination.revoked'))}
})
// Choosing a folder refused by Application Privileges authorizes nothing, so
// its text must not borrow the background save's "stays authorized" promise.
test('a folder choice refused by Application Privileges has its own message',async()=>{
 const {root,host,dialog:add}=await fixture('missing');const original=host.ctx.call
 host.ctx.call=async(method:string,params:any={})=>{if(method==='gopeed.destination'){host.calls.push({method,params});throw {code:'APP_DENIED'}}return original(method,params)}
 choose(add,'Choose again').click();await settle();choose(root,'Use another folder').click();await settle();const dialog=[...root.querySelectorAll('[role="dialog"]')].at(-1)!
 dialog.querySelector<HTMLElement>('tr[data-row-key="/downloads"]')!.click();await settle()
 const confirm=[...dialog.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Confirm selection')!;confirm.click();await settle()
 expect(host.calls.some(c=>c.method==='gopeed.destination')).toBe(true)
 expect(root.querySelectorAll('[role="dialog"]')).toHaveLength(2)
 expect(root.textContent).toContain(appCopy('en','destination.appDenied'))
 expect(root.textContent).not.toContain(appCopy('en','error.APP_DENIED'))
 expect(root.textContent).not.toContain('do not need to choose the folder again')
})
test('the folder choice and background save Application Privileges messages differ in every language',()=>{
 for(const locale of ['en','zh-TW','zh-CN','ja','ko','de','fr','it','es']){
  const choice=appCopy(locale,'destination.appDenied');expect(choice).not.toBe('');expect(choice).not.toBe(appCopy(locale,'error.APP_DENIED'))
  if(locale!=='en')expect(choice).not.toBe(appCopy('en','destination.appDenied'))
 }
})
// The SDK (0.49.4) passes WebAPI values to the DSM window channel, which
// JSON-encodes them itself: the App hands it the context unchanged.
test('folder lists go through the DSM window channel with WebAPI values',async()=>{
 const webApi=vi.fn(async(_api:string,method:string)=>method==='getinfo'?{files:[{name:'Downloads',path:'/home/Downloads',isdir:true}]}:{})
 const ctx:any={locale:'en',call:vi.fn(async()=>({mode:'browser-session'})),webApi}
 const info=await createDestinationAdapter(ctx).provider.info('/home/Downloads',new AbortController().signal)
 expect(info.path).toBe('/home/Downloads')
 expect(webApi).toHaveBeenCalledWith('SYNO.FileStation.List','getinfo',2,expect.objectContaining({path:['/home/Downloads']}))
})
