import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {toolVersion} from '../src/media'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}
function buttons(root:ParentNode,text:string){return [...root.querySelectorAll<HTMLButtonElement>('button')].filter(b=>(b.getAttribute('aria-label')||b.textContent?.trim())===text)}
function button(root:ParentNode,text:string){const b=buttons(root,text)[0];expect(b,`button ${text}`).toBeTruthy();return b}
async function click(root:ParentNode,text:string){button(root,text).click();await settle()}
async function fixture(answers:Record<string,(p:any)=>any>={},prepare?:(host:ReturnType<typeof mockHost>)=>void){
 const host=mockHost(true),original=host.ctx.call;prepare?.(host)
 host.ctx.call=async(method:string,p:any={})=>{if(answers[method]){host.calls.push({method,params:p});return answers[method](p)}return original(method,p)}
 const {root}=mountApp(host.ctx,cleanup);await settle();return {root,host}
}
const catalog=[{id:'baidu@x',title:'百度网盘下载',description:'d',version:'1.3.7',url:'https://github.com/monkeyWie/gopeed-extension-baiduwp'},{id:'sample',title:'Example extension',description:'d',version:'1.0.0',url:'https://github.com/GopeedLab/gopeed-extension-samples#sample'}]

test('the extensions page lists installed extensions, then the catalog, then installing from an address',async()=>{
 const {root}=await fixture({'gopeed.plugins.catalog':()=>catalog})
 await click(root,'Extensions')
 const order=[...root.querySelectorAll('[data-plugins] [data-section]')].map(x=>x.getAttribute('data-section'))
 expect(order).toEqual(['installed','catalog','install-url'])
 expect(root.querySelector('[data-section="installed"]')!.textContent).toContain('Check extension updates')
})

// Installed extensions and the catalog are UI lists: each entry a ListItem
// with its badges as meta and its buttons as actions.
test('installed and catalog extensions are lists of the UI list items',async()=>{
 const {root}=await fixture({'gopeed.plugins.catalog':()=>catalog})
 await click(root,'Extensions')
 expect(root.querySelector('.plugin-row')).toBeNull()
 for(const section of ['installed','catalog']){
  const list=root.querySelector(`[data-section="${section}"] [role="list"]`)!
  const items=[...list.querySelectorAll('[role="listitem"]')]
  expect(items.length).toBeGreaterThan(0)
  for(const item of items){expect(item.querySelector('[data-ui-list-item-meta]')).not.toBeNull();expect(item.querySelector('[data-ui-list-item-actions] button')).not.toBeNull()}
 }
})

test('catalog entries install with a plain Install button and link to their repository',async()=>{
 const {root}=await fixture({'gopeed.plugins.catalog':()=>catalog})
 await click(root,'Extensions')
 const entry=root.querySelector('[data-catalog-entry="baidu@x"]')!
 const install=[...entry.querySelectorAll('button')]
 expect(install.map(b=>b.textContent?.trim())).toEqual(['Install'])
 expect(install[0].getAttribute('aria-label')).toBe('Install 百度网盘下载')
 const link=entry.querySelector<HTMLAnchorElement>('a[href="https://github.com/monkeyWie/gopeed-extension-baiduwp"]')!
 expect(link.target).toBe('_blank');expect(link.rel).toContain('noopener')
 expect(entry.querySelector('[data-ui-list-item-meta]')!.textContent).toContain('1.3.7')
 expect(entry.querySelector('[data-ui-list-item-meta]')!.textContent).toContain('Not installed')
})

test('installed extensions show version, state and update badges without a stray separator',async()=>{
 const {root}=await fixture()
 await click(root,'Extensions')
 const row=root.querySelector('[data-section="installed"] [role="listitem"]')!,badges=row.querySelector('[data-ui-list-item-meta]')!.textContent
 expect(badges).toContain('1.0.0');expect(badges).toContain('Disabled');expect(badges).toContain('Available version: 1.1.0')
 expect(row.textContent).not.toMatch(/^\s*·/m)
 expect(row.querySelector('.footnote')?.textContent?.trim().startsWith('·')).not.toBe(true)
})

