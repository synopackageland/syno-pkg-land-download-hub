import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.useRealTimers()})
async function settle(){for(let i=0;i<24;i++){await Promise.resolve();await nextTick()}}
function button(root:ParentNode,text:string){
 const b=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>(b.getAttribute('aria-label')||b.textContent?.trim())===text)
 expect(b,`button ${text}`).toBeTruthy();return b!
}
async function click(root:ParentNode,text:string){button(root,text).click();await settle()}
async function fixture(answers:Record<string,(p:any)=>any>){
 const host=mockHost(true),original=host.ctx.call
 host.ctx.call=async(method:string,p:any={})=>{if(answers[method]){host.calls.push({method,params:p});return answers[method](p)}return original(method,p)}
 const {root}=mountApp(host.ctx,cleanup);await settle();await click(root,'Core updates')
 return {root,host,yt:root.querySelector('[data-media-tool="yt-dlp"]')!,ff:root.querySelector('[data-media-tool="ffmpeg"]')!}
}
const latest={version:'2026.08.19',sha256:'a'.repeat(64)}
test('a multi-minute tool job spins only its own button and converges without Refresh',async()=>{
 vi.useFakeTimers()
 const state:any={tools:{'yt-dlp':{active:{version:'2026.07.04'}},ffmpeg:{}}}
 const {root,yt,ff}=await fixture({'media.tools.status':()=>structuredClone({state}),
 'media.tools.check':()=>({latest,update:true}),
 'media.tools.update':()=>{state.tools['yt-dlp'].job={request:'q',op:'update',status:'running'};return {request:'q'}}})
 await click(yt,'Check for updates');await click(ff,'Check for updates');await click(yt,'Update');await click(root,'Confirm')
 expect(button(yt,'Update').getAttribute('aria-busy')).toBe('true')
 expect(button(ff,'Install').disabled).toBe(false)
 expect(button(ff,'Check for updates').disabled).toBe(false)
 expect(ff.querySelector('[aria-busy="true"]')).toBeNull()
 expect(root.textContent).not.toContain('Refresh shortly')
 await vi.advanceTimersByTimeAsync(120000);await settle()
 expect(button(yt,'Update').getAttribute('aria-busy')).toBe('true')
 state.tools['yt-dlp']={active:{version:latest.version},job:{request:'q',op:'update',status:'succeeded'}}
 await vi.advanceTimersByTimeAsync(2000);await settle()
 expect(yt.textContent).toContain('Up to date')
 expect(yt.textContent).toContain('Installed')
 expect(yt.querySelector('[aria-busy="true"]')).toBeNull()
 expect(yt.querySelectorAll('button')).toHaveLength(1)
})
test('a failed tool job has readable inline text and allows another attempt',async()=>{
 vi.useFakeTimers()
 const state:any={tools:{ffmpeg:{},'yt-dlp':{}}};let admissions=0
 const {root,ff,yt}=await fixture({'media.tools.status':()=>structuredClone({state}),
 'media.tools.check':()=>({latest,update:true}),
 'media.tools.update':()=>{admissions++;state.tools.ffmpeg.job={request:'q'+admissions,op:'install',status:'queued'};return {request:'q'+admissions}}})
 await click(ff,'Check for updates');await click(ff,'Install');await click(root,'Confirm')
 state.tools.ffmpeg.job={request:'q1',status:'failed',error:'tool_archive_invalid'}
 await vi.advanceTimersByTimeAsync(2000);await settle()
 expect(ff.textContent).toContain('The downloaded archive could not be unpacked safely')
 expect(ff.textContent).not.toContain('tool_archive_invalid')
 expect(yt.textContent).not.toContain('could not be unpacked')
 expect(button(ff,'Install').disabled).toBe(false)
 await click(ff,'Install');await click(root,'Confirm')
 expect(admissions).toBe(2)
 expect(ff.textContent).not.toContain('could not be unpacked')
})
test('a tool receipt updates while the core status is stalled and polling stops at unmount',async()=>{
 vi.useFakeTimers()
 const state:any={tools:{ffmpeg:{job:{request:'q',op:'install',status:'running'}}}}
 const {root,host,ff}=await fixture({'media.tools.status':()=>structuredClone({state}),'binaries.status':()=>new Promise(()=>{})})
 expect(button(ff,'Install').getAttribute('aria-busy')).toBe('true')
 state.tools.ffmpeg={active:{version:'2026-10-29-23-03'},job:{request:'q',op:'install',status:'succeeded'}}
 await vi.advanceTimersByTimeAsync(2000);await settle()
 expect(ff.textContent).toContain('Installed');expect(ff.textContent).toContain('2026.10.29 23:03')
 expect(ff.querySelector('[aria-busy="true"]')).toBeNull()
 const reads=host.calls.filter(c=>c.method==='media.tools.status').length
 cleanup.splice(0).forEach(fn=>fn());await vi.advanceTimersByTimeAsync(10000)
 expect(host.calls.filter(c=>c.method==='media.tools.status')).toHaveLength(reads)
})
test('an admission refusal restores only that card’s button',async()=>{
 const {root,ff,yt}=await fixture({'media.tools.status':()=>({state:{tools:{}}}),
 'media.tools.check':()=>({latest,update:true}),'media.tools.update':()=>{throw {code:'tool_install_failed'}}})
 await click(ff,'Check for updates');await click(ff,'Install');await click(root,'Confirm')
 expect(ff.textContent).toContain('Could not install the tool')
 expect(button(ff,'Install').disabled).toBe(false)
 expect(yt.textContent).not.toContain('Could not install the tool')
})

test('reopening a pending install retains its target and restores Install after failure',async()=>{
 vi.useFakeTimers()
 const state:any={tools:{ffmpeg:{job:{request:'q',op:'install',status:'running',release:latest}}}}
 const {ff}=await fixture({'media.tools.status':()=>structuredClone({state})})
 expect(button(ff,'Install').getAttribute('aria-busy')).toBe('true')
 state.tools.ffmpeg.job={request:'q',op:'install',status:'failed',release:latest,error:'tool_download_failed'}
 await vi.advanceTimersByTimeAsync(2000);await settle()
 expect(button(ff,'Install').disabled).toBe(false)
 expect(ff.textContent).toContain('Check the connection and try again')
})
