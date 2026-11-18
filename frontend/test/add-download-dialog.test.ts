import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {lifecycleCopy} from '@synopackageland/sdk'
// A checkbox control inside an element, or the element itself.
function tick(el:Element){const box=el.matches('[role="checkbox"],input[type="checkbox"],[data-choose-files]')?el:el.querySelector('[role="checkbox"],input[type="checkbox"]')!;(box as HTMLElement).click()}
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());for(const key of ['clipboard','permissions'])delete (navigator as any)[key]})
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}
function button(root:ParentNode,text:string){
 const b=[...root.querySelectorAll<HTMLButtonElement>('button,a')].find(b=>(b.getAttribute('aria-label')||b.textContent?.trim())===text)
 expect(b,`button ${text}`).toBeTruthy();return b!
}
async function click(root:ParentNode,text:string){button(root,text).click();await settle()}
async function open(host=mockHost(false)){
 const {root}=mountApp(host.ctx,cleanup);await settle()
 // The App's first load holds its actions; wait for it, not for a tick count.
 await vi.waitFor(()=>expect(button(root,'New download').hasAttribute('disabled')).toBe(false))
 await click(root,'New download')
 const dialog=root.querySelector('[role="dialog"]')!,field=dialog.querySelector<HTMLTextAreaElement>('textarea[aria-label="Links"]')!
 expect(field,'links field').toBeTruthy()
 return {root,host,dialog,field}
}
async function type(field:HTMLTextAreaElement,value:string){field.value=value;field.dispatchEvent(new Event('input',{bubbles:true}));await settle()}
function startButton(dialog:Element){return dialog.closest('[role="dialog"]')!.querySelector<HTMLButtonElement>('[data-add-start]')!}
const group=(dialog:Element,key:string)=>dialog.querySelector(`[data-add-group="${key}"]`)
function clipboard(state:string,text:string){
 const readText=vi.fn(async()=>text)
 Object.defineProperty(navigator,'clipboard',{configurable:true,value:{readText}})
 Object.defineProperty(navigator,'permissions',{configurable:true,value:{query:async()=>({state})}})
 return readText
}

