import {afterEach,expect,test,vi} from 'vitest'
import {formatRelativeTime} from '@synopackageland/ui'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {buttonNamed,confirmDialog,groupRows,openTab,rowOf,rows,settle,tabAction,tabLabels} from './downloads-helpers'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
const hourAgo=new Date(Date.now()-3600e3).toISOString()
function fixture(tasks:any[]){
 const host=mockHost(true);host.tasks.splice(0,host.tasks.length,...tasks)
 const {root}=mountApp(host.ctx,cleanup);return settle().then(()=>({root,host}))
}
const puts=(host:any)=>host.calls.filter((c:any)=>c.method==='gopeed.request'&&c.params?.method==='PUT').map((c:any)=>c.params.path)
const deletes=(host:any)=>host.calls.filter((c:any)=>c.method==='gopeed.request'&&c.params?.method==='DELETE').map((c:any)=>c.params.path)
const sample=()=>[
 {id:'saved',status:'done',updatedAt:hourAgo,meta:{req:{url:'https://www.example.org/a/report.pdf'}},publication:{published:true,filename:'report.pdf',folder:'/home/Downloads'}},
 {id:'broken',status:'error',meta:{opts:{name:'Broken.zip',path:'/home/Downloads'}}},
 {id:'held',status:'pause',meta:{opts:{name:'Held.iso',path:'/home/Downloads'},res:{size:4096}},progress:{downloaded:1024}},
 {id:'run',status:'running',meta:{opts:{name:'Running.mkv',path:'/home/Downloads'},res:{size:4096}},progress:{downloaded:1024,speed:512}},
]

test('the tabs count their downloads and All lists Downloading, Paused, Failed, then Completed',async()=>{
 const {root}=await fixture(sample())
 expect(tabLabels(root)).toEqual(['All (4)','Downloading (2)','Completed (1)','Failed (1)'])
 expect(rows(root).map(r=>r.dataset.group)).toEqual(['active','paused','failed','done'])
 expect(rows(root).map(r=>r.getAttribute('data-row-key'))).toEqual(['run','held','broken','saved'])
 // All lists the downloads, so it keeps their Pause all and Resume all;
 // clearing stays in Completed and Failed.
 expect([...root.querySelectorAll<HTMLElement>('[data-downloads] [data-group-action]')].map(b=>b.dataset.groupAction)).toEqual(['pause','continue'])
})

test('each tab lists only its own groups',async()=>{
 const {root}=await fixture(sample())
 const keys=()=>rows(root).map(r=>r.getAttribute('data-row-key'))
 await openTab(root,'active');expect(keys()).toEqual(['run','held'])
 await openTab(root,'done');expect(keys()).toEqual(['saved'])
 await openTab(root,'failed');expect(keys()).toEqual(['broken'])
 await openTab(root,'all');expect(keys()).toEqual(['run','held','broken','saved'])
})

test('Pause all and Resume all sit in the Downloading toolbar and act on their own group only',async()=>{
 const {root,host}=await fixture(sample())
 await openTab(root,'active')
 const pause=tabAction(root,'pause')!
 expect(pause.textContent?.trim()).toBe('Pause all');pause.click()
 await vi.waitFor(()=>expect(puts(host)).toEqual(['/api/v1/tasks/run/pause']))
 await settle()
 const resume=tabAction(root,'continue')!
 expect(resume.textContent?.trim()).toBe('Resume all');resume.click()
 // run is paused now too, so both resume.
 await vi.waitFor(()=>expect(puts(host).slice(1).sort()).toEqual(['/api/v1/tasks/held/continue','/api/v1/tasks/run/continue']))
})

