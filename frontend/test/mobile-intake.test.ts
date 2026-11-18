import {afterEach,expect,test,vi} from 'vitest'
import type {LaunchParams} from '@synopackageland/sdk'
import type {NativeIntakeItem} from '@synopackageland/sdk/mobile'
import {intakeDraft} from '../src/mobile-intake'
import {maxTorrentBytes} from '../src/download-submission'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {buttonNamed,openPanel,settle} from './downloads-helpers'

const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());delete (window as any).SynoPkgNative})
const magnet='magnet:?xt=urn:btih:'+'a'.repeat(40)
const torrent:NativeIntakeItem={kind:'file',filename:'album.torrent',mime:'application/x-bittorrent',base64:btoa('d4:infodee'),size:10}

function fixture(items:readonly NativeIntakeItem[],cold=true,shell=true,prepare?:(host:ReturnType<typeof mockHost>)=>void){
 const host=mockHost(false)
 if(shell){host.ctx.host.platform=()=>({host:'native-shell'});host.ctx.fullPage=true}
 let launch:(params:LaunchParams)=>void=()=>{}
 host.ctx.onLaunch=(fn:typeof launch)=>{launch=fn;return ()=>{launch=()=>{}}}
 if(cold)host.ctx.launchParams={link:'/add',intake:'share-cold'}
 let current=items
 const post=vi.fn((message:any)=>{
  if(message.type==='intake.take')window.dispatchEvent(new CustomEvent('synopkgnative',{detail:{v:1,type:'reply',id:message.id,ok:true,items:current}}))
 })
 ;(window as any).SynoPkgNative={protocol:1,shell:{name:'fixture',version:'1',platform:'android'},caps:{download:{url:true,base64:true,maxBase64:52428800},openExternal:true,intake:true,camera:false},post}
 prepare?.(host)
 const {root,app}=mountApp(host.ctx,cleanup)
 return {root,app,host,post,send:(params:LaunchParams)=>launch(params),launch:(id:string,next=items)=>{current=next;launch({link:'/add',intake:id})}}
}
const field=(root:ParentNode)=>root.querySelector<HTMLTextAreaElement>('[role="dialog"] textarea[aria-label="Links"]')
const creates=(host:ReturnType<typeof mockHost>)=>host.calls.filter(c=>c.method==='downloads.create_many')

test.each([
 ['single URL',[{kind:'url',url:'https://example.com/a.zip'}],'https://example.com/a.zip'],
 ['multiple lines',[{kind:'text',text:'https://example.com/a.zip\nhttps://example.com/b.zip'}],'https://example.com/a.zip\nhttps://example.com/b.zip'],
 ['Chrome title with URL',[{kind:'text',text:'Example download\nhttps://example.com/a.zip'}],'https://example.com/a.zip'],
 ['magnet',[{kind:'url',url:magnet}],magnet],
 ['ED2K',[{kind:'url',url:'ed2k://|file|example.zip|1|'+'a'.repeat(32)+'|/'}],'ed2k://|file|example.zip|1|'+'a'.repeat(32)+'|/'],
] as const)('cold %s intake prefills Add without submitting',async(_label,items,expected)=>{
 const {root,host,post}=fixture(items)
 await vi.waitFor(()=>expect(field(root)?.value).toBe(expected))
 expect(creates(host)).toHaveLength(0)
 expect(post.mock.calls.filter(([m])=>m.type==='intake.take')).toHaveLength(1)
})

test('warm intake opens Add, preserves edits on another share, and consumes each handle once',async()=>{
 const f=fixture([],false)
 await settle()
 f.launch('warm-1',[{kind:'url',url:magnet}])
 await vi.waitFor(()=>expect(field(f.root)?.value).toBe(magnet))
 field(f.root)!.value='https://example.com/edited.zip'
 field(f.root)!.dispatchEvent(new Event('input',{bubbles:true}))
 f.launch('warm-2',[{kind:'url',url:'https://example.com/new.zip'}])
 await vi.waitFor(()=>expect(field(f.root)?.value).toBe('https://example.com/edited.zip\nhttps://example.com/new.zip'))
 f.launch('warm-2')
 await settle()
 expect(f.post.mock.calls.filter(([m])=>m.type==='intake.take')).toHaveLength(2)
 expect(creates(f.host)).toHaveLength(0)
})

test('a share arriving during startup is retained',async()=>{
 let release!:()=>void
 const wait=new Promise<void>(r=>{release=r})
 const f=fixture([],false,true,host=>{
  const original=host.ctx.call
  host.ctx.call=async(m:string,p:any)=>{if(m==='gopeed.session')await wait;return original(m,p)}
 })
 f.launch('during-start',[{kind:'url',url:magnet}])
 await settle()
 expect(f.post).not.toHaveBeenCalled()
 release()
 await vi.waitFor(()=>expect(field(f.root)?.value).toBe(magnet))
})

test.each([{items:[{kind:'text',text:'nothing to download'}]},{items:[{kind:'url',url:'javascript:alert(1)'}]},{items:[]}] as {items:NativeIntakeItem[]}[])('unsupported intake is refused without opening Add or creating work: $items',async({items})=>{
 const {root,host}=fixture(items)
 await vi.waitFor(()=>expect(root.textContent).toContain('Share HTTP or HTTPS links'))
 expect(field(root)).toBeNull()
 expect(creates(host)).toHaveLength(0)
})

