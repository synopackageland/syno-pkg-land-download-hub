import {extractLinks} from './links'
import {downloadTabs,type DownloadTab} from './downloads-view'

// Links that open the App on a page or with a link to add, for a bookmark or
// a home-screen shortcut:
//   ?view=downloads[&filter=active|done|failed]  the task list
//   ?view=search | ?view=settings                another of the App's pages
//   ?add=<url>                                    a link the user confirms
// Every value is untrusted text: anything that does not match is ignored.
export const deepLinkPages=['downloads','search','settings'] as const
export type DeepLinkPage=typeof deepLinkPages[number]
export type DeepLink={page?:DeepLinkPage;filter?:DownloadTab;add?:string}
export const deepLinkKeys=['view','filter','add'] as const
// A longer value is no link anyone pastes; it is dropped, not cut.
export const maxDeepLinkURL=4096

type Source=URLSearchParams|Record<string,string|undefined>|undefined
const read=(source:Source,key:string)=>source instanceof URLSearchParams?source.get(key)??undefined:source?.[key]

// The one link a value holds, exactly as given; a value with text around
// the link, several links or a scheme the engine does not take is no link.
export function deepLinkURL(value:string|undefined){
 const text=value?.trim()
 if(!text||text.length>maxDeepLinkURL||/[\u0000-\u001f\u007f]/.test(text))return undefined
 const links=extractLinks(text)
 return links.length===1&&links[0]===text?text:undefined
}

// Reads the first source that names a page or a link: the DSM launch's
// parameters, then the page address.
export function parseDeepLink(...sources:Source[]):DeepLink{
 for(const source of sources){
  const view=read(source,'view'),filter=read(source,'filter'),add=deepLinkURL(read(source,'add'))
  const page=deepLinkPages.find(p=>p===view)
  const tab=downloadTabs.find(t=>t===filter)
  if(!page&&!add&&!tab)continue
  const out:DeepLink={}
  // A link to add belongs to the task list, as does a filter without a page.
  out.page=add?'downloads':page??'downloads'
  if(tab&&out.page==='downloads')out.filter=tab
  if(add)out.add=add
  return out
 }
 return {}
}

// The address without the deep link's parameters, so a reload neither adds
// the link again nor keeps returning to the same page.
export function withoutDeepLink(href:string){
 const url=new URL(href)
 if(!deepLinkKeys.some(k=>url.searchParams.has(k)))return undefined
 for(const k of deepLinkKeys)url.searchParams.delete(k)
 return url.toString()
}
