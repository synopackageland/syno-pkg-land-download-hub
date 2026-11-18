// Links the backend can classify: web addresses, magnets and ED2K links.
// Anything else in pasted text is dropped; the backend decides the kind.
const linkPattern=/(?:https?:\/\/|magnet:\?)[^\s<>"'`，。、；！？「」『』（）【】《》]+|ed2k:\/\/\|[^\s<>"'`]+?\|\//gi
const trailing=/[.,;:!?'"\]}>]+$/

function trim(link:string){
 let out=link.replace(trailing,'')
 // A closing parenthesis belongs to the link only when it opened one.
 while(out.endsWith(')')&&(out.match(/\(/g)?.length??0)<(out.match(/\)/g)?.length??0))out=out.slice(0,-1).replace(trailing,'')
 return out
}

// Every link in the text in order, repeats included.
export function linkMatches(text:string):string[]{
 const out:string[]=[]
 for(const match of text.matchAll(linkPattern)){const link=match[0].toLowerCase().startsWith('ed2k:')?match[0]:trim(match[0]);if(link)out.push(link)}
 return out
}
export function extractLinks(text:string):string[]{
 return [...new Set(linkMatches(text))]
}

// Each non-empty line contributes its links; a line without one is reported.
export function parseLinks(text:string){
 const unknown:string[]=[]
 for(const line of text.split(/\r?\n/).map(l=>l.trim()).filter(Boolean))if(!extractLinks(line).length)unknown.push(line)
 return {links:extractLinks(text),unknown}
}

// Reads links only when the browser already allows clipboard reads, so it
// never shows a permission prompt; any refusal leaves the field empty.
export async function readClipboardLinks(nav:Pick<Navigator,'clipboard'|'permissions'>=navigator):Promise<string[]>{
 try{
  const status=await nav.permissions?.query({name:'clipboard-read' as PermissionName})
  if(status?.state!=='granted'||!nav.clipboard?.readText)return []
  return extractLinks(await nav.clipboard.readText())
 }catch{return []}
}
