import {format} from '@synopackageland/sdk'
import {appCopy} from './app-copy'

export type LinkItem={key:string;url:string}
export type LinkResult={key:string;url:string;status:'created'|'failed';kind?:string;ids?:string[];code?:string}
type Call=(method:string,params:Record<string,unknown>)=>Promise<any>

// Kinds the backend reports, in summary order.
const kinds=['file','torrent','ed2k','media','extension','blocked','unsupported'] as const
export const counted=(locale:string,key:string,n:number)=>format(appCopy(locale,n===1?key+'.one':key),{n})

// "3 links: 2 files, 1 video page"; links not classified yet count only in the total.
export function linkSummary(list:(string|undefined)[],locale:string){
 if(!list.length)return ''
 const total=counted(locale,'add.count',list.length)
 const parts=kinds.map(k=>[k,list.filter(x=>x===k).length] as const).filter(([,n])=>n).map(([k,n])=>counted(locale,'add.kind.'+k,n))
 return parts.length?format(appCopy(locale,'add.summary'),{count:total,parts:parts.join(appCopy(locale,'add.separator'))}):total
}

export function canStart({count,ready,busy}:{count:number;ready:boolean;busy:boolean}){return count>0&&ready&&!busy}
// "Add 3 tasks": one task per chosen link or torrent file.
export function startLabel(locale:string,count:number){return count?counted(locale,'add.startCount',count):appCopy(locale,'add.start')}

// A key identifies one link of the dialog across repeated batches; the
// backend answers a known key with the tasks it already created.
export function linkKey(){return [...crypto.getRandomValues(new Uint8Array(16))].map(b=>b.toString(16).padStart(2,'0')).join('')}

export const maxBatchLinks=32
// Keeps each request under the SDK's 1 MiB body limit; one dropped .torrent
// file (at most 512 KiB, base64) always fits.
const maxBatchBytes=900_000
// These end the dialog instead of failing single links.
const fatal=['session_required','auth_unavailable','app_privilege_denied']

function parts(items:LinkItem[]){
 const out:LinkItem[][]=[]
 let part:LinkItem[]=[],bytes=0
 for(const item of items){
  const size=JSON.stringify(item).length
  if(part.length&&(part.length>=maxBatchLinks||bytes+size>maxBatchBytes)){out.push(part);part=[];bytes=0}
  part.push(item);bytes+=size
 }
 if(part.length)out.push(part)
 return out
}

// The daemon owns resolution and metadata waits. Admission returns task IDs;
// the dialog never polls a source, and a closed dialog cannot cancel queued work.
export async function submitLinks(call:Call,path:string,items:LinkItem[]):Promise<LinkResult[]>{
 const results:LinkResult[]=[]
 for(const part of parts(items)){
  let replies:any[]
  try{replies=(await call('downloads.create_many',{path,items:part}))?.results??[]}
  catch(e:any){if(fatal.includes(e?.code))throw e;replies=part.map(i=>({key:i.key,status:'failed',code:e?.code||'engine_unavailable'}))}
  for(const item of part){
   const reply=replies.find(r=>r?.key===item.key)??{status:'failed',code:'engine_unavailable'}
   results.push({key:item.key,url:item.url,status:reply.status==='created'||reply.status==='pending'?'created':'failed',kind:reply.kind,ids:reply.ids,code:reply.code})
  }
 }
 return results
}
