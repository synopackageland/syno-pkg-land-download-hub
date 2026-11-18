import type {Call,SourcePage} from './search'
import {requestKey} from '@synopackageland/sdk'

export type SearchJob={id:string;key:string;operation:string;sourceId?:string;state:string;progress?:{phase?:string;done:number;total:number};errorCode?:string;cancelRequested?:boolean;startedAt?:string}
export type SearchAdmission={outcome:'accepted'|'busy';job:SearchJob;duplicate?:boolean}
export type JobOptions={signal?:AbortSignal;onPage?:(page:SourcePage)=>void;onStatus?:(job:SearchJob)=>void;pause?:()=>Promise<void>}
export const searchRequestKey=requestKey
const terminal=new Set(['failed','canceled','interrupted'])
function check(signal?:AbortSignal){if(signal?.aborted)throw new DOMException('Polling stopped','AbortError')}
function delay(signal?:AbortSignal){
 return new Promise<void>((resolve,reject)=>{
  const finish=()=>{clearTimeout(timer);signal?.removeEventListener('abort',abort);resolve()}
  const abort=()=>{clearTimeout(timer);signal?.removeEventListener('abort',abort);reject(new DOMException('Polling stopped','AbortError'))}
  const timer=setTimeout(finish,500);signal?.addEventListener('abort',abort,{once:true});if(signal?.aborted)abort()
 })
}

// Admission only; there is no hidden wait or replay. The original key can
// reconnect after a lost reply, using a fresh authenticated owner-only list.
export async function startSearchOperation(call:Call,method:string,params:Record<string,unknown>):Promise<SearchAdmission>{
 let admission:SearchAdmission
 try{admission=await call(method,params)}catch(error){
  const code=(error as any)?.code
  if(!['network','network_error','search_network','timeout','request_timeout','api_timeout','jobs_busy','jobs_unavailable','canceled'].includes(code))throw error
  const jobs=await call('search.jobs.list',{}).catch(()=>null)
  const job=jobs?.jobs?.find((j:SearchJob)=>j.key===params.key)
  if(!job)throw error
  admission={outcome:'accepted',job,duplicate:true}
 }
 if(admission.outcome==='busy')throw {code:'search_busy',jobId:admission.job.id}
 if(admission.outcome!=='accepted'||!admission.job?.id)throw {code:'search_protocol'}
 return admission
}

// UI polling remains outside each CGI request. Aborting detaches this viewer;
// only an explicit search.jobs.cancel request cancels the durable SDK job.
export async function followSearchJob(call:Call,id:string,options:JobOptions={}){
 let offset=0,startedAt=""
 const until=Date.now()+5*60*1000
 while(Date.now()<until){
  check(options.signal)
  const job:SearchJob=await call('search.jobs.status',{id});check(options.signal)
  if(job.startedAt&&startedAt!==job.startedAt){offset=0;startedAt=job.startedAt}
  options.onStatus?.(job)
  if(terminal.has(job.state))throw {code:'search_job_'+job.state,jobId:id,errorCode:job.errorCode}
  if(job.state==='paused')throw {code:'search_job_paused',jobId:id}
  // A finished job whose every page was already read needs no further read.
  const done=job.progress?.done??0
  if(job.operation==='search'&&(done>offset||(job.state==='succeeded'&&!(done>0&&offset>=done)))){
   for(;;){
    let page:{items:SourcePage[];total:number;nextOffset?:number}
    // Every new page in one call: on a busy NAS each call takes seconds.
    try{page=await call('search.jobs.pages',{id,offset,limit:100})}catch(e:any){if(job.state==='running'&&e?.code==='search_result_unavailable')break;throw e}
    check(options.signal)
    for(const item of page.items)options.onPage?.(item)
    offset+=page.items.length
    if(offset>=page.total||!page.items.length)break
   }
  }
  if(job.state==='succeeded')return call('search.jobs.result',{id})
  if(options.pause)await options.pause();else await delay(options.signal)
 }
 throw {code:'search_timeout',jobId:id}
}
