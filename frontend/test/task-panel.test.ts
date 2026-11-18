import {afterEach,expect,test,vi} from 'vitest'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {buttonNamed,confirmDialog,panel,settle} from './downloads-helpers'
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.useRealTimers()})
const http={id:'http',status:'running',meta:{opts:{name:'Direct.zip',path:'/home/Downloads'},req:{url:'https://example.org/Direct.zip'},res:{size:4096}},progress:{downloaded:1024,speed:512}}
const bt={id:'bt',protocol:'bt',status:'running',meta:{opts:{name:'Album',path:'/home/Downloads'},req:{url:'magnet:?xt=urn:btih:'+'a'.repeat(40)},res:{name:'Album',size:2048,files:[{name:'a.flac',size:1024},{name:'b.flac',size:1024}]}},progress:{downloaded:512,speed:256}}
const media={id:'media',protocol:'media',status:'running',meta:{opts:{name:'Clip',path:'/home/Downloads'},res:{size:4096}},progress:{downloaded:1024,speed:128}}
async function fixture(tasks:any[],stats?:(path:string)=>any){
 const host=mockHost(true);host.tasks.splice(0,host.tasks.length,...structuredClone(tasks))
 if(stats){const original=host.ctx.call;host.ctx.call=async(method:string,p:any={})=>{if(method==='gopeed.request'&&p.path?.endsWith('/stats')){host.calls.push({method,params:p});return {code:0,data:stats(p.path)}}return original(method,p)}}
 const {root}=mountApp(host.ctx,cleanup);await settle();return {root,host}
}
async function open(root:ParentNode,id:string){root.querySelector<HTMLElement>(`[data-downloads-list] [data-row-key="${id}"]`)!.click();await settle();return panel(root)!}
const statReads=(host:any,id:string)=>host.calls.filter((c:any)=>c.method==='gopeed.request'&&c.params.path===`/api/v1/tasks/${id}/stats`).length
async function menu(p:HTMLElement,label:string){
 const trigger=p.querySelector<HTMLButtonElement>('button[aria-label="More"]')!
 trigger.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0}))
 trigger.dispatchEvent(new KeyboardEvent('keydown',{bubbles:true,key:'Enter'}))
 await settle()
 if(!document.querySelector('[role="menuitem"]')){trigger.click();await settle()}
 const item=[...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(i=>i.textContent?.trim()===label)
 expect(item,`menu item ${label}`).toBeTruthy();item!.click();await settle()
}

test('a row click opens its panel and a second click closes it',async()=>{
 const {root}=await fixture([http])
 expect(panel(root)).toBeNull()
 const p=await open(root,'http')
 expect(p.textContent).toContain('Direct.zip')
 expect(root.querySelector('[data-row-key="http"]')!.getAttribute('aria-current')).toBe('true')
 await open(root,'http');expect(panel(root)).toBeNull()
})

test('direct and torrent downloads show three tiles, a media download two',async()=>{
 const {root}=await fixture([http,bt,media])
 expect((await open(root,'http')).querySelector('.gopeed-panel-tiles')!.getAttribute('data-tiles')).toBe('3')
 expect((await open(root,'bt')).querySelector('.gopeed-panel-tiles')!.getAttribute('data-tiles')).toBe('3')
 const m=await open(root,'media')
 expect(m.querySelector('.gopeed-panel-tiles')!.getAttribute('data-tiles')).toBe('2')
 expect(m.querySelectorAll('.gopeed-panel-tile')).toHaveLength(2)
})

test('the connection line reads the engine counters from /stats',async()=>{
 const {root}=await fixture([http,bt],path=>path.includes('/http/')?{connections:[{completed:true},{retryTimes:2},{}]}:{totalPeers:12,activePeers:3,connectedSeeders:2,seedRatio:0.5,seedTime:120})
 const h=await open(root,'http')
 await vi.waitFor(()=>expect(h.querySelector('[data-connection-line]')?.textContent).toBe('3 connections · 1 done · 1 retrying'))
 expect(h.querySelector('[data-tiles] .gopeed-panel-tile:last-child b')?.textContent).toBe('2 / 3')
 const b=await open(root,'bt')
 await vi.waitFor(()=>expect(b.querySelector('[data-connection-line]')?.textContent).toBe('3 of 12 peers active · 2 seeders'))
 expect(b.querySelector('[data-details-peers]')?.textContent).toContain('3 / 12')
})

test('Copy link shows only when the task carries its link',async()=>{
 const hidden={...http,id:'hidden',meta:{opts:{name:'Hidden.zip',path:'/home/Downloads'},res:{size:4096}}}
 const {root}=await fixture([http,hidden])
 expect((await open(root,'http')).querySelector('[data-copy-link]')).toBeTruthy()
 expect((await open(root,'hidden')).querySelector('[data-copy-link]')).toBeNull()
})

test('the More menu removes the record after confirmation',async()=>{
 const {root,host}=await fixture([http])
 await menu(await open(root,'http'),'Remove')
 expect(document.querySelector('[role="dialog"]')).toBeTruthy()
 await confirmDialog()
 await vi.waitFor(()=>expect(host.calls.some(c=>c.params?.method==='DELETE'&&c.params.path==='/api/v1/tasks/http')).toBe(true))
})

test('the More menu retries a failed save',async()=>{
 const failed={id:'unsaved',status:'done',meta:{opts:{name:'Unsaved.zip',path:'/home/Downloads'}},publication:{delegated:true,state:'save_failed',error:'not-found',retryable:true,published:false}}
 const {root,host}=await fixture([failed])
 await menu(await open(root,'unsaved'),'Retry save')
 await vi.waitFor(()=>expect(host.calls.find(c=>c.method==='gopeed.publish')?.params).toEqual({id:'unsaved'}))
})

test('a torrent chooses its files and Apply sends the choice',async()=>{
 const {root,host}=await fixture([bt])
 const p=await open(root,'bt')
 const apply=p.querySelector<HTMLButtonElement>('[data-apply-files]')!
 expect(apply.disabled).toBe(true)
 const row=[...p.querySelectorAll('[data-details-files] tbody tr')].find(r=>r.textContent?.includes('b.flac'))!
 row.querySelector<HTMLElement>('[role="checkbox"],input[type="checkbox"]')!.click();await settle()
 expect(apply.disabled).toBe(false);expect(apply.textContent?.trim()).toBe('Download selected (1)')
 apply.click();await settle()
 await vi.waitFor(()=>expect(host.calls.find(c=>c.method==='downloads.select_files')?.params).toEqual({id:'bt',files:[0]}))
})

test('the panel reads counters only while it is open',async()=>{
 vi.useFakeTimers({shouldAdvanceTime:true})
 const {root,host}=await fixture([http])
 expect(statReads(host,'http')).toBe(0)
 const p=await open(root,'http')
 await vi.waitFor(()=>expect(statReads(host,'http')).toBe(1))
 await vi.advanceTimersByTimeAsync(4100);expect(statReads(host,'http')).toBe(2)
 buttonNamed(p,'Close')!.click();await settle()
 expect(panel(root)).toBeNull()
 const after=statReads(host,'http');await vi.advanceTimersByTimeAsync(12000);expect(statReads(host,'http')).toBe(after)
})