test('the dialog holds only the links field and the location',async()=>{
 const {dialog}=await open()
 expect(dialog.textContent).toContain('/home/Downloads')
 for(const gone of ['Filename','Choose background folder','Refresh destination','Accepts HTTP/HTTPS','Or upload a .torrent file'])expect(dialog.textContent).not.toContain(gone)
 expect(dialog.querySelectorAll('input:not([type="file"]),select')).toHaveLength(0)
 expect(startButton(dialog).textContent?.trim()).toBe('Add');expect(startButton(dialog).disabled).toBe(true)
 expect(dialog.textContent).not.toMatch(/Add paused|New folder/)
})
// The grouped link list needs a fixed width that does not jump while links are typed.
test('the dialog has the large DSM width',async()=>{
 const {dialog}=await open()
 expect(dialog.getAttribute('data-ui-dialog-size')).toBe('lg')
 expect(dialog.textContent).toContain('Each link becomes its own download')
})
test('typed links fall into groups by the backend kind and lines without a link are flagged',async()=>{
 const {dialog,field,host}=await open()
 await type(field,'https://example.com/a.zip https://www.youtube.com/watch?v=1\nhello there\nhttps://unsupported.example/x')
 await vi.waitFor(()=>expect(group(dialog,'none')?.textContent).toContain('https://unsupported.example/x'))
 expect(host.calls.filter(c=>c.method==='downloads.classify').at(-1)?.params).toEqual({urls:['https://example.com/a.zip','https://www.youtube.com/watch?v=1','https://unsupported.example/x']})
 expect([...dialog.querySelectorAll('[data-add-group]')].map(g=>g.getAttribute('data-add-group'))).toEqual(['file','media','none'])
 expect(group(dialog,'file')?.textContent).toContain('Direct');expect(group(dialog,'file')?.textContent).toContain('https://example.com/a.zip')
 expect(group(dialog,'media')?.textContent).toContain('Video / stream (yt-dlp)')
 expect(group(dialog,'none')?.textContent).toContain("Can't download");expect(group(dialog,'none')?.textContent).toContain('Not recognized')
 // A link that cannot be added has no box to tick.
 expect(group(dialog,'none')?.querySelector('[role="checkbox"],input[type="checkbox"]')).toBeNull()
 expect(dialog.querySelector('[data-add-tally]')?.textContent).toBe('3 links')
 // Lines without a link are an orange notice the owner can close.
 const unknown=dialog.querySelector('[data-add-unknown]')!
 expect(unknown.textContent).toContain('Lines without a link are skipped: hello there')
 expect(unknown.closest('[data-tone="warning"]')??unknown.querySelector('[data-tone="warning"]')??unknown.getAttribute('data-tone')).toBeTruthy()
 button(unknown,'Close').click()
 await vi.waitFor(()=>expect(dialog.querySelector('[data-add-unknown]')?.hasAttribute('hidden')).toBe(true))
 expect(startButton(dialog).textContent?.trim()).toBe('Add 2 tasks');expect(startButton(dialog).disabled).toBe(false)
})
test('numbered patterns expand, repeats are dropped and the tally says so',async()=>{
 const {dialog,field,host}=await open()
 await type(field,'https://a.example/ep[01-03].mp4\nhttps://a.example/ep02.mp4\nhttps://a.example/ep02.mp4')
 await vi.waitFor(()=>expect(startButton(dialog).textContent?.trim()).toBe('Add 3 tasks'))
 expect(dialog.querySelector('[data-add-tally]')?.textContent).toBe('3 links · 2 duplicates removed · 3 from patterns')
 await vi.waitFor(()=>expect(host.calls.filter(c=>c.method==='downloads.classify').at(-1)?.params.urls).toEqual(['https://a.example/ep01.mp4','https://a.example/ep02.mp4','https://a.example/ep03.mp4']))
 expect(dialog.querySelector('[data-add-cap]')).toBeNull()
})
// Rendering hundreds of rows in jsdom takes a while.
test('patterns and batches show their caps',{timeout:30000},async()=>{
 const {dialog,field}=await open()
 await type(field,'https://a.example/[1-500].jpg')
 await vi.waitFor(()=>expect(dialog.querySelector('[data-add-cap]')?.textContent).toContain('A pattern adds at most 100 links'))
 expect(startButton(dialog).textContent?.trim()).toBe('Add 100 tasks')
 await type(field,['a','b','c'].map(x=>`https://a.example/${x}[1-100].jpg`).join('\n'))
 await vi.waitFor(()=>expect(dialog.querySelector('[data-add-cap]')?.textContent).toContain('At most 256 links at once'))
 expect(startButton(dialog).textContent?.trim()).toBe('Add 256 tasks')
})
test('unticked links and groups are left out of the batch',async()=>{
 const {dialog,field,host}=await open()
 await type(field,'https://example.com/a.zip\nhttps://example.com/b.zip\nhttps://www.youtube.com/watch?v=1')
 await vi.waitFor(()=>expect(group(dialog,'media')).toBeTruthy())
 tick(dialog.querySelector('[data-add-row="https://example.com/b.zip"]')!);await settle()
 expect(group(dialog,'file')?.querySelector('[data-add-group-check]')?.getAttribute('aria-checked')??group(dialog,'file')?.querySelector('[aria-checked]')?.getAttribute('aria-checked')).toBe('mixed')
 expect(startButton(dialog).textContent?.trim()).toBe('Add 2 tasks')
 tick(group(dialog,'media')!.querySelector('header')!);await settle()
 expect(startButton(dialog).textContent?.trim()).toBe('Add 1 task')
 startButton(dialog).click();await vi.waitFor(()=>expect(host.calls.some(c=>c.method==='downloads.create_many')).toBe(true))
 expect(host.calls.find(c=>c.method==='downloads.create_many')?.params.items.map((i:any)=>i.url)).toEqual(['https://example.com/a.zip'])
})
test('loopback and private addresses are named as blocked, not as unrecognized',async()=>{
 const {dialog,field}=await open()
 await type(field,'https://example.com/a.zip\nhttp://127.0.0.1:5000/a.zip')
 await vi.waitFor(()=>expect(group(dialog,'none')?.textContent).toContain('http://127.0.0.1:5000/a.zip'))
 expect(group(dialog,'none')?.textContent).toContain('Loopback or private network address')
 expect(dialog.textContent).not.toContain('Not recognized')
 expect(startButton(dialog).textContent?.trim()).toBe('Add 1 task')
})
test('a manual paste keeps only the links',async()=>{
 const {field}=await open()
 const paste=new Event('paste',{bubbles:true,cancelable:true})
 Object.defineProperty(paste,'clipboardData',{value:{getData:()=>'See https://a.example/1, and magnet:?xt=urn:btih:'+'a'.repeat(40)+' thanks!'}})
 field.dispatchEvent(paste);await settle()
 expect(paste.defaultPrevented).toBe(true)
 expect(field.value).toBe('https://a.example/1\nmagnet:?xt=urn:btih:'+'a'.repeat(40))
})
test('links on the clipboard fill the field when reading is allowed and clear in one click',async()=>{
 clipboard('granted','copied https://a.example/1 and https://a.example/2')
 const {dialog,field}=await open()
 expect(field.value).toBe('https://a.example/1\nhttps://a.example/2')
 expect(dialog.textContent).toContain('Pasted from the clipboard')
 await click(dialog,'Clear')
 expect(field.value).toBe('');expect(dialog.textContent).not.toContain('Pasted from the clipboard')
})
test('a clipboard that needs a prompt is left alone without a message',async()=>{
 const readText=clipboard('prompt','https://a.example/1')
 const {dialog,field}=await open()
 expect(readText).not.toHaveBeenCalled();expect(field.value).toBe('')
 expect(dialog.querySelector('[role="alert"]')).toBeNull()
})
test('start sends the links in one batch and keeps the failed ones with their reason',async()=>{
 const {root,dialog,field,host}=await open()
 await type(field,'https://example.com/a.zip\nhttps://fail.example/b.zip\nhttps://www.youtube.com/watch?v=1\nhttps://unsupported.example/x')
 await vi.waitFor(()=>expect(startButton(dialog).textContent?.trim()).toBe('Add 3 tasks'))
 startButton(dialog).click();await vi.waitFor(()=>expect(dialog.textContent).toContain('1 link could not be added'))
 const batch=host.calls.filter(c=>c.method==='downloads.create_many')
 expect(batch).toHaveLength(1)
 expect(batch[0].params.path).toBe('/home/Downloads')
 expect(batch[0].params.items.map((i:any)=>i.url)).toEqual(['https://example.com/a.zip','https://fail.example/b.zip','https://www.youtube.com/watch?v=1'])
 expect(new Set(batch[0].params.items.map((i:any)=>i.key)).size).toBe(3)
 expect(dialog.querySelector('[role="alert"]')?.textContent).toContain('https://fail.example/b.zip')
 expect(dialog.querySelector('[role="alert"]')?.textContent).toContain('private network')
 expect(field.value).toBe('https://fail.example/b.zip')
 expect(root.textContent).toContain('Added 2 downloads')
})
test('when every link is added the dialog closes and the list refreshes',async()=>{
 const {root,dialog,field,host}=await open()
 await type(field,'https://example.com/a.zip')
 const reads=host.calls.filter(c=>c.params?.path==='/api/v1/tasks').length
 startButton(dialog).click();await vi.waitFor(()=>expect(root.querySelector('[role="dialog"]')).toBeNull())
 expect(root.textContent).toContain('Added 1 download')
 expect(host.calls.filter(c=>c.params?.path==='/api/v1/tasks').length).toBeGreaterThan(reads)
})
test('without a usable location the dialog says so in one line and offers to choose again',async()=>{
 const host=mockHost(false),original=host.ctx.call
 host.ctx.call=async(method:string,params:any)=>method==='downloads.location'?{key:'save',status:'reselection-required',selection:{scope:'home',root:'/home',generation:2},destination:null}:original(method,params)
 const {dialog,field}=await open(host)
 await type(field,'https://example.com/a.zip')
 expect(dialog.textContent).toContain('Scope or access changed')
 expect(button(dialog,'Choose again').disabled).toBe(false)
 expect(startButton(dialog).disabled).toBe(false)
 startButton(dialog).click();await settle()
 expect(document.body.textContent).toContain('Save downloads to')
 expect(host.calls.some(c=>c.method==='downloads.create_many')).toBe(false)
})
test('a dropped or chosen .torrent file becomes a torrent link',async()=>{
 const {dialog,field,host}=await open()
 const drop=new Event('drop',{bubbles:true,cancelable:true})
 Object.defineProperty(drop,'dataTransfer',{value:{files:[new File([new Uint8Array([100,101])],'album.torrent'),new File(['x'],'notes.txt')]}})
 field.dispatchEvent(drop)
 await vi.waitFor(()=>expect(dialog.textContent).toContain('album.torrent'))
 expect(drop.defaultPrevented).toBe(true);expect(dialog.textContent).not.toContain('notes.txt')
 expect(group(dialog,'torrent')?.textContent).toContain('album.torrent');expect(dialog.querySelector('[data-add-tally]')?.textContent).toBe('1 link')
 const upload=dialog.querySelector<HTMLInputElement>('input[type="file"]')!;expect(upload.accept).toContain('.torrent')
 expect(button(dialog,'Open file')).toBeTruthy()
 Object.defineProperty(upload,'files',{value:[new File([new Uint8Array([100])],'single.torrent')]});upload.dispatchEvent(new Event('change',{bubbles:true}))
 await vi.waitFor(()=>expect(dialog.textContent).toContain('single.torrent'))
 await click(dialog,'Remove single.torrent')
 expect(dialog.textContent).not.toContain('single.torrent')
 startButton(dialog).click();await vi.waitFor(()=>expect(host.calls.some(c=>c.method==='downloads.create_many')).toBe(true))
 expect(host.calls.find(c=>c.method==='downloads.create_many')?.params.items.map((i:any)=>i.url)).toEqual(['data:application/x-bittorrent;base64,ZGU='])
})
test('an accepted preparing task closes the dialog without waiting for metadata',async()=>{
 const host=mockHost(false),original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>method==='downloads.create_many'?{results:p.items.map((i:any)=>({key:i.key,status:'created',ids:['prepare-1']}))}:original(method,p)
 const {root,dialog,field}=await open(host)
 await type(field,'magnet:?xt=urn:btih:'+'a'.repeat(40))
 startButton(dialog).click()
 await vi.waitFor(()=>expect(root.querySelector('[role="dialog"]')).toBeNull())
 expect(root.textContent).toContain('Added 1 download')
})
// DSM is often reached over plain HTTP, where browsers have no clipboard API,
// and elsewhere a read needs a browser prompt: Ctrl+V or ⌘V is the way in.
test('the dialog offers no clipboard button that could only fail',async()=>{
 clipboard('prompt','https://a.example/1')
 const {dialog}=await open()
 expect(dialog.textContent).not.toContain('Paste from clipboard')
})
test('the location appears once in the dialog',async()=>{
 const {dialog}=await open()
 expect(dialog.querySelectorAll('.ui-path-display')).toHaveLength(1)
 expect(dialog.textContent!.split('/home/Downloads')).toHaveLength(2)
})
// A refused add is not a save waiting on the executor: nothing was created,
// so nothing resumes on its own.
test.each(['EXECUTOR_UNAVAILABLE','EXECUTOR_TIMEOUT','queue-full'])('a link refused with %s says to try again later, not that it resumes',async code=>{
 const host=mockHost(false),original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>method==='downloads.create_many'?{results:p.items.map((i:any)=>({key:i.key,status:'failed',code}))}:original(method,p)
 const {dialog,field}=await open(host)
 await type(field,'https://a.example/1.zip')
 startButton(dialog).click();await vi.waitFor(()=>expect(dialog.querySelector('[role="alert"]')).not.toBeNull())
 const alert=dialog.querySelector('[role="alert"]')!.textContent!
 expect(alert).toContain('Background saving is not available right now. Try again later')
 expect(alert).not.toContain('resume automatically')
})
// SDK 0.10.7: a stopped or starting App refuses the add in the SDK's
// action words: the links stay, so the copy says to try again rather than
// that the page updates by itself.
const action=(l:'stopped'|'starting')=>lifecycleCopy(l,'en',{for:'action'})
test.each([['APP_STOPPED','stopped'],['APP_STARTING','starting'],['EXECUTOR_STARTING','starting']] as const)('a %s refusal uses the SDK action wording',async(code,lifecycle)=>{
 const host=mockHost(false),original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>{if(method==='downloads.create_many')throw {code};return original(method,p)}
 const {dialog,field}=await open(host)
 await type(field,'https://a.example/1.zip')
 startButton(dialog).click();await vi.waitFor(()=>expect(dialog.textContent).toContain(action(lifecycle)))
 expect(dialog.textContent).not.toContain(lifecycleCopy(lifecycle,'en'))
})
test.each([['APP_STOPPED','stopped'],['APP_STARTING','starting']] as const)('a link refused with %s keeps the SDK action wording',async(code,lifecycle)=>{
 const host=mockHost(false),original=host.ctx.call
 host.ctx.call=async(method:string,p:any)=>method==='downloads.create_many'?{results:p.items.map((i:any)=>({key:i.key,status:'failed',code}))}:original(method,p)
 const {dialog,field}=await open(host)
 await type(field,'https://a.example/1.zip')
 startButton(dialog).click();await vi.waitFor(()=>expect(dialog.textContent).toContain('https://a.example/1.zip — '+action(lifecycle)))
})
// The header, footer and location rows wrap through the UI Space.
test('the dialog rows wrap with a UI Space',async()=>{
 const {dialog}=await open()
 for(const row of ['.add-links-head','.add-links-foot','.add-location']){
  const el=dialog.querySelector<HTMLElement>(row)!;expect(el.style.flexWrap,row).toBe('wrap')
 }
 expect(dialog.querySelector<HTMLElement>('.add-location')!.style.justifyContent).toBe('flex-start')
})

