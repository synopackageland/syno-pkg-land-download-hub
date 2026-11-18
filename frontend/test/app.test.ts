import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.restoreAllMocks()})
async function settle(){for(let i=0;i<40;i++){await Promise.resolve();await nextTick()}}
async function fixture(admin=true,platform?:object){
 const host=mockHost(admin);if(platform)host.ctx.host.platform=()=>platform
 const {root,app}=mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle();return {root,host,app}
}
function button(root:ParentNode,text:string){
 const b=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>(b.getAttribute('aria-label')||b.textContent?.trim())===text)
 expect(b,`button ${text}`).toBeTruthy();return b!
}
async function click(root:ParentNode,text:string){button(root,text).click();await settle()}
test('SDK 0.8 DOM namespace and explicit DSM window layout',async()=>{
 const {root}=await fixture()
 expect(root.querySelector('[data-syno-pkg-land-ui]')).toBeTruthy()
 expect(root.querySelector('.syno-pkg-land-ui-root')).toBeTruthy()
 expect(root.querySelector('[data-syno-ui],.syno-ui-root')).toBeNull()
 expect(root.querySelector('[data-ui-layout-mode="window"]')).toBeTruthy()
})
test('normal users have personal downloads and settings without global management controls',async()=>{
 const {root,host}=await fixture(false)
 expect(root.textContent).toContain('Example download.zip')
 await click(root,'Settings')
 expect(root.textContent).not.toContain('Download core settings')
 expect([...root.querySelectorAll('button')].some(b=>['Extensions','Core updates'].includes(b.textContent?.trim()||''))).toBe(false)
 expect(root.querySelector('[data-settings-panel="general"] button[type="submit"]')).toBeNull()
 expect(root.querySelector('[data-media-settings]')).not.toBeNull()
 expect(root.querySelector('input[type="password"]')).toBeNull()
 expect(host.calls.some(c=>/^(gopeed\.(settings|plugins)|binaries\.|delegate\.(enable|disable)|executor\.control)/.test(c.method))).toBe(false)
 // The App's refresh and the SDK's ExecutorNotice each follow window reopens.
 expect(host.subscriptions).toBe(2)
})
// SDK 0.10.1: AppShell given the window's ctx says, above every page, that an
// executor awaiting approval holds some features back; the pages stay usable.
test('an executor awaiting approval shows a notice above the pages',async()=>{
 const host=mockHost(false);host.ctx.executor.status=async()=>({state:'needs-admin-approval',reason:'pins-changed'})
 const {root}=mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle()
 expect(root.textContent).toContain('Some features are unavailable until an administrator approves system access for this app')
 expect(root.textContent).toContain('Example download.zip')
})
// Executor setup reads shares through the browser, using the complete context.
test('approving from the notice reads shares through the browser transport',async()=>{
 const host=mockHost(true);host.ctx.executor.status=async()=>({state:'needs-admin-approval',reason:'pins-changed'})
 const {root}=mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle()
 await click(root,'Approve')
 const dialog=root.querySelector('[role="dialog"]')!;expect(dialog).toBeTruthy()
 expect(host.calls.some(c=>c.method==='fixture.dsm.shares')).toBe(true)
 expect(host.calls.some(c=>c.method==='bg.browse'||c.method.startsWith('nas.files.'))).toBe(false)
 expect(dialog.querySelector('[role="alert"]')).toBeNull()
 const password=dialog.querySelector<HTMLInputElement>('input[type="password"]')!
 expect(password).toBeTruthy();expect(password.disabled).toBe(false)
})
test('download filters, pause/resume, publication and history removal preserve API contracts',async()=>{
 const {root,host}=await fixture()
 expect(root.textContent).toContain('Example download.zip')
 await click(root,'Resume');await click(root,'Pause')
 expect(host.calls.some(c=>c.params.path==='/api/v1/tasks/active/pause')).toBe(true)
 expect(host.calls.some(c=>c.params.path==='/api/v1/tasks/paused/continue')).toBe(true)
 const done=root.querySelector('[data-group="done"]')!;expect(done.textContent).not.toContain('Example download.zip');expect(done.textContent).toContain('Saved document.pdf')
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toEqual([])
 button(done,'Clear download record').click();await settle();await click(root,'Confirm')
 expect(host.calls.some(c=>c.params.method==='DELETE')).toBe(true)
})
test('new-download dialog sends its links as one batch and stays inside its root',async()=>{
 const {root,host}=await fixture();await click(root,'New download')
 const dialog=root.querySelector('[role="dialog"]')!;expect(dialog).toBeTruthy()
 const field=dialog.querySelector<HTMLTextAreaElement>('textarea')!;field.value='https://example.com/file.zip';field.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 await click(dialog,'Add 1 task')
 expect(host.calls.find(c=>c.method==='downloads.create_many')?.params).toMatchObject({path:'/home/Downloads',items:[{url:'https://example.com/file.zip'}]})
})
test.each([['source_unsupported','extension that supports it'],['url_denied','private network'],['ed2k_link_incomplete','ed2k://'],['invalid_files','256 files']])('engine and safety refusal %s is explained without codes',async(code,text)=>{
  const {root,host}=await fixture();const original=host.ctx.call
  host.ctx.call=async(method:string,p:any)=>method==='downloads.create_many'?{results:p.items.map((i:any)=>({key:i.key,status:'failed',code}))}:original(method,p)
  await click(root,'New download');const dialog=root.querySelector('[role="dialog"]')!
  const field=dialog.querySelector<HTMLTextAreaElement>('textarea')!;field.value='https://pan.example/s/abc';field.dispatchEvent(new Event('input',{bubbles:true}));await settle()
  await click(dialog,'Add 1 task')
  const alert=dialog.querySelector('[role="alert"]')?.textContent||''
  expect(alert).toContain(text);expect(alert).not.toContain(code)
})
test('slow magnet metadata belongs to a preparing task after the dialog closes',async()=>{
 const {root,host}=await fixture();const original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{
  if(method==='downloads.create_many'){
   host.tasks.push({id:'prepare-magnet',status:'preparing',preparation:true,meta:{res:{name:'Torrent source'}}})
   return {results:p.items.map((i:any)=>({key:i.key,status:'created',ids:['prepare-magnet']}))}
  }
  return original(method,p)
 }
 await click(root,'New download');const dialog=root.querySelector('[role="dialog"]')!
 const field=dialog.querySelector<HTMLTextAreaElement>('textarea')!;field.value='magnet:?xt=urn:btih:'+'a'.repeat(40);field.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 button(dialog,'Add 1 task').click()
 await vi.waitFor(()=>expect(root.querySelector('[role="dialog"]')).toBeNull())
 await vi.waitFor(()=>expect(root.textContent).toContain('Preparing'))
 cleanup.splice(0).forEach(fn=>fn())
})
test('settings read back and submit numeric bounds without changing other options',async()=>{
 const {root,host}=await fixture();await click(root,'Settings');await click(root,'Apply')
 expect(host.calls.find(c=>c.method==='gopeed.settings.set')?.params).toEqual({maxRunning:2,connections:4,seedRatio:0,seedMinutes:0,btPort:16889,ed2kPort:4662,ed2kUdpPort:4672,trackers:['udp://tracker.example.org/announce']})
 expect(root.textContent).toContain('ED2K TCP port')
 expect(root.textContent).toContain('TCP and UDP')
 expect(root.textContent).toContain('Settings saved and verified')
})
test('extra trackers are entered one per line and blank lines are dropped',async()=>{
 const {root,host}=await fixture();await click(root,'Settings')
 const field=await vi.waitFor(()=>{const f=root.querySelector<HTMLTextAreaElement>('[data-bt-trackers] textarea, textarea[data-bt-trackers]');expect(f?.value).toBe('udp://tracker.example.org/announce');return f!})
 field.value='udp://a.example.org/announce\n\n https://b.example.net/announce \n';field.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 await click(root,'Apply')
 await vi.waitFor(()=>expect(host.calls.find(c=>c.method==='gopeed.settings.set')?.params.trackers).toEqual(['udp://a.example.org/announce','https://b.example.net/announce']))
 expect(root.textContent).toContain('Extra trackers')
})
test('extensions and core-update pages retain management controls',async()=>{
 const {root,host}=await fixture();await click(root,'Extensions')
 expect(root.textContent).toContain('Example extension');expect(button(root,'Install extension').disabled).toBe(true)
 await click(root,'Core updates');expect(host.calls.some(c=>c.method==='binaries.status')).toBe(true)
 expect(root.textContent).toContain('1.9.3')
})
test('session revocation clears private rows, controls and polling subscription',async()=>{
 const {root,host}=await fixture();host.ctx.call=async()=>{throw {code:'session_required'}}
 await click(root,'Refresh');expect(root.textContent).not.toContain('Example download.zip');expect(host.subscriptions).toBe(0)
})
test('independent mounts use unique ARIA IDs and app-local modals; unmount stops subscriptions',async()=>{
 const a=await fixture(),b=await fixture();await click(a.root,'New download')
 expect(a.root.querySelector('[role="dialog"]')).toBeTruthy();expect(b.root.querySelector('.gopeed-layout[aria-hidden="true"],.gopeed-layout[inert]')).toBeNull()
 const ids=[...document.querySelectorAll('[id]')].map(e=>e.id);expect(new Set(ids).size).toBe(ids.length)
 expect(a.host.subscriptions).toBe(2);a.app.unmount();expect(a.host.subscriptions).toBe(0)
 // Avoid a second unmount from afterEach.
 cleanup.splice(cleanup.length-2,1,()=>a.root.remove())
})
test('in a DSM window dialogs may leave the window, and pressing one brings the window to the front',async()=>{
 const {root,host}=await fixture();await click(root,'New download')
 const floating=document.querySelector<HTMLElement>('[data-ui-floating]')
 expect(floating?.querySelector('[role="dialog"]')).toBeTruthy()
 expect(floating!.closest('[data-v-app]')).toBeNull()
 floating!.dispatchEvent(new Event('pointerdown'))
 expect(host.calls.filter(c=>c.method==='fixture.host.activateWindow')).toHaveLength(1)
})
test('on a narrow viewport the floating dialogs become bottom sheets',async()=>{
 const width=window.innerWidth
 Object.defineProperty(window,'innerWidth',{configurable:true,value:390})
 try{
  const {root}=await fixture();await click(root,'New download')
  expect(document.querySelector('[data-ui-floating]')?.getAttribute('data-ui-presentation')).toBe('mobile')
 }finally{Object.defineProperty(window,'innerWidth',{configurable:true,value:width})}
})
test('outside a DSM desktop window dialogs stay inside the app',async()=>{
 for(const platform of [{host:'standalone-web'},{host:'dsm',dsmStandalone:true}]){
  const {root}=await fixture(true,platform);await click(root,'New download')
  expect(root.querySelector('[role="dialog"]')?.closest('[data-v-app]')).toBeTruthy()
  expect(document.querySelector('[data-ui-floating]')).toBeNull()
  cleanup.splice(0).forEach(fn=>fn())
 }
})
test('destination picker creates a caller-bound SDK grant without a session-only fallback',async()=>{
 const {root,host}=await fixture(false);await click(root,'Settings');await click(root,'Choose folder')
 const dialog=root.querySelector('[role="dialog"]')!;expect(dialog).toBeTruthy()
 // SDK 0.9.5 selects a folder only by clicking its row and draws it with the UI file-type icon.
 const row=dialog.querySelector<HTMLElement>('tr[data-row-key="/home"]')!
 expect(row.querySelector('.lucide-folder')).toBeTruthy()
 expect(dialog.querySelector('[data-folder-icon]')).toBeNull()
 row.dispatchEvent(new KeyboardEvent('keydown',{key:' ',bubbles:true}));await settle()
 expect(row.getAttribute('aria-selected')).toBe('false')
 row.click();await settle()
 expect(row.getAttribute('aria-selected')).toBe('true')
 await click(dialog,'Confirm selection')
 expect(host.calls.find(c=>c.method==='gopeed.destination')?.params).toEqual({key:'save',path:'/home'})
 expect(host.calls.some(c=>c.method==='gopeed.folder')).toBe(false)
})
test('extension settings, enable, update and removal retain explicit confirmations',async()=>{
 const {root,host}=await fixture();await click(root,'Extensions')
 const article=root.querySelector('[data-section="installed"] [role="listitem"]')!;await click(article,'Settings')
 expect(root.querySelector('[role="dialog"]')!.getAttribute('data-ui-dialog-sizing')).toBe('content')
 await click(root.querySelector('[role="dialog"]')!,'Save')
 expect(host.calls.find(c=>c.method==='gopeed.plugins.settings')?.params).toEqual({id:'sample',settings:{label:'example',mode:'safe'}})
 for(const [text,method] of [['Enable','switch'],['Update','update'],['Remove','delete']]){
  await click(article,text);expect(host.calls.some(c=>c.method===`gopeed.plugins.${method}`)).toBe(false)
  await click(root,'Confirm');expect(host.calls.some(c=>c.method===`gopeed.plugins.${method}`&&c.params.consent===true)).toBe(true)
 }
})
test('core check, verified install and rollback dispatch only after confirmation',async()=>{
 const {root,host}=await fixture();await click(root,'Core updates');await click(root,'Check for updates')
 // Card actions stay on the page, so the UI renders tool buttons.
 for(const text of ['Check for updates','Reinstall official version','Restore previous version'])expect(button(root,text).dataset.kind,text).toBe('tool')
 await click(root,'Reinstall official version')
 expect(host.calls.some(c=>c.method==='binaries.update')).toBe(false);await click(root,'Confirm')
 expect(host.calls.find(c=>c.method==='binaries.update')?.params).toEqual({version:'1.9.3',sha256:'f'.repeat(64),consent:true})
 await click(root,'Restore previous version');await click(root,'Confirm')
 expect(host.calls.find(c=>c.method==='binaries.rollback')?.params).toEqual({consent:true})
})
test('saved file navigation checks owner metadata and browser access before File Station selects the file',async()=>{
 const {root,host}=await fixture();const open=vi.spyOn(host.ctx.host,'revealFile');await click(root.querySelector('[data-row-key="done"]')!,'Open in File Station')
 expect(host.calls.find(c=>c.method==='gopeed.reveal')?.params).toEqual({id:'done'});expect(open).toHaveBeenCalledWith('/home/Downloads/document.pdf')
 // File Station opening is its own feedback: no success banner takes room.
 expect(root.textContent).not.toContain('Requested File Station')
})
test('a desktop opened from the standalone page selects the saved file in File Station',async()=>{
 const before=location.href;history.replaceState(null,'','?gopeedReveal=done')
 try{
  const host=mockHost();const open=vi.spyOn(host.ctx.host,'revealFile');mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle()
  expect(host.calls.find(c=>c.method==='gopeed.reveal')?.params).toEqual({id:'done'});expect(open).toHaveBeenCalledWith('/home/Downloads/document.pdf')
 }finally{history.replaceState(null,'',before)}
})
test('all business dialogs expose a connected accessible description',async()=>{
 const {root}=await fixture()
 function described(){const d=root.querySelector('[role="dialog"]')!;expect(d).toBeTruthy();const id=d.getAttribute('aria-describedby');expect(id).toBeTruthy();expect(root.querySelector(`[id="${id}"]`)?.textContent?.trim()).toBeTruthy()}
 await click(root,'New download');described();await click(root,'Cancel')
 await click(root,'Clear download record');described();await click(root,'Cancel')
 await click(root,'Extensions');await click(root.querySelector('[data-section="installed"] [role="listitem"]')!,'Settings');described()
})