test('torrent intake opens a file draft and waits for confirmation',async()=>{
 const {root,host}=fixture([torrent])
 await vi.waitFor(()=>expect(root.querySelector('[role="dialog"]')?.textContent).toContain('album.torrent'))
 expect(creates(host)).toHaveLength(0)
 const start=root.querySelector<HTMLButtonElement>('[data-add-start]')!
 await vi.waitFor(()=>expect(start.disabled).toBe(false))
 start.click()
 await vi.waitFor(()=>expect(creates(host)).toHaveLength(1))
 expect(creates(host)[0].params.items[0].url).toBe('data:application/x-bittorrent;base64,'+torrent.base64)
})

test('torrent type, size and decoded length are checked',()=>{
 for(const file of [{...torrent,filename:'a.txt'},{...torrent,size:maxTorrentBytes+1},{...torrent,size:2},{...torrent,base64:'!'}])expect(()=>intakeDraft([file])).toThrow()
})

test('desktop ignores native intake and keeps its existing deep-link Add',async()=>{
 const {root,post,host,send}=fixture([{kind:'url',url:magnet}],true,false)
 await settle()
 expect(field(root)).toBeNull()
 expect(post).not.toHaveBeenCalled()
 send({view:'downloads',add:'https://example.com/desktop.zip'})
 await vi.waitFor(()=>expect(field(root)?.value).toBe('https://example.com/desktop.zip'))
 expect(creates(host)).toHaveLength(0)
 expect(root.querySelector('[data-save-phone]')).toBeNull()
})

test('Save to phone is shell-only and uses the owner-bound path and SDK native File Station transfer',async()=>{
 const {root,host,post}=fixture([],false)
 await settle()
 const p=await openPanel(root,'Saved document.pdf')
 const save=p.querySelector<HTMLButtonElement>('[data-save-phone]')!
 expect(save.textContent).toContain('Save to phone')
 save.click()
 await vi.waitFor(()=>expect(post.mock.calls.some(([m])=>m.type==='download')).toBe(true))
 expect(host.calls.some(c=>c.method==='gopeed.reveal'&&c.params.id==='done')).toBe(true)
 const download=post.mock.calls.find(([m])=>m.type==='download')![0]
 const url=new URL(download.url)
 expect(url.origin).toBe(location.origin)
 expect(url.searchParams.get('api')).toBe('SYNO.FileStation.Download')
 expect(JSON.parse(url.searchParams.get('path')!)).toEqual(['/home/Downloads/document.pdf'])
 expect(download.filename).toBe('document.pdf')
})

test('desktop saved panel has File Station and no Save to phone',async()=>{
 const {root}=fixture([],false,false)
 await settle()
 const p=await openPanel(root,'Saved document.pdf')
 expect(buttonNamed(p,'Open in File Station')).toBeTruthy()
 expect(p.querySelector('[data-save-phone]')).toBeNull()
})

test('a phone browser Portal launch has no Save to phone',async()=>{
 const {root}=fixture([],false,false,host=>{
  host.ctx.fullPage=true
  host.ctx.host.platform=()=>({host:'standalone-web'})
 })
 await settle()
 const p=await openPanel(root,'Saved document.pdf')
 expect(p.querySelector('[data-save-phone]')).toBeNull()
})

test('unsaved and active tasks never offer Save to phone',async()=>{
 const {root}=fixture([],false)
 await settle()
 for(const name of ['Example download.zip','Ready to save.txt']){
  const p=await openPanel(root,name)
  expect(p.querySelector('[data-save-phone]')).toBeNull()
 }
})

test('a finished multi-file task saves only committed files',async()=>{
 const f=fixture([],false)
 const done=f.host.tasks.find(t=>t.id==='done')
 done.publication.files=[{name:'one.pdf',rel:'Album/one.pdf',state:'committed',size:10},{name:'two.pdf',rel:'Album/two.pdf',state:'committed',size:20}]
 await settle()
 const p=await openPanel(f.root,'Saved document.pdf')
 expect(p.querySelector('[data-save-phone]')).toBeNull()
 const buttons=p.querySelectorAll<HTMLButtonElement>('[data-save-phone-file]')
 expect(buttons).toHaveLength(2)
 buttons[1].click()
 await vi.waitFor(()=>expect(f.post.mock.calls.some(([m])=>m.type==='download')).toBe(true))
 const download=f.post.mock.calls.find(([m])=>m.type==='download')![0]
 expect(JSON.parse(new URL(download.url).searchParams.get('path')!)).toEqual(['/home/Downloads/two.pdf'])
})

test('a current DSM file refusal prevents a phone transfer',async()=>{
 const f=fixture([],false),original=f.host.ctx.call
 f.host.ctx.call=async(m:string,p:any)=>{if(m==='fixture.dsm.info')throw {code:'denied'};return original(m,p)}
 await settle()
 const p=await openPanel(f.root,'Saved document.pdf')
 p.querySelector<HTMLButtonElement>('[data-save-phone]')!.click()
 await vi.waitFor(()=>expect(f.host.calls.some(c=>c.method==='gopeed.reveal')).toBe(true))
 await settle()
 expect(f.post.mock.calls.some(([m])=>m.type==='download')).toBe(false)
})

test('an unmounted app cancels intake and removes its launch listener',async()=>{
 const f=fixture([],false)
 await settle()
 f.app.unmount()
 f.launch('after-unmount',[{kind:'url',url:magnet}])
 await settle()
 expect(f.post).not.toHaveBeenCalled()
})
