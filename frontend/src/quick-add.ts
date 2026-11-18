import {extractLinks} from './links'
import {maxBatchLinks} from './add-download'

// The phone's one-line Add field: the links it holds, at most one batch.
export function quickLinks(text:string){return extractLinks(text).slice(0,maxBatchLinks)}

// The Paste button reads the clipboard only where the browser lets a page
// do so on a tap: a secure context (HTTPS) with the async clipboard. Over
// plain HTTP it focuses the field instead, for the keyboard's own Paste.
export function clipboardReadable(win:{isSecureContext?:boolean;navigator?:{clipboard?:{readText?:unknown}}}=window){
 return !!win.isSecureContext&&typeof win.navigator?.clipboard?.readText==='function'
}
// The clipboard's links, or undefined when the browser refused the read.
export async function pastedLinks(nav:{clipboard?:{readText?:()=>Promise<string>}}=navigator):Promise<string[]|undefined>{
 try{return extractLinks(await nav.clipboard!.readText!())}catch{return undefined}
}

// Tasks added on this device lead the list, the newest first, so a new
// download shows at the top at once; the rest keep their order.
export function pinRecent<T extends {id:string}>(rows:T[],recent:string[]){
 if(!recent.length)return rows
 const rank=new Map(recent.map((id,i)=>[id,i]))
 const pinned=rows.filter(r=>rank.has(r.id)).sort((a,b)=>rank.get(a.id)!-rank.get(b.id)!)
 return [...pinned,...rows.filter(r=>!rank.has(r.id))]
}
// Newest first, each id once, at most a screenful.
export function addRecent(recent:string[],ids:string[],limit=20){
 return [...new Set([...[...ids].reverse(),...recent])].slice(0,limit)
}