test('completed tasks save automatically on open and manual save stays hidden',async()=>{
 const {root,host}=await fixture();await settle()
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toEqual([])
 expect([...root.querySelectorAll('button')].some(b=>b.textContent?.includes('Save completed downloads'))).toBe(false)
})
test('legacy jobs require reselection and expose no retry or browser publication',async()=>{
 const {root,host}=await fixture();await settle()
 expect(root.textContent).toContain('Choose the same destination again')
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toEqual([])
 expect([...root.querySelectorAll('button')].some(b=>b.textContent?.includes('Retry save'))).toBe(false)
})
test('uncertain and already saved tasks are never automatically replayed',async()=>{
 const host=mockHost();host.tasks.find(t=>t.id==='pending').publication={uncertain:true,published:false}
 const {root}=mountApp(host.ctx,cleanup);await settle()
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toHaveLength(0)
 expect(root.textContent).toContain('Unconfirmed')
 expect(root.textContent).toContain('The save result is uncertain.')
 expect([...root.querySelectorAll('button')].some(b=>b.textContent?.includes('Retry save'))).toBe(false)
})

test('engine done with incomplete content clearly fails without publication or retry',async()=>{
 const host=mockHost(false)
 const task=host.tasks.find(t=>t.id==='pending')
 task.publication={delegated:true,published:false,error:'download_incomplete',state:'download_failed'}
 const {root}=mountApp(host.ctx,cleanup);await settle()
 expect(root.textContent).toContain('Download failed')
 expect(root.textContent).toContain('The engine did not verify a complete download. Nothing was saved.')
 expect(root.textContent).not.toContain('download_incomplete')
 const failedRow=root.querySelector<HTMLElement>(`[data-row-key="${task.id}"][data-group="failed"]`)!
 expect(failedRow.querySelector('[role="progressbar"]')?.getAttribute('data-tone')).toBe('danger')
 expect(host.calls.filter(c=>c.method==='gopeed.publish')).toHaveLength(0)
 expect([...root.querySelectorAll('button')].some(b=>b.textContent?.includes('Retry save'))).toBe(false)
 expect([...root.querySelectorAll('[data-group="done"]')].map(r=>r.textContent).join('')).not.toContain(task.meta.opts.name)
})

