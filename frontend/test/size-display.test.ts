import {afterEach,expect,test} from 'vitest'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
import {metaOf,openPanel,rowOf,settle} from './downloads-helpers'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))

// Sizes read as DSM shows them: 1024 base, KB/MB/GB/TB, one decimal at most.
test('download sizes and speeds use DSM units',async()=>{
 const host=mockHost(true)
 host.tasks.push({id:'big',status:'pause',meta:{opts:{name:'Disk image.iso',path:'/home/Downloads'},res:{size:2*1024**4}}})
 const {root}=mountApp(host.ctx,cleanup);await settle()
 expect(metaOf(rowOf(root,'Example download.zip')!)).toContain('/ 4 KB')
 expect(metaOf(rowOf(root,'Disk image.iso')!)).toContain('/ 2 TB')
 expect(metaOf(rowOf(root,'Example download.zip')!)).toContain('512 B/s')
})

test('an unknown size shows a dash',async()=>{
 const {root}=mountApp(mockHost(true).ctx,cleanup);await settle()
 const p=await openPanel(root,'Paused download.zip')
 const kv=[...p.querySelectorAll('.gopeed-panel-kv dt')].find(d=>d.textContent==='Size')!
 expect(kv.nextElementSibling!.textContent).toBe('—')
})

// With no size yet there is nothing to count down to.
test('a download of unknown size shows no time left',async()=>{
 const host=mockHost(true)
 host.tasks.push({id:'nosize',status:'running',meta:{opts:{name:'Streamed.zip',path:'/home/Downloads'},res:{}},progress:{downloaded:12*1024**2,speed:1024**2}})
 const {root}=mountApp(host.ctx,cleanup);await settle()
 expect(metaOf(rowOf(root,'Streamed.zip')!)).toContain('1 MB/s')
 expect(metaOf(rowOf(root,'Streamed.zip')!)).not.toMatch(/left/)
})