test('reinstalling the same core is a secondary action and missing rollbacks are hidden',async()=>{
 const tools={state:{tools:{'yt-dlp':{active:{version:'2026.08.19'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
 const {root}=await fixture({'media.tools.status':()=>tools,'binaries.status':()=>({ready:true,active:{version:'1.9.3'}})})
 await click(root,'Core updates');await click(root.querySelector('[data-core]')!,'Check for updates')
 const reinstall=button(root,'Reinstall official version'),check=button(root.querySelector('[data-core]')!,'Check for updates')
 expect(reinstall.className).toBe(check.className)
 expect(buttons(root,'Restore previous version')).toHaveLength(0)
})

test('every media tool card shows a state badge and a latest-version row',async()=>{
 const tools={state:{tools:{'yt-dlp':{active:{version:'2026.08.19'},previous:{version:'2026.07.01'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
 const {root}=await fixture({'media.tools.status':()=>tools,'media.tools.check':p=>p.tool==='ffmpeg'?{latest:{version:'2026-10-12-22-13'},update:true}:{latest:{version:'2026.08.19'},update:false}})
 await click(root,'Core updates')
 const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!,ffmpeg=root.querySelector('[data-media-tool="ffmpeg"]')!
 expect(ytdlp.querySelector('[data-badges]')!.textContent).toContain('Installed')
 expect(ffmpeg.querySelector('[data-badges]')!.textContent).toContain('Not installed')
 for(const card of [ytdlp,ffmpeg])expect(card.textContent).toContain('Latest stable')
 expect(buttons(ytdlp,'Restore previous version')).toHaveLength(1)
 expect(buttons(ffmpeg,'Restore previous version')).toHaveLength(0)
 await click(ffmpeg,'Check for updates')
 expect(ffmpeg.querySelector('[data-badges]')!.textContent).toContain('Available to install')
 expect(ffmpeg.textContent).toContain('2026.10.12 22:13')
})

// A card is titled by the tool's own name, not the backend's key, and an
// up-to-date tool says so with a badge beside the version.
test('tool cards carry the tools\' names and mark an up-to-date version with a badge',async()=>{
 const tools={state:{tools:{'yt-dlp':{active:{version:'2026.08.19'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
 const {root}=await fixture({'media.tools.status':()=>tools,'media.tools.check':()=>({latest:{version:'2026.08.19'},update:false})})
 await click(root,'Core updates')
 const title=(tool:string)=>root.querySelector(`[data-media-tool="${tool}"]`)!.textContent
 expect(title('ffmpeg')).toContain('FFmpeg');expect(title('deno')).toContain('Deno')
 const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!
 await click(ytdlp,'Check for updates')
 const latest=[...ytdlp.querySelectorAll('dl')].find(d=>d.textContent?.includes('Latest stable'))!
 expect(latest.querySelector('dd')!.textContent).toContain('2026.08.19')
 expect(latest.querySelector('dd')!.textContent).not.toContain('·')
 expect(latest.querySelector('dd [data-ui-badge-dot]')?.parentElement?.textContent).toContain('Up to date')
})

// The latest version starts where the current one does, at the label column.
test('a tool card\'s latest version lines up with its current version',async()=>{
 const tools={state:{tools:{'yt-dlp':{active:{version:'2026.08.19'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
 const {root}=await fixture({'media.tools.status':()=>tools,'media.tools.check':()=>({latest:{version:'2026.08.19'},update:false})})
 await click(root,'Core updates')
 const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!
 await click(ytdlp,'Check for updates')
 const latest=[...ytdlp.querySelectorAll('dl')].find(d=>d.textContent?.includes('Latest stable'))!
 for(const box of latest.querySelectorAll<HTMLElement>('dd *'))expect(box.style.justifyContent||'flex-start').toBe('flex-start')
})

test('dated tool builds read like the other date versions',()=>{
 expect(toolVersion('2026-10-12-22-13')).toBe('2026.10.12 22:13')
 expect(toolVersion('2026.08.19')).toBe('2026.08.19')
 expect(toolVersion('v2.5.4')).toBe('v2.5.4')
})

const counts=(root:ParentNode)=>[...root.querySelectorAll('[data-downloads] [data-ui-tab-value]')].map(t=>[t.getAttribute('data-ui-tab-value'),t.textContent?.trim()])
test('download tabs show their counts and the empty list offers one New download action',async()=>{
 const {root}=await fixture()
 expect(counts(root)).toEqual([['all','All (4)'],['active','Downloading (2)'],['done','Completed (2)'],['failed','Failed (0)']])
 cleanup.splice(0).forEach(fn=>fn())
 const empty=await fixture({},host=>host.tasks.splice(0))
 expect(buttons(empty.root,'New download')).toHaveLength(1)
})

test('failed downloads leave Downloading for their own Failed tab',async()=>{
 const {root}=await fixture({},host=>host.tasks.push(
  {id:'broken',status:'error',meta:{opts:{name:'Broken.zip',path:'/home/Downloads'}}},
  {id:'unsaved',status:'done',publication:{error:'PERMISSION_DENIED',filename:'Unsaved.zip'}}))
 expect(counts(root)).toEqual([['all','All (6)'],['active','Downloading (2)'],['done','Completed (2)'],['failed','Failed (2)']])
})

test('a paused download has its own Paused group',async()=>{
 const {root}=await fixture()
 const paused=[...root.querySelectorAll('[data-group="paused"]')].map(r=>r.textContent).join('')
 expect(paused).toContain('Paused download.zip')
 expect(paused).not.toContain('Example download.zip')
})

// A row opens its details beside the list; the same row again closes them.
async function openDetails(root:ParentNode,key:string){
 root.querySelector<HTMLElement>(`[data-downloads-list] [data-row-key="${key}"]`)!.click();await settle()
 return root.querySelector<HTMLElement>('[data-task-panel]')!
}
test('task details show transfer with live peer counts and per-file save states',async()=>{
 vi.useFakeTimers({shouldAdvanceTime:true})
 try{
  const {root,host}=await fixture({},host=>host.tasks.push({id:'bt',protocol:'bt',status:'running',createdAt:'2026-10-20T08:00:00Z',meta:{opts:{name:'Album',path:'/home/Downloads'},req:{url:'magnet:?xt=urn:btih:'+'a'.repeat(40)},res:{size:2048,files:[{name:'a.flac',size:1024},{name:'b.flac',size:1024}]}},progress:{downloaded:1024,speed:256,uploaded:512,uploadSpeed:64,used:30e9},publication:{files:[{name:'a.flac',rel:'Album/a.flac',size:1024,state:'committed'},{name:'b.flac',rel:'Album/b.flac',size:1024,state:'pending'}]}}))
  const panel=await openDetails(root,'bt')
  expect(panel.textContent).toContain('Album')
  // The magnet link is copied, not shown.
  expect(panel.querySelector('[data-copy-link]')).toBeTruthy()
  await vi.waitFor(()=>expect(panel.querySelector('[data-details-peers]')?.textContent).toContain('3 / 12'))
  expect(panel.textContent).toContain('0.50')
  const reads=()=>host.calls.filter(c=>c.method==='gopeed.request'&&c.params.path==='/api/v1/tasks/bt/stats').length
  const before=reads();await vi.advanceTimersByTimeAsync(5000);expect(reads()).toBe(before+1)
  const files=panel.querySelector('[data-details-files]')!.textContent
  expect(files).toContain('Album/a.flac');expect(files).toContain('Saved');expect(files).toContain('Waiting to save')
  // Files already planned for saving can no longer be chosen.
  expect(panel.querySelector('[data-apply-files]')).toBeNull()
  panel.querySelector<HTMLElement>('[aria-label="Close"]')!.click();await settle()
  expect(root.querySelector('[data-task-panel]')).toBeNull()
  const after=reads();await vi.advanceTimersByTimeAsync(10000);expect(reads()).toBe(after)
 }finally{vi.useRealTimers()}
})

test('details of a plain download show no peer counts or file list',async()=>{
 const {root}=await fixture()
 const panel=await openDetails(root,'active')
 expect(panel.textContent).toContain('Example download.zip')
 expect(panel.querySelector('[data-details-peers]')).toBeNull()
 expect(panel.querySelector('[data-panel-files]')).toBeNull()
 expect(panel.querySelector('[data-tiles="3"]')?.textContent).toContain('Connections')
})

test('core settings group BT and ED2K fields under their own headings with ranges as help',async()=>{
 const {root}=await fixture()
 await click(root,'Settings')
 const groups=[...root.querySelectorAll('[data-section="core"] [data-ui-fieldset]')].map(x=>x.textContent)
 expect(groups).toHaveLength(3)
 expect(groups[0]).toContain('Download core settings');expect(groups[1]).toContain('BT seeding and port');expect(groups[2]).toContain('ED2K ports')
 const first=root.querySelector('[data-settings-panel] form:has(input[type="number"]) label')!
 expect(first.textContent).not.toContain('(1–16)')
 expect(root.querySelector('[data-settings-panel] form:has(input[type="number"])')!.textContent).toContain('1–16')
 // The seeding time names its unit after the field.
 const minutes=[...root.querySelectorAll('[data-section="core"] [data-form-item]')].find(x=>x.textContent?.includes('Stop seeding after'))!
 expect(minutes.querySelector('[data-ui-input-unit]')?.textContent?.trim()).toBe('min')
})

test('a tool that is not installed offers to install instead of claiming an update',async()=>{
 const tools={state:{tools:{'yt-dlp':{active:{version:'2026.08.19'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
 const {root}=await fixture({'media.tools.status':()=>tools,'media.tools.check':p=>({latest:{version:p.tool==='yt-dlp'?'2026.09.01':'v2.5.4'},update:true})})
 await click(root,'Core updates')
 const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!,deno=root.querySelector('[data-media-tool="deno"]')!
 await click(ytdlp,'Check for updates');await click(deno,'Check for updates')
 const badges=(card:Element)=>[...card.querySelectorAll('[data-badges] > *')].map(x=>x.textContent?.trim())
 expect(badges(deno)).toEqual(['Available to install'])
 expect(badges(ytdlp)).toEqual(['Installed','Update available'])
})

test('a previous core with the active version is not offered as a rollback',async()=>{
 const {root}=await fixture({'binaries.status':()=>({ready:true,active:{version:'1.9.3'},previous:{version:'1.9.3'}})})
 await click(root,'Core updates')
 const core=root.querySelector('[data-core]')!
 expect(core.textContent).not.toContain('Previous version')
 expect(buttons(core,'Restore previous version')).toHaveLength(0)
})

test('settings show the destination once, keep Apply with its fields and drop the stray history note',async()=>{
 const {root}=await fixture()
 await click(root,'Settings')
 const page=root.querySelector('[data-settings-panel="general"]')!
 expect([...page.querySelectorAll('input')].some(i=>i.value==='/home/Downloads')).toBe(false)
 expect(page.textContent).toContain('/home/Downloads')
 const apply=button(page,'Apply')
 expect(apply.closest('.ui-settings-page-footer')).toBeTruthy()
 expect(apply.getAttribute('form')).toBe(page.querySelector('form[data-section="core"]')!.id)
 expect(page.textContent).not.toContain('does not delete saved files')
})

test('the catalog does not repeat installed extensions',async()=>{
 const {root}=await fixture({'gopeed.plugins.catalog':()=>catalog})
 await click(root,'Extensions')
 expect(root.querySelector('[data-catalog-entry="sample"]')).toBeNull()
 expect(root.querySelector('[data-catalog-entry="baidu@x"]')).toBeTruthy()
 expect(root.querySelector('[data-section="catalog"]')!.textContent).toContain('1 installed extension is listed above')
})

// The panel has room for the whole destination; it is also the tooltip.
test('the details show the whole destination path, also as its tooltip',async()=>{
 const {root}=await fixture()
 const panel=await openDetails(root,'active')
 const path=panel.querySelector('.gopeed-panel-kv [data-ui-path-text]')!
 expect(path.textContent?.trim()).toBe('/home/Downloads')
 expect(path.getAttribute('data-ui-tooltip-text')).toBe('/home/Downloads')
})

// A confirmation's technical details sit on their own lines, not in brackets
// after the sentence.
test('confirmations put versions and addresses on their own lines',async()=>{
 const tools={state:{tools:{'yt-dlp':{active:{version:'2026.08.19'},previous:{version:'2026.07.01'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
 const {root}=await fixture({'gopeed.plugins.catalog':()=>catalog,'media.tools.status':()=>tools,'media.tools.check':()=>({latest:{version:'2026.10.01'},update:true})})
 const description=()=>root.querySelector('[role="dialog"] [data-ui-message-box-description], [role="dialog"] p')!.textContent!
 const cancel=()=>click(root.querySelector('[role="dialog"]')!,'Cancel')
 await click(root,'Core updates');const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!
 await click(ytdlp,'Check for updates');await click(ytdlp,'Update')
 expect(description()).toMatch(/\n2026\.08\.19 → 2026\.10\.01$/);expect(description()).not.toContain('(');await cancel()
 await click(ytdlp,'Restore previous version')
 expect(description()).toMatch(/\n2026\.08\.19 → 2026\.07\.01$/);expect(description()).not.toContain('(');await cancel()
 await click(root,'Extensions');await click(root,'Install 百度网盘下载')
 expect(description()).toMatch(/\n百度网盘下载 1\.3\.7\nhttps:\/\/github\.com\/monkeyWie\/gopeed-extension-baiduwp$/);expect(description()).not.toContain('(')
})

// A saved row opens in File Station from its own button; the App keeps no
// link CSS of its own, and the extension address sits in a UI input group.
test('a saved row opens in File Station and the extension address sits in a UI input group',async()=>{
 const {root}=await fixture()
 const row=root.querySelector<HTMLElement>('[data-row-key="done"]')!
 expect(row.querySelector('.gopeed-dl-name')?.textContent).toContain('Saved document.pdf')
 expect(row.querySelector('button[aria-label="Open in File Station"]')).toBeTruthy()
 expect(root.querySelector('.file-link')).toBeNull()
 await click(root,'Extensions')
 const line=root.querySelector('[data-section="install-url"] input')!.parentElement!.closest('[data-syno-pkg-land-ui]:has(> button[type="submit"])')!
 expect(line,'input group').toBeTruthy();expect(line.className).toContain('flex-1')
 expect(root.querySelector('.install-line')).toBeNull()
})