test('official catalog discloses version, source and permissions before disabled installation',async()=>{
 const {root,host}=await fixture();await click(root,'Extensions')
 expect(root.textContent).toContain('Official registry sample');expect(root.textContent).toContain('https://github.com/GopeedLab/gopeed-extension-samples')
 await click(root,'Install Official registry sample')
 expect(host.calls.some(c=>c.method==='gopeed.plugins.install')).toBe(false)
 const dialog=root.querySelector('[role="dialog"]')!;expect(dialog.textContent).toContain('network');expect(dialog.textContent).toContain('1.0.0')
 await click(dialog,'Confirm')
 expect(host.calls.find(c=>c.method==='gopeed.plugins.install')?.params).toEqual({url:'https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample',consent:true})
})
// Gopeed 1.9.3 adds an installed extension to its list without a lock, so one
// installs at a time; the rest of the App stays usable meanwhile.
test('installing one catalog extension marks only that entry and says why the others wait',async()=>{
 const host=mockHost(true),call=host.ctx.call;let finish=()=>{}
 host.ctx.call=(async(method:string,params:any={})=>{
  if(method==='gopeed.plugins.catalog')return [{id:'a@x',title:'First',version:'1.0.0',url:'https://github.com/GopeedLab/a'},{id:'b@x',title:'Second',version:'2.0.0',url:'https://github.com/GopeedLab/b'}]
  if(method==='gopeed.plugins.install'){host.calls.push({method,params});await new Promise<void>(resolve=>{finish=resolve});return {saved:true}}
  return call(method,params)
 }) as typeof call
 const {root}=mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle()
 await click(root,'Extensions');await click(root,'Install First');await click(root.querySelector('[role="dialog"]')!,'Confirm')
 const first=button(root,'Install First'),second=button(root,'Install Second')
 expect(first.querySelector('[role="status"]'),'first shows progress').toBeTruthy()
 expect(second.disabled).toBe(true);expect(second.querySelector('[role="status"]')).toBeNull()
 expect(root.querySelector('[data-section="catalog"]')!.textContent).toContain('One extension installs at a time')
 expect(button(root,'Refresh').disabled).toBe(false)
 finish();await settle()
 expect(root.querySelector('[data-section="catalog"]')!.textContent).not.toContain('One extension installs at a time')
 expect(button(root,'Install Second').disabled).toBe(false)
})
test('the catalog footnote names every supported download source',async()=>{
 const {root}=await fixture();await click(root,'Extensions')
 const note=root.querySelector('[data-section="catalog"] .ui-settings-section-body > p')?.textContent||''
 for(const source of ['HTTP','BT','ED2K','extension'])expect(note).toContain(source)
 expect(note).not.toContain('single-file')
})
test('installed extensions show current and available versions and bind upgrade consent to that version',async()=>{
 const {root,host}=await fixture();await click(root,'Extensions')
 expect(root.textContent).toContain('Current version: 1.0.0');expect(root.textContent).toContain('Available version: 1.1.0')
 await click(root.querySelector('[data-section="installed"] [role="listitem"]')!,'Update');await click(root,'Confirm')
 expect(host.calls.find(c=>c.method==='gopeed.plugins.update')?.params).toEqual({id:'sample',version:'1.1.0',consent:true})
})
test('failed extension upgrade reports automatic restoration of the old version',async()=>{
 const {root,host}=await fixture();const original=host.ctx.call
 host.ctx.call=async(method:string,params:any)=>{if(method==='gopeed.plugins.update')throw {code:'plugin_update_failed_previous_restored'};return original(method,params)}
 await click(root,'Extensions');await click(root.querySelector('[data-section="installed"] [role="listitem"]')!,'Update');await click(root,'Confirm')
 expect(root.textContent).toContain('previous extension version and settings were restored')
 expect(root.textContent).toContain('Current version: 1.0.0')
})

