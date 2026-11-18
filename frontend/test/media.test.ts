import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mediaRetryable} from '../src/media'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}
function button(root:ParentNode,text:string){
 const b=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>(b.getAttribute('aria-label')||b.textContent?.trim())===text)
 expect(b,`button ${text}`).toBeTruthy();return b!
}
async function click(root:ParentNode,text:string){button(root,text).click();await settle()}
async function fixture(admin=true,answers:Record<string,(p:any)=>any>={}){
 const host=mockHost(admin),original=host.ctx.call
 host.ctx.call=async(method:string,p:any={})=>{if(answers[method]){host.calls.push({method,params:p});return answers[method](p)}return original(method,p)}
 const {root}=mountApp(host.ctx,cleanup);await settle()
 return {root,host}
}

test('failed media downloads can be retried',()=>{
 expect(mediaRetryable({protocol:'media',status:'error'})).toBe(true);expect(mediaRetryable({status:'error'})).toBe(false)
})
test('failed media tasks show the reason and can be retried',async()=>{
 const {root,host}=await fixture(false)
 host.tasks.push({id:'media-1',protocol:'media',status:'error',meta:{opts:{path:'/home/Downloads'},res:{name:'Clip'}},publication:{error:'media_geo_restricted'},media:{title:'Clip'}})
 await click(root,'Refresh')
 expect(root.textContent).toContain('not available in the NAS location')
 await click(root,'Retry')
 expect(host.calls.some(c=>c.params?.path==='/api/v1/tasks/media-1/continue')).toBe(true)
})
const tools={state:{request:'q',tool:'yt-dlp',tools:{'yt-dlp':{active:{version:'2026.07.04'},previous:{version:'2026.06.01'}},ffmpeg:{}}},pending:false,arch:'amd64'}
test('administrators check, update and roll back media tools only after confirmation',async()=>{
 const check={current:'2026.07.04',latest:{version:'2026.08.19',sha256:'a'.repeat(64)},update:true}
 const {root,host}=await fixture(true,{'media.tools.status':()=>tools,'media.tools.check':()=>check,'media.tools.update':()=>({request:'q'}),'media.tools.rollback':()=>({request:'q'})})
 await click(root,'Core updates')
 const section=root.querySelector('[data-media-tool="yt-dlp"]')!;expect(section.textContent).toContain('2026.07.04');expect(section.textContent).toContain('2026.06.01')
 await click(section,'Check for updates')
 expect(section.textContent).toContain('2026.08.19')
 await click(section,'Update');expect(host.calls.some(c=>c.method==='media.tools.update')).toBe(false)
 await click(root,'Confirm')
 expect(host.calls.find(c=>c.method==='media.tools.update')?.params).toEqual({tool:'yt-dlp',version:'2026.08.19',sha256:'a'.repeat(64),consent:true})
 await click(section,'Restore previous version');await click(root,'Confirm')
 expect(host.calls.find(c=>c.method==='media.tools.rollback')?.params).toEqual({tool:'yt-dlp',consent:true})
 const ffmpeg=root.querySelector('[data-media-tool="ffmpeg"]')!;expect(ffmpeg.textContent).toContain('Not installed')
})
test('a submitted tool update marks its own button pending without a banner',async()=>{
 let pending=false
 const check={current:'2026.07.04',latest:{version:'2026.08.19',sha256:'a'.repeat(64)},update:true}
 const {root}=await fixture(true,{'media.tools.status':()=>({...tools,pending}),'media.tools.check':()=>check,'media.tools.update':()=>{pending=true;return {request:'q'}}})
 await click(root,'Core updates')
 const section=root.querySelector('[data-media-tool="yt-dlp"]')!
 await click(section,'Check for updates');await click(section,'Update');await click(root,'Confirm')
 expect(section.querySelector('[aria-busy="true"]')).not.toBeNull()
 expect(root.textContent).not.toContain('Refresh shortly')
 expect(root.textContent).not.toContain('The request was sent')
})
test('tool check and installation failures are explained on the card',async()=>{
 const {root}=await fixture(true,{'media.tools.status':()=>({...tools,pending:false,state:{...tools.state,tool:'yt-dlp',error:'tool_install_failed'}}),'media.tools.check':()=>{throw {code:'tool_source_unavailable'}}})
 await click(root,'Core updates')
 const section=root.querySelector('[data-media-tool="yt-dlp"]')!
 expect(section.textContent).toContain('Could not install the tool')
 await click(section,'Check for updates')
 expect(root.textContent).toContain('Could not reach GitHub')
})
test('a GitHub rate limit says when to try again, apart from an unreachable GitHub',async()=>{
 const reset='2026-10-14T02:03:02Z',d=new Date(reset),hhmm=`${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`
 const {root}=await fixture(true,{'media.tools.status':()=>tools,'media.tools.check':(p:any)=>{throw p.tool==='yt-dlp'?{code:'tool_source_rate_limited',message:`GitHub rate limit reached; it resets at ${reset}. Add a GitHub token in Settings > GitHub to raise the limit`}:{code:'tool_release_unverified'}}})
 await click(root,'Core updates')
 const section=root.querySelector('[data-media-tool="yt-dlp"]')!
 await click(section,'Check for updates')
 expect(section.textContent).toContain(`GitHub rate limit reached. It resets at ${hhmm}`)
 expect(button(section,'Add a GitHub token')).toBeTruthy()
 expect(section.textContent).not.toContain('Could not reach GitHub')
 const ffmpeg=root.querySelector('[data-media-tool="ffmpeg"]')!
 await click(ffmpeg,'Check for updates')
 expect(ffmpeg.textContent).toContain('could not be verified')
})
test('normal users never reach media tool management',async()=>{
 const {root,host}=await fixture(false)
 expect(root.textContent).not.toContain('Core updates')
 expect(host.calls.some(c=>c.method.startsWith('media.tools.'))).toBe(false)
})

test('DRM refusal is readable on a failed task',async()=>{
 const {root,host}=await fixture(false)
 host.tasks.push({id:'prepare-drm',status:'error',meta:{opts:{path:'/home/Downloads'},res:{name:'Protected stream'}},publication:{error:'media_drm'}})
 await click(root,'Refresh')
 expect(root.textContent).toContain('DRM-protected content cannot be downloaded')
})

test.each(['amd64','arm64'])('gallery-dl has architecture-specific controls on %s',async arch=>{
 const {root,host}=await fixture(true,{'media.tools.status':()=>({...tools,arch})})
 await click(root,'Core updates');const row=root.querySelector('[data-media-tool="gallery-dl"]')!
 expect(row).not.toBeNull()
 if(arch==='arm64'){expect(row.textContent).toContain('Not available on this model');expect(row.querySelector('button')).toBeNull()}
 else expect(button(row,'Check for updates')).toBeTruthy()
 expect(host.calls.some(c=>c.method==='media.tools.update')).toBe(false)
})
// A failed status read says so once; it never claims an installed tool is missing.
test('an unread tool state is unknown, not "Not installed"',async()=>{
 let fail=true
 const {root}=await fixture(true,{'media.tools.status':()=>{if(fail)throw {code:'internal'};return tools}})
 await click(root,'Core updates')
 const section=()=>root.querySelector('[data-media-tool="yt-dlp"]')!
 expect(section().textContent).not.toContain('Not installed')
 expect(section().textContent).toContain('—')
 fail=false
 await click(root,'Refresh')
 expect(section().textContent).toContain('2026.07.04')
 expect(root.querySelector('[data-media-tool="ffmpeg"]')!.textContent).toContain('Not installed')
})
