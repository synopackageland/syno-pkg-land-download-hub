import {afterEach,expect,test} from 'vitest'
const clone=<T>(value:T):T=>JSON.parse(JSON.stringify(value))
import {nextTick} from 'vue'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
import {defaultMediaOptions} from '../src/media-options'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(f=>f()))
async function settle(){for(let i=0;i<60;i++){await Promise.resolve();await nextTick()}}
const button=(root:ParentNode,label:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!
async function fixture(){
 const host=mockHost(false),base=host.ctx.call,seen:{method:string;params:any}[]=[]
 let prefs={defaults:defaultMediaOptions(),rules:[] as any[],follows:[] as any[]},cookies={domains:[] as {domain:string;count:number}[],importedAt:''}
 host.ctx.call=async(method:string,params:any={})=>{
  if(!method.startsWith('media.')||method.startsWith('media.tools.'))return base(method,params)
  seen.push({method,params})
  if(method==='media.settings.get')return clone(prefs)
  if(method==='media.settings.save'){prefs.defaults=params;return params}
  if(method==='media.cookies.status')return clone(cookies)
  if(method==='media.cookies.import'){cookies={domains:[{domain:'video.example',count:2}],importedAt:'2026-11-01T00:00:00Z'};return clone(cookies)}
  if(method==='media.cookies.delete'){cookies.domains=[];return clone(cookies)}
  if(method==='media.rules.save'){prefs.rules.push({...params,id:'rule-one'});return prefs.rules[0]}
  if(method==='media.follows.add'){prefs.follows.push({...params,id:'follow-one',nextCheck:'2026-11-02T00:00:00Z'});return prefs.follows[0]}
 }
 const {root}=mountApp(host.ctx,clean);await settle();button(root,'Settings').click();await settle();const page=await settingsTab(root,'media',settle);return{root,page,host,seen}
}
test('ordinary users can save their media defaults without global engine settings',async()=>{
 const {page,seen}=await fixture();expect(page).not.toBeNull();expect(page.textContent).toContain('My media defaults');expect(page.textContent).toContain('No followed channels; no background checks')
 const checkbox=page.querySelector<HTMLElement>('[data-media-options] [role=checkbox]')!;checkbox.click();await settle();button(page,'Apply').click();await settle()
 expect(seen.find(c=>c.method==='media.settings.save')?.params.embedThumbnail).toBe(true)
 expect(page.textContent).not.toContain('Download core settings')
})
test('cookie import clears the raw input and renders only metadata, then deletes all',async()=>{
 const {root,page,seen}=await fixture();button(page,'Import cookies').click();await settle()
 const input=root.querySelector<HTMLTextAreaElement>('[data-media-cookie-text]')!;expect(input).not.toBeNull();input.value='# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tid\tprivate-value';input.dispatchEvent(new Event('input'));await settle()
 const form=input.closest('form')!;form.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 expect(seen.find(c=>c.method==='media.cookies.import')?.params.text).toContain('private-value');expect(root.textContent).not.toContain('private-value');expect(root.querySelector('textarea[data-media-cookie-text]')).toBeNull();expect(page.textContent).toContain('video.example');expect(page.textContent).toContain('2')
 button(page,'Delete all cookies').click();await settle();expect(seen.find(c=>c.method==='media.cookies.delete')?.params).toEqual({domain:''});expect(page.textContent).toContain('No imported cookies')
})
test('follow confirmation is opt-in and defaults to a six hour interval',async()=>{
 const {root,page,seen}=await fixture();button(page,'Follow channel').click();await settle()
 const dialog=[...root.querySelectorAll<HTMLElement>('[role=dialog]')].find(d=>d.textContent?.includes('Automatically download'))!
 expect(button(dialog,'Apply').disabled).toBe(true)
 const url=dialog.querySelector<HTMLInputElement>('input[type=url]')!;url.value='https://video.example/channel';url.dispatchEvent(new Event('input'));await settle()
 expect(dialog.querySelector<HTMLInputElement>('input[type=number]')!.value).toBe('6');dialog.querySelector<HTMLElement>('[role=checkbox]')!.click();await settle();dialog.querySelector('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 expect(seen.find(c=>c.method==='media.follows.add')?.params).toEqual({url:'https://video.example/channel',path:'',intervalHours:6,consent:true})
})

test('advanced options send typed choices only for the selected download batch',async()=>{
 const {root,host}=await fixture();button(root,'Downloads').click();await settle();root.querySelector<HTMLButtonElement>('[data-add-download]')!.click();await settle()
 root.querySelector<HTMLButtonElement>('[data-media-advanced]')!.click();await settle()
 const dialog=[...root.querySelectorAll<HTMLElement>('[role=dialog]')].at(-1)!;dialog.querySelector<HTMLElement>('[role=checkbox]')!.click();await settle()
 // The first boolean option belongs to the batch override, not the defaults.
 dialog.querySelector<HTMLElement>('[data-media-options] [role=checkbox]')!.click();await settle();button(dialog,'Apply').click();await settle()
 const field=root.querySelector<HTMLTextAreaElement>('.add-download textarea')!;field.value='https://www.youtube.com/watch?v=fixture';field.dispatchEvent(new Event('input'));await settle();field.closest('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 const submitted=host.calls.find(c=>c.method==='downloads.create_many')?.params
 expect(submitted.mediaOptions.embedThumbnail).toBe(true);expect(submitted.mediaOptions.format).toBe('best');expect(submitted).not.toHaveProperty('args');expect(submitted).not.toHaveProperty('cookies')
})