test('each extension upgrade refusal has its own translated explanation',async()=>{
 for(const [code,locale,text] of [['plugin_source_denied','en','not from the official Gopeed engine extension catalog'],['plugin_snapshot_failed','zh-TW','無法備份目前的擴充套件'],['plugin_commit_uncertain','ja','アップグレードが完了したか確認できません'],['downloads_running','en','Paused downloads continue with the new version'],['downloads_running','zh-TW','暫停的下載會以新版繼續']]){
  const {root,host}=await fixture();const original=host.ctx.call
  host.ctx.call=async(method:string,params:any)=>{if(method==='gopeed.plugins.update')throw {code};return original(method,params)}
  await click(root,'Extensions');await click(root.querySelector('[data-section="installed"] [role="listitem"]')!,'Update');host.ctx.locale=locale;await click(root,'Confirm')
  expect(root.textContent).toContain(text)
  root.remove()
 }
},15_000)

test('successful extension upgrade clears the consumed available version',async()=>{
 const {root}=await fixture();await click(root,'Extensions');await click(root.querySelector('[data-section="installed"] [role="listitem"]')!,'Update');await click(root,'Confirm')
 const row=root.querySelector('[data-section="installed"] [role="listitem"]')!
 expect(row.textContent).toContain('Current version: 1.1.0')
 expect(row.textContent).not.toContain('Available version: 1.1.0')
 expect([...row.querySelectorAll('button')].some(b=>b.textContent?.trim()==='Update')).toBe(false)
})
test('multi-file tasks show saved files and retry only after a failure',async()=>{
 const {root,host,app}=await fixture()
 host.tasks.push({id:'bt',status:'done',meta:{res:{name:'Album',files:[{name:'a',size:3},{name:'b',size:4}]}},publication:{delegated:true,state:'save_failed',error:'EXISTS',retryable:true,filename:'Album',folder:'/home/Downloads',files:[{name:'Album - a',state:'committed'},{name:'Album - b',state:'save_failed',error:'EXISTS'}]}})
 await click(root,'Refresh')
 expect(root.textContent).toContain('1 of 2 files saved')
 await click(root,'Retry failed files')
 expect(host.calls.find(c=>c.method==='gopeed.publish')?.params).toEqual({id:'bt'})
 void app
})

