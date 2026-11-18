import {expect,test,vi} from 'vitest'
import {startSearchOperation,followSearchJob} from '../src/search-jobs'

test('busy admission exposes the existing id and never claims the new input was accepted',async()=>{
 const call=vi.fn(async()=>({outcome:'busy',job:{id:'original',state:'running',operation:'search'}}))
 await expect(startSearchOperation(call,'search.jobs.start',{key:'new',sourceIds:['a'],query:'new',page:1})).rejects.toMatchObject({code:'search_busy',jobId:'original'})
 expect(call).toHaveBeenCalledTimes(1)
})
test('reconnect reads partial then final pages by original id without resubmitting',async()=>{
 const methods:string[]=[],seen:string[]=[];let polls=0
 const call=async(method:string,params:any)=>{
  methods.push(method);expect(params.id).toBe('job')
  if(method==='search.jobs.status')return {id:'job',operation:'search',state:++polls===1?'running':'succeeded',progress:{done:polls,total:2}}
  if(method==='search.jobs.pages')return {items:[{sourceId:polls===1?'a':'b',results:[]}],offset:params.offset,total:polls}
  return {kind:'search',pages:2}
 }
 await followSearchJob(call,'job',{onPage:p=>seen.push(p.sourceId),pause:async()=>{}})
 expect(seen).toEqual(['a','b']);expect(methods.filter(m=>m.endsWith('.start'))).toEqual([])
})
// Each CGI call costs seconds on a busy NAS: all new pages come in one read.
test('new pages are read in one call',async()=>{
 const reads:any[]=[];let polls=0
 const call=async(method:string,params:any)=>{
  if(method==='search.jobs.status')return {id:'job',operation:'search',state:++polls===1?'running':'succeeded',progress:{done:3,total:3}}
  if(method==='search.jobs.pages'){reads.push(params);return {items:['a','b','c'].slice(params.offset).map(sourceId=>({sourceId,results:[]})),offset:params.offset,total:3}}
  return {kind:'search',pages:3}
 }
 await followSearchJob(call,'job',{pause:async()=>{}})
 expect(reads).toEqual([{id:'job',offset:0,limit:100}])
})
test('lost admission reply reconnects by request key and never resends',async()=>{
 let starts=0
 const call=async(method:string)=>{if(method==='search.jobs.start'){starts++;throw {code:'network'}};return {jobs:[{id:'accepted',key:'original',operation:'search',state:'queued'}]}}
 const out=await startSearchOperation(call,'search.jobs.start',{key:'original'})
 expect(out.job.id).toBe('accepted');expect(starts).toBe(1)
})
test('canceled and interrupted jobs fail without result reads or resubmission',async()=>{
 for(const state of ['canceled','interrupted','failed']){
  const call=vi.fn(async()=>({id:'job',operation:'search',state}))
  await expect(followSearchJob(call,'job')).rejects.toMatchObject({code:'search_job_'+state,jobId:'job'})
  expect(call).toHaveBeenCalledTimes(1)
 }
})
test('aborting polling clears the delay without canceling the backend job',async()=>{
 const controller=new AbortController(),call=vi.fn(async()=>({id:'job',state:'queued',operation:'search'}))
 const pending=followSearchJob(call,'job',{signal:controller.signal});await Promise.resolve();controller.abort()
 await expect(pending).rejects.toMatchObject({name:'AbortError'})
 expect(call).toHaveBeenCalledTimes(1)
})

test('an App input conflict is never recovered as acceptance of a different input',async()=>{
 const call=vi.fn(async(method:string)=>{if(method.endsWith('.start'))throw {code:'operation-conflict'};return {jobs:[{id:'old',key:'reused',operation:'search',state:'succeeded'}]}})
 await expect(startSearchOperation(call,'search.jobs.start',{key:'reused',query:'different'})).rejects.toMatchObject({code:'operation-conflict'})
 expect(call).toHaveBeenCalledTimes(1)
})