test('Clear completed and Clear failed sit in their tab toolbars, ask first and clear only their group',async()=>{
 const {root,host}=await fixture(sample())
 await openTab(root,'done')
 const clearDone=tabAction(root,'clear')!
 expect(clearDone.textContent?.trim()).toBe('Clear completed (1)')
 clearDone.click();await settle()
 expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Clear completed records (1)?')
 expect(deletes(host)).toEqual([])
 await confirmDialog()
 await vi.waitFor(()=>expect(deletes(host)).toEqual(['/api/v1/tasks/saved']))
 await settle()
 await openTab(root,'failed')
 const clearFailed=tabAction(root,'clear')!
 expect(clearFailed.textContent?.trim()).toBe('Clear failed (1)')
 clearFailed.click();await settle()
 expect(document.querySelector('[role="dialog"]')?.textContent).toContain('Clear failed records (1)?')
 await confirmDialog()
 await vi.waitFor(()=>expect(deletes(host)).toEqual(['/api/v1/tasks/saved','/api/v1/tasks/broken']))
})

test('a completed row reads host · method · when it finished',async()=>{
 const {root}=await fixture(sample())
 const when=formatRelativeTime(new Date(hourAgo).getTime(),{locale:'en'})
 const meta=rowOf(root,'report.pdf')!.querySelector('.gopeed-dl-meta')!
 expect(meta.getAttribute('title')).toBe(`example.org · Direct · ${when}`)
 expect([...meta.querySelectorAll('span:not(.gopeed-dl-dot)')].map(x=>x.textContent)).toEqual(['example.org','Direct',when])
})

test('a failed row says why in red instead of its meta line',async()=>{
 const {root}=await fixture(sample())
 const row=rowOf(root,'Broken.zip')!
 const reason=row.querySelector('.gopeed-dl-meta')!
 expect(reason.classList.contains('is-failed')).toBe(true)
 expect(reason.textContent?.trim()).toBeTruthy()
 expect(row.querySelector('[role="progressbar"]')?.getAttribute('data-tone')).toBe('danger')
})

test('a long tab shows 100 rows until Show all',async()=>{
 const many=Array.from({length:105},(_,i)=>({id:'d'+i,status:'done',updatedAt:new Date(Date.now()-i*1000).toISOString(),publication:{published:true,filename:`file-${i}.zip`,folder:'/home/Downloads'}}))
 const {root}=await fixture(many)
 await openTab(root,'done')
 expect(tabLabels(root)[2]).toBe('Completed (105)')
 expect(groupRows(root,'done')).toHaveLength(100)
 // The latest come first.
 expect(rows(root)[0].getAttribute('data-row-key')).toBe('d0')
 buttonNamed(root.querySelector('[data-downloads-list]')!,'Show all (105)')!.click();await settle()
 expect(groupRows(root,'done')).toHaveLength(105)
})

test('the toolbar filter narrows every group by name',async()=>{
 const {root}=await fixture(sample())
 const input=root.querySelector<HTMLInputElement>('[data-downloads-filter] input, input[data-downloads-filter]')!
 input.value='held';input.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 expect(rows(root).map(r=>r.getAttribute('data-row-key'))).toEqual(['held'])
 input.value='nothing like it';input.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 expect(root.textContent).toContain('No matching downloads')
})

test('the toolbar says what downloads and how fast, and the page draws no title bar',async()=>{
 const {root}=await fixture(sample())
 const summary=root.querySelector('[data-downloads-summary]')!
 expect(summary.textContent?.trim()).toBe('1 downloading · ↓ 512 B/s')
 expect(summary.closest('[data-ui-section-tabs-actions]')).toBeTruthy()
 expect(root.querySelector('[data-ui-page-header]')).toBeNull()
 // A screen reader still hears the page's name.
 expect([...root.querySelectorAll('h1')].map(h=>h.textContent?.trim())).toEqual(['Downloads'])
 for(const action of ['[data-page-refresh]','[data-add-download]','[data-downloads-filter]'])expect(root.querySelector(`[data-ui-section-tabs-actions] ${action}`),action).not.toBeNull()
 cleanup.splice(0).forEach(fn=>fn())
 const idle=await fixture(sample().filter(t=>t.status!=='running'))
 expect(idle.root.querySelector('[data-downloads-summary]')).toBeNull()
})
