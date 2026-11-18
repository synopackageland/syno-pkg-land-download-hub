import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<25;i++){await Promise.resolve();await nextTick()}}
const button=(root:ParentNode,label:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.getAttribute('aria-label')===label||b.textContent?.trim()===label)!
async function open(catalog:()=>Promise<any[]>,installed:any[]=[]){
 const host=mockHost(true),call=host.ctx.call
 host.ctx.call=(async(method:string,params:any={})=>{
  if(method==='gopeed.plugins.catalog')return catalog()
  if(method==='gopeed.plugins.list')return structuredClone(installed)
  return call(method,params)
 }) as typeof call
 const {root}=mountApp(host.ctx,cleanup,'width:1280px;height:700px');await settle()
 button(root,'Extensions').click();await settle()
 return {root,host}
}
const store=[
 {id:'Gwkang@gopeed-extension-gofile',title:'Gofile 下载器',version:'1.0.6',url:'https://github.com/iGwkang/gopeed-extension-gofile',repoUrl:'https://github.com/iGwkang/gopeed-extension-gofile',icon:'https://raw.githubusercontent.com/iGwkang/gopeed-extension-gofile/main/icon.png'},
 {id:'Locon213@gopeed-extension-mediafire',title:'MediaFire Downloader',version:'1.0.0',url:'https://github.com/Locon213/gopeed-extension-mediafire',repoUrl:'https://github.com/Locon213/gopeed-extension-mediafire'},
 {id:'other@sample',title:'Other sample',version:'1.0.0',url:'https://github.com/other/sample'},
]
test('Recommended lists the store file hosts, installs only on an admin click and leaves them out of the catalog below',async()=>{
 const {root,host}=await open(async()=>structuredClone(store),[{identity:'Locon213@gopeed-extension-mediafire',title:'MediaFire Downloader',version:'1.0.0',disabled:true}])
 const section=root.querySelector('[data-section="recommended"]')!
 expect(section.querySelector('h2,h3,[role="heading"]')?.textContent?.trim()??section.textContent).toContain('Recommended')
 expect([...section.querySelectorAll('[data-recommended]')].map(e=>e.getAttribute('data-recommended'))).toEqual(['gofile','mediafire'])
 expect(section.querySelector('[data-recommended="gofile"]')!.textContent).toContain('Download Gofile share links')
 expect(section.querySelector('[data-recommended="mediafire"]')!.textContent).toContain('Installed')
 expect(section.querySelector('[data-recommended="mediafire"] button')).toBeNull()
 // DSM's img-src refuses the store's GitHub-hosted icon, so the card shows the glyph and requests nothing.
 expect(section.querySelector('[data-recommended="gofile"] img')).toBeNull()
 expect(section.querySelector('[data-recommended="gofile"] .gopeed-recommended-glyph')).not.toBeNull()
 // Nothing installs by itself.
 expect(host.calls.some(c=>c.method==='gopeed.plugins.install')).toBe(false)
 expect(root.querySelector('[data-section="catalog"]')!.textContent).not.toContain('Gofile')
 button(section,'Install Gofile 下载器').click();await settle()
 const dialog=root.querySelector('[role="dialog"]')!
 expect(dialog.textContent).toContain('1.0.6')
 button(dialog,'Confirm').click();await settle()
 expect(host.calls.find(c=>c.method==='gopeed.plugins.install')?.params).toEqual({url:'https://github.com/iGwkang/gopeed-extension-gofile',consent:true})
})
test('an unreadable catalog hides Recommended without a banner of its own',async()=>{
 const {root}=await open(async()=>{throw {code:'catalog_unavailable'}})
 expect(root.querySelector('[data-section="recommended"]')).toBeNull()
 expect(root.querySelectorAll('[role="alert"]')).toHaveLength(0)
})
test('a store without the recommended entries shows no Recommended section',async()=>{
 const {root}=await open(async()=>[store[2]])
 expect(root.querySelector('[data-section="recommended"]')).toBeNull()
})
