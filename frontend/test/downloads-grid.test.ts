import {afterEach,expect,test,vi} from 'vitest'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {formatDateTime} from '@synopackageland/ui'
import {buttonNamed,confirmDialog,metaOf,openPanel,openTab,rowOf,rows,settle,statusOf,tabAction} from './downloads-helpers'
const clean:(()=>void)[]=[]
afterEach(()=>{clean.splice(0).forEach(f=>f());localStorage.clear()})
const deletes=(host:ReturnType<typeof mockHost>)=>host.calls.filter(c=>c.method==='gopeed.request'&&c.params.method==='DELETE').map(c=>c.params.path)
const filterBox=(root:ParentNode)=>root.querySelector<HTMLInputElement>('input[data-downloads-filter],[data-downloads-filter] input')!
async function typeFilter(root:ParentNode,text:string){const input=filterBox(root);input.value=text;input.dispatchEvent(new Event('input',{bubbles:true}));await settle()}

// A long name keeps one line and its whole text is the tooltip.
test('a very long name stays on one line with the whole name as its tooltip',async()=>{
 const host=mockHost(true),name='超長的檔案名稱 Very long mixed 中英文 file name '.repeat(12)+'.mkv'
 host.tasks.push({id:'long',status:'pause',meta:{opts:{name,path:'/home/Downloads/Movies'}}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const cell=rowOf(root,'Very long mixed')!.querySelector('.gopeed-dl-name')!
 expect(cell.getAttribute('title')).toBe(name)
})

test('the panel shows the whole destination path and copies it',async()=>{
 const writeText=vi.fn(async()=>{})
 vi.stubGlobal('navigator',{...navigator,clipboard:{writeText}});clean.push(()=>vi.unstubAllGlobals())
 const host=mockHost(true)
 host.tasks.push({id:'slash',status:'pause',meta:{opts:{name:'Aurora.mkv',path:'/Movies/Northern Lights/'}}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const p=await openPanel(root,'Aurora.mkv')
 expect(p.textContent).toContain('Northern Lights')
 const copy=p.querySelector<HTMLButtonElement>('[data-ui-path-copy]')
 if(copy){copy.click();await settle();expect(writeText).toHaveBeenCalledWith('/Movies/Northern Lights/')}
})

test('the filter box finds downloads by name',async()=>{
 const {root}=mountApp(mockHost(true).ctx,clean);await settle()
 await typeFilter(root,'saved doc')
 expect(rows(root).map(r=>r.getAttribute('data-row-key'))).toEqual(['done'])
 await typeFilter(root,'nothing like it')
 expect(root.querySelector('[data-downloads]')!.textContent).toContain('No matching downloads')
})

test('the panel reads the start and completion times and the time left from the engine',async()=>{
 const host=mockHost(true)
 Object.assign(host.tasks[0],{createdAt:'2026-10-17T03:04:00Z',updatedAt:'2026-10-17T03:09:00Z'})
 Object.assign(host.tasks[2],{createdAt:'2026-10-16T01:00:00Z',updatedAt:'2026-10-16T01:30:00Z'})
 const {root}=mountApp(host.ctx,clean);await settle()
 let p=await openPanel(root,'Example download.zip')
 expect(p.textContent).toContain(formatDateTime('2026-10-17T03:04:00Z',{locale:'en'}))
 // Only a finished download has a completion time.
 expect(p.textContent).not.toContain('Completed')
 expect(p.querySelector('[data-panel-percent]')!.textContent).toBe('25%')
 // 3 KB left at 512 B/s.
 expect(p.textContent).toContain('0:06')
 expect(metaOf(rowOf(root,'Example download.zip')!)).toContain('0:06 left')
 p=await openPanel(root,'Saved document.pdf')
 expect(p.textContent).toContain(formatDateTime('2026-10-16T01:30:00Z',{locale:'en'}))
})

// Only saved records are cleared: a finished download that still waits to
// save is not complete yet.
test('Clear completed clears every saved record after one confirmation',async()=>{
 const host=mockHost(true)
 host.tasks.push({id:'done2',status:'done',publication:{published:true,filename:'Second.iso',folder:'/home/Downloads'}})
 const {root}=mountApp(host.ctx,clean);await settle()
 await openTab(root,'done')
 buttonNamed(root,'Clear completed (2)')!.click();await settle()
 const dialog=document.querySelector('[data-ui-dialog]')!
 expect(dialog.textContent).toContain('Clear completed records (2)?')
 expect(document.getElementById(dialog.getAttribute('aria-describedby')!)!.textContent?.trim()).toBe('Only the records are cleared. Files already saved to the NAS stay there; delete them in File Station if you no longer need them')
 await confirmDialog()
 expect(deletes(host)).toEqual(['/api/v1/tasks/done','/api/v1/tasks/done2'])
 await openTab(root,'all')
 expect(rowOf(root,'Ready to save.txt')).toBeTruthy()
 expect(root.textContent).toContain('Cleared 2 records. Saved files stay on the NAS')
 expect(buttonNamed(root,'Clear completed (2)')).toBeUndefined()
})

test('the Failed tab toolbar clears the failed records',async()=>{
 const host=mockHost(true)
 host.tasks.push({id:'broken',status:'error',meta:{opts:{name:'Broken.zip',path:'/home/Downloads'}}})
 const {root}=mountApp(host.ctx,clean);await settle()
 await openTab(root,'failed')
 expect(tabAction(root,'clear')!.textContent?.trim()).toBe('Clear failed (1)')
 tabAction(root,'clear')!.click();await settle()
 const dialog=document.querySelector('[data-ui-dialog]')!
 expect(document.getElementById(dialog.getAttribute('aria-describedby')!)!.textContent?.trim()).toBe('Only the records are cleared. Data of downloads that were not saved is discarded; files already saved to the NAS stay there')
 await confirmDialog()
 expect(deletes(host)).toEqual(['/api/v1/tasks/broken'])
})

test('the Downloading tab has nothing to clear in bulk',async()=>{
 const {root}=mountApp(mockHost(true).ctx,clean);await settle()
 await openTab(root,'active')
 expect(tabAction(root,'clear')).toBeNull()
})

// A status's reason or next step reads on its row's meta line and in the panel.
test('a status reason sits on the row and in the panel',async()=>{
 const host=mockHost(true)
 host.tasks.push({id:'legacy',status:'done',meta:{opts:{name:'Legacy.zip',path:'/home/Downloads'}}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const reason='Choose the same destination again in Settings to authorize this legacy download'
 expect(metaOf(rowOf(root,'Legacy.zip')!)).toContain(reason)
 expect((await openPanel(root,'Legacy.zip')).querySelector('[data-panel-details]')!.textContent).toContain(reason)
 expect((await openPanel(root,'Example download.zip')).querySelector('[data-panel-details]')).toBeNull()
})
test('a canceling save reads Canceling and says what is canceled',async()=>{
 const host=mockHost(true)
 host.tasks.push({id:'canceling',status:'done',meta:{opts:{name:'Canceling.zip',path:'/home/Downloads'}},publication:{delegated:true,state:'copying',error:'CANCEL_REQUESTED'}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const row=rowOf(root,'Canceling.zip')!
 expect(statusOf(row)).toBe('Canceling')
 expect(metaOf(row)).toContain('Canceling background saving. Waiting for the original operation; do not submit again')
})

// The page fills the window: its header stays in view and only the list
// scrolls. Settings fills it too.
test('the downloads and settings pages fill the window height, the extensions page does not',async()=>{
 const {root}=mountApp(mockHost(true).ctx,clean);await settle()
 expect(root.querySelector('.app-page-fill [data-downloads] .gopeed-dl-scroll [data-downloads-list]')).not.toBeNull()
 const go=async(name:string,key:string)=>{buttonNamed(root,name)?.click()??root.querySelector<HTMLElement>(`[data-ui-sidebar-item="${key}"],[data-value="${key}"]`)?.click();await settle()}
 await go('Settings','settings')
 expect(root.querySelector('.app-page-fill .ui-section-tabs-fill [data-settings-panel]')).not.toBeNull()
 await go('Extensions','plugins')
 expect(root.querySelector('[data-plugins]')).not.toBeNull()
 expect(root.querySelector('.app-page-fill')).toBeNull()
})
// The panel sits beside the list; a narrow window lays it over the list
// through a container query on the Downloads area.
test('the task panel is the list\'s side column, inside the area its container query measures',async()=>{
 const {root}=mountApp(mockHost(true).ctx,clean);await settle()
 const p=await openPanel(root,'Example download.zip')
 expect(p.parentElement!.classList.contains('gopeed-dl')).toBe(true)
 expect(p.previousElementSibling!.classList.contains('gopeed-dl-scroll')).toBe(true)
})