// A .txt list adds its links to the field; text that is not a link stays out.
test('an opened or dropped .txt list adds its links',async()=>{
 const {dialog,field}=await open()
 await type(field,'https://example.org/a.iso')
 const upload=dialog.querySelector<HTMLInputElement>('input[type="file"]')!;expect(upload.accept).toContain('.txt')
 Object.defineProperty(upload,'files',{value:[new File(['https://example.org/b.iso\nnot a link\nmagnet:?xt=urn:btih:'+'b'.repeat(40)+'\n'],'urls.txt',{type:'text/plain'})]});upload.dispatchEvent(new Event('change',{bubbles:true}))
 await vi.waitFor(()=>expect(field.value).toBe('https://example.org/a.iso\nhttps://example.org/b.iso\nmagnet:?xt=urn:btih:'+'b'.repeat(40)))
 expect(dialog.textContent).not.toContain('urls.txt')
})
test('a .txt list larger than 256 KiB is refused with its reason',async()=>{
 const {dialog,field}=await open()
 const upload=dialog.querySelector<HTMLInputElement>('input[type="file"]')!
 Object.defineProperty(upload,'files',{value:[new File(['x'.repeat(256*1024+1)],'big.txt')]});upload.dispatchEvent(new Event('change',{bubbles:true}))
 await vi.waitFor(()=>expect(dialog.textContent).toContain('larger than 256 KiB'))
 expect(field.value).toBe('')
})

