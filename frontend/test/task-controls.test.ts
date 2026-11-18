import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<25;i++){await Promise.resolve();await nextTick()}}
test('preparing, media and waiting-to-save rows expose pause and resume, including the Downloading tab Pause all',async()=>{
 const host=mockHost(false)
 host.tasks.splice(0,host.tasks.length,...[
  {id:'prep',status:'preparing',preparation:true,meta:{res:{name:'Preparing file'}}},
  {id:'media',status:'pause',protocol:'media',meta:{res:{name:'Media file'}}},
  {id:'save',status:'done',meta:{res:{name:'Saving file'}},publication:{delegated:true,state:'copying',published:false}},
 ])
 const {root}=mountApp(host.ctx,cleanup);await settle()
 expect(root.textContent).toContain('Preparing')
 expect(root.querySelectorAll('button[aria-label="Pause"]')).toHaveLength(2)
 expect(root.querySelectorAll('button[aria-label="Resume"]')).toHaveLength(1)
 // A finished download still saving sits with the downloads, so the active
 // group's Pause all pauses its save as well as the preparing download.
 expect(root.querySelector('[data-row-key="save"][data-group="active"]')).not.toBeNull()
 root.querySelector<HTMLElement>('[data-downloads] [data-ui-tab-value="active"]')!.click();await settle()
 const all=root.querySelector<HTMLButtonElement>('[data-ui-section-tabs-actions] [data-group-action="pause"]')!
 expect(all.textContent?.trim()).toBe('Pause all');all.click()
 await vi.waitFor(()=>expect(host.calls.filter(c=>c.method==='gopeed.request'&&c.params?.method==='PUT')).toHaveLength(2))
 expect(host.calls.filter(c=>c.params?.method==='PUT').map(c=>c.params.path).sort()).toEqual(['/api/v1/tasks/prep/pause','/api/v1/tasks/save/pause'])
 await settle()
 root.querySelector<HTMLElement>('[data-row-key="media"]')!.click();await settle()
 expect(root.textContent).toContain('yt-dlp')
})
