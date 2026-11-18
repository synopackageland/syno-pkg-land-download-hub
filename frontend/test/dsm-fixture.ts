// Same-origin DSM transport fixture. The App backend never handles these reads.
// Used by both the browser preview and jsdom, with a distinct token per mount.
const sessions=new Map<string,(method:string,params:any)=>Promise<any>>()
let sequence=0
const nativeFetch=globalThis.fetch?.bind(globalThis)
export function installDsmFixture(ctx:any){
 const token='fixture-dsm-'+(++sequence)
 ctx.files={token:()=>token}
 ;(globalThis as any).SYNO={SDS:{Session:{SynoToken:token}}}
 sessions.set(token,(method,params)=>ctx.call('fixture.dsm.'+method,params))
 globalThis.fetch=async(input:RequestInfo|URL,init?:RequestInit)=>{
  if(String(input)!=='/webapi/entry.cgi'){
   if(!nativeFetch)throw Error('Unexpected fixture request')
   return nativeFetch(input,init)
  }
  const body=init?.body as URLSearchParams
  // DSM takes the session token from the X-SYNO-TOKEN header; a body SynoToken is refused (119).
  if(body.has('SynoToken'))return Response.json({success:false,error:{code:119}})
  const dispatch=sessions.get(new Headers(init?.headers).get('X-SYNO-TOKEN')??'')
  if(!dispatch)return Response.json({success:false,error:{code:106}})
  const method=body.get('method')
  const params=method==='getinfo'?{path:JSON.parse(body.get('path')!)[0]}:method==='list'?{path:body.get('folder_path'),offset:Number(body.get('offset'))}:{offset:Number(body.get('offset'))}
  try{
   const data=await dispatch(method==='getinfo'?'info':method==='list_share'?'shares':String(method),params)
   return Response.json({success:true,data:method==='getinfo'?{files:[data]}:data})
  }catch{return Response.json({success:false,error:{code:105}})}
 }
}
