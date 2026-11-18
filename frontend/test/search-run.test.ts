import {expect,test} from 'vitest'
import {searchSources,sourceProblem,type SourcePage} from '../src/search'

test('one asynchronous admission carries all selected sources, not synchronous batches',async()=>{
 const requests:any[]=[],seen:SourcePage[]=[]
 const call=async(method:string,params:any)=>{
  requests.push({method,params})
  if(method==='search.jobs.start')return {outcome:'accepted',job:{id:'original',operation:'search',state:'queued'}}
  if(method==='search.jobs.status')return {id:'original',operation:'search',state:'succeeded'}
  if(method==='search.jobs.pages')return {items:[{sourceId:'a',results:[]}],total:1}
  return {kind:'search',pages:1}
 }
 await searchSources(call,['a','b'],'ubuntu',p=>seen.push(p))
 expect(requests[0]).toMatchObject({method:'search.jobs.start',params:{key:expect.any(String),sourceIds:['a','b'],query:'ubuntu',page:1}})
 expect(requests.filter(x=>x.method.endsWith('.start'))).toHaveLength(1)
 expect(seen).toHaveLength(1)
})
test('a session refusal is not replayed or replaced with a legacy API',async()=>{
 const requests:string[]=[]
 const call=async(method:string)=>{requests.push(method);throw {code:'session_required'}}
 await expect(searchSources(call,['a'],'x',()=>{})).rejects.toMatchObject({code:'session_required'})
 expect(requests).toEqual(['search.jobs.start'])
})
test('each source error keeps its specific problem',()=>{
 expect(['search_cloudflare','search_login','search_auth_unsupported','search_network','search_timeout','search_parse','search_empty','search_busy','search_tool_missing','search_unsupported','search_source_disabled','whatever'].map(sourceProblem))
 .toEqual(['cloudflare','login','login','network','timeout','parse','empty','busy','tool','unsupported','disabled','error'])
})