const chooseMagnet='magnet:?xt=urn:btih:'+'c'.repeat(40)+'&dn=choose'

test('file choice sits in the BitTorrent group, is off by default and then opens no file list',async()=>{
 try{localStorage.clear()}catch{}
 const {root,dialog,field}=await open()
 expect(dialog.querySelector('[data-choose-files]')).toBeNull()
 await type(field,chooseMagnet)
 const box=await vi.waitFor(()=>{const b=group(dialog,'torrent')?.querySelector('header [data-choose-files]');expect(b).toBeTruthy();return b!})
 expect(box.closest('[data-ui-checkbox]')?.textContent).toContain('Choose files after adding')
 expect(box.getAttribute('data-state')).toBe('unchecked')
 startButton(dialog).click()
 await vi.waitFor(()=>expect(root.querySelector('[role="dialog"]')).toBeNull())
 await settle()
 expect(document.querySelector('[role="dialog"]')).toBeNull()
})

test('with file choice on, a new torrent opens its files to choose and the choice is sent',async()=>{
 try{localStorage.clear()}catch{}
 const {host,dialog,field}=await open()
 await type(field,chooseMagnet)
 await vi.waitFor(()=>expect(dialog.querySelector('[data-choose-files]')).toBeTruthy())
 tick(dialog.querySelector('[data-choose-files]')!);await settle()
 startButton(dialog).click()
 // The new torrent's detail panel opens on its files.
 const details=await vi.waitFor(()=>{const d=document.querySelector('[data-task-panel]');expect(d?.querySelector('[data-details-files]')).toBeTruthy();return d!})
 const apply=details.querySelector<HTMLButtonElement>('[data-apply-files]')!
 expect(apply.textContent).toContain('(2)');expect(apply.disabled).toBe(true)
 const row=[...details.querySelectorAll('[data-details-files] tbody tr')].find(r=>r.textContent?.includes('b.flac'))!
 tick(row);await settle()
 expect(apply.textContent).toContain('(1)');expect(apply.disabled).toBe(false)
 apply.click();await settle()
 await vi.waitFor(()=>expect(host.calls.find(c=>c.method==='downloads.select_files')?.params).toEqual({id:'bt-0',files:[0]}))
 await vi.waitFor(()=>expect(apply.disabled).toBe(true))
 // The choice to see file lists stays for this user.
 expect(localStorage.getItem('SynoPkgGopeed.chooseFiles.normal1')).toBe('1')
})
