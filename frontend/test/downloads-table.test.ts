import {afterEach,expect,test} from 'vitest'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {metaOf,rowOf,settle,statusOf} from './downloads-helpers'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(f=>f()))

// A row names the file, its received and total size and speed on one meta
// line, and a bar only while the work is not saved.
test('a row names the file, its size and speed, and shows a bar only while it runs',async()=>{
 const {root}=mountApp(mockHost(true).ctx,clean);await settle()
 const running=rowOf(root,'Example download.zip')!,saved=rowOf(root,'Saved document.pdf')!
 expect(running.querySelector('.gopeed-dl-name')!.textContent).toContain('Example download.zip')
 expect(metaOf(running)).toContain('1 KB / 4 KB')
 expect(metaOf(running)).toContain('512 B/s')
 expect(statusOf(running)).toBe('Downloading')
 expect(running.querySelector('[role="progressbar"]')).not.toBeNull()
 expect(statusOf(saved)).toBe('Saved')
 expect(saved.querySelector('[role="progressbar"]')).toBeNull()
})

test('the list sits on the page without a toolbar frame and the save help is said once, in Settings',async()=>{
 const {root}=mountApp(mockHost(true).ctx,clean);await settle()
 expect(root.querySelector('.list-toolbar')).toBeNull()
 expect(root.querySelector('[data-downloads]')!.textContent).not.toContain('Uncertain or interrupted operations are never replayed')
})

test('the first read of the list shows a loading area, not an empty list',async()=>{
 const host=mockHost(true),original=host.ctx.call;let release=()=>{}
 host.ctx.call=(async(method:string,params:any)=>{if(method==='gopeed.request'&&params?.path==='/api/v1/tasks')await new Promise<void>(r=>{release=r});return original(method,params)}) as typeof original
 const {root}=mountApp(host.ctx,clean);await settle()
 expect(root.querySelector('[data-downloads] [data-downloads-list]')).toBeNull()
 expect(root.querySelector('[data-downloads] [role="status"]')).not.toBeNull()
 release();await settle()
 expect(root.querySelector('[data-downloads] [data-downloads-list]')).not.toBeNull()
})

// A confirmation is the UI's message box: Cancel and Confirm only, and the
// action runs once it is confirmed.
test('clearing a record asks through the UI message box',async()=>{
 const host=mockHost(true),{root}=mountApp(host.ctx,clean);await settle()
 const before=host.tasks.length
 rowOf(root,'Paused download.zip')!.querySelector<HTMLButtonElement>('button[aria-label="Clear download record"]')!.click();await settle()
 const dialog=root.querySelector('[role="dialog"]')!
 expect(dialog.querySelector('.confirm-text')).toBeNull()
 expect([...dialog.querySelectorAll('button')].map(b=>b.textContent?.trim())).toEqual(['Cancel','Confirm'])
 ;[...dialog.querySelectorAll('button')].find(b=>b.textContent?.trim()==='Confirm')!.click();await settle()
 expect(root.querySelector('[role="dialog"]')).toBeNull()
 expect(host.tasks.length).toBe(before-1)
})
// The engine can stay "running" after the last byte arrived, for as long as
// the save behind it cannot start; the row then waits to save at no speed.
test('a running download with every byte received waits to save without a speed',async()=>{
 const host=mockHost(true)
 host.tasks.splice(0,host.tasks.length,{id:'received',status:'running',meta:{opts:{name:'Received.dat',path:'/home/Downloads'},res:{size:4096}},progress:{downloaded:4096,speed:0},publication:{delegated:true,published:false}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const row=rowOf(root,'Received.dat')!
 expect(statusOf(row)).toBe('Waiting to save')
 expect(row.textContent).not.toContain('Downloading')
 expect(row.textContent).not.toContain('B/s')
})
test('a running download of unknown size still downloads',async()=>{
 const host=mockHost(true)
 host.tasks.splice(0,host.tasks.length,{id:'stream',status:'running',meta:{opts:{name:'Stream.dat',path:'/home/Downloads'},res:{size:0}},progress:{downloaded:4096,speed:100}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const row=rowOf(root,'Stream.dat')!
 expect(statusOf(row)).toBe('Downloading')
 expect(row.querySelector('[role="progressbar"]')!.hasAttribute('data-indeterminate')).toBe(true)
})

// A paused download is not working: its bar stands still.
test('a paused download of unknown size has a still bar',async()=>{
 const host=mockHost(true)
 host.tasks.splice(0,host.tasks.length,{id:'held',status:'pause',meta:{opts:{name:'Held.dat',path:'/home/Downloads'},res:{size:0}},progress:{downloaded:0,speed:0}})
 const {root}=mountApp(host.ctx,clean);await settle()
 const row=rowOf(root,'Held.dat')!
 expect(row.querySelector('[role="progressbar"]')!.hasAttribute('data-indeterminate')).toBe(false)
})
