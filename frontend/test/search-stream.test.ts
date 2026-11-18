import {expect,test} from 'vitest'
import {followSearchJob} from '../src/search-jobs'

test('a fast source is read while a long source remains in the SDK job',async()=>{
 const controller=new AbortController(),seen:string[]=[]
 const call=async(method:string)=>{
  if(method==='search.jobs.status')return {id:'original',operation:'search',state:'running',progress:{done:1,total:2}}
  if(method==='search.jobs.pages')return {items:[{sourceId:'fast',results:[]}],total:1}
  throw new Error('unexpected synchronous request')
 }
 const start=performance.now()
 await expect(followSearchJob(call,'original',{signal:controller.signal,onPage:p=>{seen.push(p.sourceId);controller.abort()}})).rejects.toMatchObject({name:'AbortError'})
 expect(seen).toEqual(['fast']);expect(performance.now()-start).toBeLessThan(1000)
})
test('a broken poll preserves its original id and never starts another search',async()=>{
 const requests:string[]=[]
 const call=async(method:string)=>{requests.push(method);throw {code:'search_network'}}
 await expect(followSearchJob(call,'original')).rejects.toMatchObject({code:'search_network'})
 expect(requests).toEqual(['search.jobs.status'])
})