// The task view's retryable flag is what gopeed.publish retries (D1).
test('retry follows the task view for single and multi-file saves',async()=>{
 const {root,host}=await fixture()
 host.tasks.push({id:'one',status:'done',meta:{opts:{name:'one.bin'},res:{name:'one.bin',files:[{name:'one.bin',size:3}]}},publication:{delegated:true,state:'save_failed',error:'destination-exists',retryable:true,filename:'one.bin',folder:'/home/Downloads'}})
 host.tasks.push({id:'busy',status:'done',meta:{res:{name:'Album',files:[{name:'a',size:3}]}},publication:{delegated:true,state:'save_failed',error:'EXISTS',retryable:false,filename:'Album',folder:'/home/Downloads',files:[{name:'Album - a',state:'save_failed',error:'EXISTS'}]}})
 await click(root,'Refresh')
 expect([...root.querySelectorAll('button')].filter(b=>['Retry save','Retry failed files'].includes(b.getAttribute('aria-label')||''))).toHaveLength(1)
 await click(root,'Retry save')
 expect(host.calls.find(c=>c.method==='gopeed.publish')?.params).toEqual({id:'one'})
})

// U1: refusal codes become readable messages in the user's language.
async function mountWith(host:ReturnType<typeof mockHost>){
 const {root}=mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle();return root
}
function refusingTaskList(code:string,locale='en'){
 const host=mockHost();host.ctx.locale=locale;const original=host.ctx.call
 host.ctx.call=async(m:string,p:any)=>{if(m==='gopeed.request'&&p.path==='/api/v1/tasks'&&p.method==='GET')throw {code,message:'Gopeed operation refused: '+code};return original(m,p)}
 return host
}
test('engine refusals show translated messages instead of codes',async()=>{
 for(const [code,locale,text] of [['engine_unavailable','en','The download engine is not responding'],['engine_crash_loop','zh-TW','下載核心反覆當掉後已停止'],['engine_restarting','ja','ダウンロードエンジンを再起動しています'],['source_unreachable','zh-TW','下載中心無法讀取此位址']]){
  const root=await mountWith(refusingTaskList(code,locale))
  expect(root.textContent).toContain(text)
  expect(root.textContent).not.toContain(code)
  expect(root.textContent).not.toContain('operation refused')
 }
})
test('an unknown refusal code shows a generic message, never the code',async()=>{
 const root=await mountWith(refusingTaskList('weird_new_code'))
 expect(root.textContent).toContain('Unable to perform this operation. Please try again later')
 expect(root.textContent).not.toContain('weird_new_code')
})
// A crashed engine also refuses the task list, as on the device.
test('the core panel explains an engine crash loop while the task list is refused',async()=>{
 const host=refusingTaskList('engine_unavailable');const original=host.ctx.call
 host.ctx.call=async(m:string,p:any)=>m==='binaries.status'?{ready:false,error:'engine_crash_loop',active:{version:'1.9.3'}}:original(m,p)
 const root=await mountWith(host);await click(root,'Core updates')
 expect(root.textContent).toContain('The download engine stopped after repeated crashes')
 expect(root.textContent).toContain('Package Center')
 expect(root.textContent).not.toContain('not responding')
 expect(root.textContent).not.toContain('engine_crash_loop')
})

