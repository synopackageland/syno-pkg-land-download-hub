// The New download dialog's view of its links: numbered patterns expanded,
// repeats removed, and each link grouped by the route the backend chose.
// The backend's downloads.classify stays the only classifier.
import {linkMatches,parseLinks} from './links'

// A pattern such as img[1-3].jpg or ep[01-12].mp4 names at most this many
// links; a batch holds at most totalCap, the most classify takes at once.
export const patternCap=100
export const totalCap=256
const range=/\[(\d{1,6})-(\d{1,6})\]/g

// The links one pattern names, in order; null for a link with no pattern.
// Several ranges in one link combine. An IPv6 host such as [::1] is no range.
export function expandPattern(url:string):{urls:string[];capped:boolean}|null{
 const ranges=[...url.matchAll(range)]
 if(!ranges.length)return null
 let out=['']
 let at=0,capped=false
 for(const m of ranges){
  const [whole,a,b]=m,start=Number(a),end=Number(b)
  const step=start<=end?1:-1,width=a.length>1&&a.startsWith('0')?a.length:0
  const values:string[]=[]
  for(let n=start;step>0?n<=end:n>=end;n+=step){values.push(width?String(n).padStart(width,'0'):String(n));if(values.length>patternCap)break}
  const prefix=url.slice(at,m.index)
  const next:string[]=[]
  for(const head of out){for(const v of values){next.push(head+prefix+v);if(next.length>patternCap)break}if(next.length>patternCap)break}
  out=next;at=(m.index??0)+whole.length
 }
 out=out.map(head=>head+url.slice(at))
 if(out.length>patternCap){out=out.slice(0,patternCap);capped=true}
 return {urls:out,capped}
}

export type CollectedLink={url:string;pattern:boolean}
export function collectLinks(text:string){
 const {unknown}=parseLinks(text)
 const seen=new Set<string>(),links:CollectedLink[]=[]
 let duplicates=0,expanded=0,cappedPattern=false,cappedTotal=false
 for(const raw of linkMatches(text)){
  const pattern=expandPattern(raw)
  if(pattern?.capped)cappedPattern=true
  for(const url of pattern?.urls??[raw]){
   if(seen.has(url)){duplicates++;continue}
   if(links.length>=totalCap){cappedTotal=true;continue}
   seen.add(url);links.push({url,pattern:!!pattern})
   if(pattern)expanded++
  }
 }
 return {links,unknown,duplicates,expanded,cappedPattern,cappedTotal}
}

// The dialog's groups, in order. A link whose kind is not known yet waits
// in "pending"; blocked and unsupported links cannot be added.
export const linkGroups=['file','media','torrent','ed2k','extension','pending','none'] as const
export type LinkGroup=typeof linkGroups[number]
// The backend's kind, and the route it chose for a web link: a stream or a
// page yt-dlp handles goes to yt-dlp, everything else to Gopeed.
export function groupOf(result:{kind?:string;route?:string}|undefined):LinkGroup{
 if(!result?.kind)return 'pending'
 if(result.kind==='blocked'||result.kind==='unsupported')return 'none'
 if(result.kind==='media'||result.route==='media'||result.route==='yt-dlp')return 'media'
 if(result.kind==='torrent'||result.kind==='ed2k'||result.kind==='extension')return result.kind
 return 'file'
}
