import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {pageLoadTimeout} from '../src/page-load'
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.useRealTimers()})
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}
function button(root:ParentNode,text:string){
 const b=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>(b.getAttribute('aria-label')||b.textContent?.trim())===text)
 expect(b,`button ${text}`).toBeTruthy();return b!
}
async function click(root:ParentNode,text:string){button(root,text).click();await settle()}
type Pending={method:string;params:any;resolve:(v:any)=>void;reject:(e:any)=>void}
// Holds the named methods open until the test answers them.
async function fixture(held:string[],answers:Record<string,(p:any)=>any>={}){
 const host=mockHost(true),original=host.ctx.call,pending:Pending[]=[]
 host.ctx.call=(method:string,params:any={})=>{
  if(held.includes(method)){host.calls.push({method,params});return new Promise((resolve,reject)=>pending.push({method,params,resolve,reject}))}
  if(answers[method]){host.calls.push({method,params});return Promise.resolve().then(()=>answers[method](params))}
  return original(method,params)
 }
 const {root}=mountApp(host.ctx,cleanup);await settle()
 const open=(method:string)=>pending.filter(p=>p.method===method)
 return {root,host,pending,open}
}
const two=[{identity:'one',title:'First extension',version:'1.0.0',disabled:true},{identity:'two',title:'Second extension',version:'2.0.0',disabled:false}]

test('the extensions page renders at once and loads its list and catalog concurrently',async()=>{
 const {root,open}=await fixture(['gopeed.plugins.list','gopeed.plugins.catalog'])
 await click(root,'Extensions')
 expect(root.querySelector('[data-plugins]')).toBeTruthy()
 expect(open('gopeed.plugins.list')).toHaveLength(1)
 expect(open('gopeed.plugins.catalog')).toHaveLength(1)
 expect(root.querySelector('[data-section="installed"] [aria-busy="true"]')).toBeTruthy()
 expect(root.querySelector('[data-section="catalog"] [aria-busy="true"]')).toBeTruthy()
 open('gopeed.plugins.catalog')[0].resolve([{id:'x@y',title:'Catalog entry',description:'',version:'1',url:'https://github.com/a/b'}])
 await settle()
 expect(root.querySelector('[data-section="catalog"]')!.textContent).toContain('Catalog entry')
 expect(root.querySelector('[data-section="installed"] [aria-busy="true"]')).toBeTruthy()
})

test('installed extensions check for updates concurrently and each row shows its own state',async()=>{
 const {root,open}=await fixture(['gopeed.plugins.check'],{'gopeed.plugins.list':()=>structuredClone(two)})
 await click(root,'Extensions')
 expect(open('gopeed.plugins.check').map(p=>p.params.id)).toEqual(['one','two'])
 const rows=[...root.querySelectorAll('[data-section="installed"] [role="listitem"]')]
 for(const row of rows)expect(row.querySelector('[aria-busy="true"]')).toBeTruthy()
 open('gopeed.plugins.check')[1].resolve({newVersion:'2.1.0'});await settle()
 expect(rows[1].textContent).toContain('Available version: 2.1.0')
 expect(rows[1].querySelector('[aria-busy="true"]')).toBeNull()
 expect(rows[0].querySelector('[aria-busy="true"]')).toBeTruthy()
})

test('tabs stay usable while the extensions page loads and late answers do not reach the next page',async()=>{
 const {root,open}=await fixture(['gopeed.plugins.list','gopeed.plugins.catalog'])
 await click(root,'Extensions')
 expect(button(root,'Downloads').disabled).toBe(false)
 await click(root,'Downloads')
 expect(root.querySelector('[data-plugins]')).toBeNull()
 expect(root.textContent).toContain('Example download.zip')
 open('gopeed.plugins.catalog')[0].reject({code:'catalog_unavailable'})
 open('gopeed.plugins.list')[0].resolve(structuredClone(two))
 await settle()
 expect(root.querySelector('[role="alert"]')).toBeNull()
 expect(root.textContent).not.toContain('First extension')
})

test('a catalog that does not answer in time shows a timeout and a retry in its own section',async()=>{
 vi.useFakeTimers({toFake:['setTimeout','clearTimeout']})
 const {root,open}=await fixture(['gopeed.plugins.catalog'])
 await click(root,'Extensions')
 await vi.advanceTimersByTimeAsync(pageLoadTimeout);await settle()
 const catalog=root.querySelector('[data-section="catalog"]')!
 expect(catalog.querySelector('[aria-busy="true"]')).toBeNull()
 expect(catalog.textContent).toContain('did not answer in time')
 expect(root.textContent).toContain('Example extension')
 await click(catalog,'Retry catalog')
 expect(open('gopeed.plugins.catalog')).toHaveLength(2)
})

const tools={state:{tools:{'yt-dlp':{active:{version:'2026.07.04'}},ffmpeg:{},deno:{}}},pending:false,arch:'amd64'}
test('checking one media tool queries only that tool and marks only its row busy',async()=>{
 const {root,host,open}=await fixture(['media.tools.check'],{'media.tools.status':()=>tools})
 await click(root,'Core updates')
 const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!,ffmpeg=root.querySelector('[data-media-tool="ffmpeg"]')!
 await click(ytdlp,'Check for updates')
 expect(host.calls.filter(c=>c.method==='media.tools.check').map(c=>c.params)).toEqual([{tool:'yt-dlp'}])
 expect(ytdlp.querySelector('[aria-busy="true"]')).toBeTruthy()
 expect(ffmpeg.querySelector('[aria-busy="true"]')).toBeNull()
 expect(button(ffmpeg,'Check for updates').disabled).toBe(false)
 await click(ffmpeg,'Check for updates')
 expect(open('media.tools.check').map(p=>p.params.tool)).toEqual(['yt-dlp','ffmpeg'])
 open('media.tools.check')[1].resolve({latest:{version:'7.1',sha256:'b'.repeat(64)},update:true});await settle()
 expect(ffmpeg.textContent).toContain('7.1')
 expect(ytdlp.querySelector('[aria-busy="true"]')).toBeTruthy()
})

test('a failed tool check is shown in its own row',async()=>{
 const {root}=await fixture([],{'media.tools.status':()=>tools,'media.tools.check':()=>{throw {code:'tool_source_unavailable'}}})
 await click(root,'Core updates')
 const ytdlp=root.querySelector('[data-media-tool="yt-dlp"]')!,ffmpeg=root.querySelector('[data-media-tool="ffmpeg"]')!
 await click(ytdlp,'Check for updates')
 expect(ytdlp.textContent).toContain('Could not reach GitHub')
 expect(ffmpeg.textContent).not.toContain('Could not reach GitHub')
})