test.each(['directory','denied'])('saved file navigation refuses current browser access: %s',async state=>{
 const {root,host}=await fixture(),original=host.ctx.call
 host.ctx.call=async(method:string,params:any={})=>{
  if(method==='fixture.dsm.info'){
   if(state==='denied')throw {code:'denied'}
   return {name:'document.pdf',path:params.path,isdir:true}
  }
  return original(method,params)
 }
 const open=vi.spyOn(host.ctx.host,'revealFile')
 await click(root.querySelector('[data-row-key="done"]')!,'Open in File Station')
 expect(host.calls.some(c=>c.method==='gopeed.reveal')).toBe(true)
 expect(open).not.toHaveBeenCalled()
})
test('saved history is revealed using its original path after the current grant changes',async()=>{
 const {root,host}=await fixture(),original=host.ctx.call
 host.ctx.call=async(method:string,params:any={})=>{
  if(method==='gopeed.reveal')return {folder:'/home/Old downloads',filename:'old.pdf'}
  if(method==='downloads.location')return {key:'save',status:'missing',destination:null}
  return original(method,params)
 }
 const open=vi.spyOn(host.ctx.host,'revealFile')
 await click(root.querySelector('[data-row-key="done"]')!,'Open in File Station')
 expect(host.calls.some(c=>c.method==='fixture.dsm.info'&&c.params.path==='/home/Old downloads/old.pdf')).toBe(true)
 expect(open).toHaveBeenCalledWith('/home/Old downloads/old.pdf')
})
test('temporary DSM authentication unavailability keeps the app shell and can recover',async()=>{
 const {root,host}=await fixture();const call=host.ctx.call
 host.ctx.call=async()=>{throw {code:'auth_unavailable'}}
 await click(root,'Refresh')
 expect(root.textContent).not.toContain('Sign in to DSM')
 expect(root.querySelector('[data-gopeed-native]')).toBeTruthy()
 expect(host.subscriptions).toBeGreaterThan(0)
 host.ctx.call=call;await click(root,'Refresh')
 expect(root.textContent).toContain('Example download.zip')
})
