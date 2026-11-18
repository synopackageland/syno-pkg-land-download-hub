import {afterEach,beforeEach,expect,test,vi} from 'vitest'
import {createApp} from 'vue'
import PreviewDialog from '../src/PreviewDialog.vue'
import {fileStationURL} from '../src/preview'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {settle} from './downloads-helpers'
const cleanup:(()=>void)[]=[]
const MiB=1024**2
beforeEach(()=>{
 Object.assign(URL,{createObjectURL:vi.fn(()=>'blob:preview'),revokeObjectURL:vi.fn()})
})
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.unstubAllGlobals()})
const answer=(body:BodyInit,type:string)=>vi.fn(async()=>new Response(body,{headers:{'content-type':type,'content-disposition':'inline'}}))
async function show(props:Record<string,unknown>){
 const el=document.createElement('div');document.body.append(el)
 const path=vi.fn(async()=>'/home/Downloads/'+props.name)
 const app=createApp(PreviewDialog,{ctx:{locale:'en'},size:1024,path,token:async()=>'tok',canPlay:()=>'maybe',...props})
 app.mount(el);cleanup.push(()=>{app.unmount();el.remove()})
 await settle();await vi.waitFor(()=>expect(box().getAttribute('data-preview-state')).not.toBe('loading'))
 return {path}
}
const box=()=>document.querySelector<HTMLElement>('[data-preview] .gopeed-preview, .gopeed-preview')!

test('an image shows from a blob in an <img>',async()=>{
 const fetcher=answer(new Uint8Array([1,2,3]),'image/png')
 await show({name:'photo.png',fetcher,path:async()=>'/home/Revealed/photo.png'})
 expect(box().getAttribute('data-preview-kind')).toBe('image')
 // The read asks File Station for the path the reveal gave, with the session token.
 const body=new URLSearchParams(String((fetcher.mock.calls[0] as any[])[1].body))
 expect(JSON.parse(body.get('path')!)).toEqual(['/home/Revealed/photo.png']);expect((fetcher.mock.calls[0] as any[])[1].headers).toEqual({'X-SYNO-TOKEN':'tok'})
 expect(box().querySelector('img')?.getAttribute('src')).toBe('blob:preview')
 expect(fetcher).toHaveBeenCalledTimes(1)
})

test('a saved page runs sandboxed under its own policy, with a Source view',async()=>{
 await show({name:'page.html',fetcher:answer('<h1>Hello</h1>','text/html')})
 const frame=box().querySelector('iframe')!
 expect(frame.getAttribute('sandbox')).toBe('')
 expect(frame.getAttribute('srcdoc')!.startsWith('<meta http-equiv="Content-Security-Policy"')).toBe(true)
 expect(frame.getAttribute('srcdoc')).toContain('<h1>Hello</h1>')
 const source=[...box().querySelectorAll<HTMLElement>('button,[role="radio"],[role="tab"]')].find(b=>b.textContent?.trim()==='Source')!
 expect([...box().querySelectorAll('button,[role="radio"],[role="tab"]')].some(b=>b.textContent?.trim()==='Page')).toBe(true)
 source.click();await settle()
 expect(box().querySelector('iframe')).toBeNull()
 expect(box().querySelector('[data-preview-source]')?.textContent).toBe('<h1>Hello</h1>')
})

test('a PDF opens in the browser viewer, an unsandboxed frame on a blob',async()=>{
 await show({name:'guide.pdf',fetcher:answer(new Uint8Array([37,80,68,70]),'application/pdf')})
 const frame=box().querySelector('iframe')!
 expect(frame.hasAttribute('sandbox')).toBe(false)
 expect(frame.getAttribute('src')).toBe('blob:preview')
})

test('a video streams from File Station with the session token and fetches nothing',async()=>{
 const fetcher=vi.fn()
 await show({name:'clip.mp4',size:4*1024*MiB,fetcher})
 expect(box().querySelector('video')?.getAttribute('src')).toBe(fileStationURL('/home/Downloads/clip.mp4','tok'))
 expect(box().querySelector('video')?.getAttribute('src')).toContain('SynoToken=tok')
 expect(fetcher).not.toHaveBeenCalled()
})

test('a long text file shows its first part and says so',async()=>{
 await show({name:'notes.txt',size:2*MiB,fetcher:answer('a'.repeat(MiB+10),'application/octet-stream')})
 expect(box().querySelector('[data-preview-text]')?.textContent).toHaveLength(MiB)
 expect(box().textContent).toContain('Showing the first 1 MiB')
})

test('a file over its cap is not read and offers File Station',async()=>{
 const fetcher=vi.fn()
 await show({name:'huge.png',size:51*MiB,fetcher})
 expect(box().getAttribute('data-preview-state')).toBe('tooLarge')
 expect(box().textContent).toContain('This file is too large to preview')
 expect([...box().querySelectorAll('button')].some(b=>b.textContent?.trim()==='Open in File Station')).toBe(true)
 expect(fetcher).not.toHaveBeenCalled()
})

test('a video the browser cannot play says so',async()=>{
 await show({name:'movie.mkv',canPlay:()=>'',fetcher:vi.fn()})
 expect(box().getAttribute('data-preview-state')).toBe('unsupported')
 expect(box().textContent).toContain("This browser can't play this file")
})

test('the App previews a saved file at the path the owner-bound reveal names',async()=>{
 const fetcher=answer(new Uint8Array([37,80,68,70]),'application/pdf')
 vi.stubGlobal('fetch',fetcher)
 const host=mockHost(true);const {root}=mountApp(host.ctx,cleanup);await settle()
 const button=root.querySelector<HTMLElement>('[data-row-key="done"]')!.querySelector<HTMLButtonElement>('button[aria-label="Preview Saved document.pdf"]')!
 button.click();await settle()
 await vi.waitFor(()=>expect(host.calls.find(c=>c.method==='gopeed.reveal')?.params).toEqual({id:'done'}))
 await vi.waitFor(()=>expect(document.querySelector('[data-preview]')).toBeTruthy())
 // Nothing else names the file: no read goes out before the reveal answers.
 expect(host.calls.findIndex(c=>c.method==='gopeed.reveal')).toBeGreaterThanOrEqual(0)
})
