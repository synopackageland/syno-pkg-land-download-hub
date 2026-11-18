import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
import {appCopy} from '../src/app-copy'
import {saveReasonMessage} from '../src/save-reason'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<30;i++){await Promise.resolve();await nextTick()}}
test('two ordinary user windows retain separate personal rows and destinations',async()=>{
 const windows=['normal1','normal2'].map(user=>{
  const host=mockHost(false,user),{root}=mountApp(host.ctx,clean);return {host,root,user}
 });await settle()
 for(const {root,user} of windows){expect(root.textContent).toContain(user+' · Example download.zip');expect(root.textContent).not.toContain((user==='normal1'?'normal2':'normal1')+' · Example download.zip')}
})
test('simultaneous download dialogs submit only their own mounted user form',async()=>{
 const windows=['normal1','normal2'].map(user=>{
  const host=mockHost(false,user),{root}=mountApp(host.ctx,clean);return {host,root,user}
 });await settle()
 for(const {root,user} of windows){
  const open=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='New download')!;open.click();await settle()
  const field=root.querySelector<HTMLTextAreaElement>('textarea[aria-label="Links"]')!;field.value='https://example.com/'+user+'.bin';field.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 }
 const second=windows[1],submit=[...second.root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Add 1 task')!
 expect(submit.form).toBe(second.root.querySelector('form'))
 submit.click();await settle()
 expect(second.host.calls.find(c=>c.method==='downloads.create_many')?.params.items.map((i:any)=>i.url)).toEqual(['https://example.com/normal2.bin'])
 expect(windows[0].host.calls.some(c=>c.method==='downloads.create_many')).toBe(false)
})
test.each(['en','zh-TW','zh-CN','ja','ko','de','fr','it','es'])('new authorization and save-reason copy is complete in %s',locale=>{
 const catalogs=import.meta.glob('../../i18n/messages/*.json',{eager:true,import:'default'}) as Record<string,Record<string,string>>
 const en=catalogs['../../i18n/messages/en.json'],local=catalogs['../../i18n/messages/'+locale+'.json']
 for(const key of Object.keys(en).filter(k=>k.startsWith('gopeed.'))){expect(local[key],key).toBeTruthy();expect(local[key],key).not.toMatch(/^gopeed\./)}
 expect(appCopy(locale,'entry.privileges')).toBe(local['gopeed.entry.privileges'])
 expect(saveReasonMessage('integration-disabled',locale)).toBe(local['gopeed.reason.integration-disabled'])
 expect(saveReasonMessage('future-reason',locale)).toBe(local['gopeed.reason.unknown'])
 expect(saveReasonMessage('<credential>',locale)).toBe('')
})
test('SDK user role hides administrator controls even if business session says admin',async()=>{
 const host=mockHost(true);host.ctx.user={name:'normal1',isAdmin:false}
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).not.toContain('Extensions')
 expect(root.textContent).not.toContain('Core updates')
})
test('administrator settings explains DSM default denial and offers Application Privileges navigation',async()=>{
 const host=mockHost(true);host.ctx.user={name:'fixture-admin',isAdmin:true};let opened='';host.ctx.host.openApp=(id:string)=>{opened=id;return {status:'requested'}}
 const {root}=mountApp(host.ctx,clean);await settle()
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Settings')!.click();await settle()
 await settingsTab(root,'access',settle)
 expect(root.textContent).toContain('DSM initially permits administrators only')
 const button=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Open Control Panel')!;expect(button).toBeDefined();button.click();await settle();expect(opened).toBe('SYNO.SDS.AdminCenter.Application')
})

test('a DSM user denied AppPriv sees the authorization guidance without download controls',async()=>{
 const host=mockHost(false,'normal1',false),{root}=mountApp(host.ctx,clean);await settle()
 expect(root.textContent).toContain('DSM initially permits administrators only')
 expect(root.textContent).not.toContain('New download')
 expect(host.calls.map(c=>c.method)).toEqual(['gopeed.session'])
})

// The shell renders at once; only a confirmed refusal shows the denied page.
test('while the session check runs the shell is open, not a full-window placeholder',async()=>{
 const host=mockHost();const call=host.ctx.call;host.ctx.call=(m:string,p:any)=>m==='gopeed.session'?new Promise(()=>{}):call(m,p)
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.querySelector('[data-ui-sidebar-body]')).not.toBeNull()
 expect(root.querySelector('.app-denied')).toBeNull()
 expect(root.textContent).not.toContain('Checking DSM permissions')
})

test('the AppPriv refusal is titled as missing application privileges, not a sign-in problem',async()=>{
 for(const [locale,title] of [['en','Application privileges required'],['zh-TW','需要應用程式權限']]){
  const host=mockHost(false,'normal1',false);host.ctx.locale=locale
  const {root}=mountApp(host.ctx,clean);await settle()
  expect(root.querySelector('.app-denied h1, .app-denied h2')?.textContent?.trim()).toBe(title)
  expect(root.textContent).not.toMatch(/DSM sign-in required|需要登入 DSM/)
 }
})

// A failed session lookup establishes no refusal, even after startup settles.
test('temporary authentication failure during startup keeps the shell open',async()=>{
 const host=mockHost(),call=host.ctx.call
 host.ctx.call=(method:string,params:any)=>method==='gopeed.session'?Promise.reject({code:'auth_unavailable'}):call(method,params)
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.querySelector('[data-ui-sidebar-body]')).not.toBeNull()
 expect(root.querySelector('.app-denied')).toBeNull()
 expect(root.querySelector('[data-gopeed-native]')).not.toBeNull()
})
