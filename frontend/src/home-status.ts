import {clientTimeout,createNasProvider,type Context} from '@synopackageland/sdk'

export type HomeStatus='enabled'|'disabled'|'unknown'
export type HomeSettings={enable?:boolean;enable_domain?:boolean;enable_ldap?:boolean}
export function homeDecision(input:{admin:boolean;kind?:'local'|'domain'|'ldap';settings?:HomeSettings;visible?:boolean}):HomeStatus{
 // File Station is the current caller's evidence, including directory users.
 if(input.visible!==undefined)return input.visible?'enabled':'disabled'
 if(input.admin&&input.settings&&input.kind){
  const value=input.settings[input.kind==='domain'?'enable_domain':input.kind==='ldap'?'enable_ldap':'enable']
  if(typeof value==='boolean')return value?'enabled':'disabled'
 }
 if(input.admin&&input.settings&&input.settings.enable===false&&input.settings.enable_domain===false&&input.settings.enable_ldap===false)return 'disabled'
 return 'unknown'
}
const cache=new WeakMap<Context,{at:number;value:Promise<HomeStatus>}>()
export function checkHome(ctx:Context,retry=false):Promise<HomeStatus>{
 const saved=cache.get(ctx);if(!retry&&saved&&Date.now()-saved.at<30_000)return saved.value
 const value=(async()=>{
  const provider=createNasProvider(ctx),controller=new AbortController()
  const timeout=setTimeout(()=>controller.abort(),clientTimeout(ctx.requestTimeout))
  let settings:HomeSettings|undefined,visible:boolean|undefined
  try{
   // This is the caller's own DSM channel. Refusal falls through to their
   // File Station share list; no privileged or package-account fallback.
   const control=ctx.user?.isAdmin&&ctx.webApi?ctx.webApi('SYNO.Core.User.Home','get',1,{}).then((r:any)=>{settings=r?.data??r}).catch(()=>{}):Promise.resolve()
   const shares=(async()=>{let offset=0;for(let page=0;page<100;page++){
    const result=await provider.shares(offset,controller.signal)
    if(result.entries.some(e=>e.path==='/home')){visible=true;return}
    if(result.nextOffset===undefined){visible=false;return}
    if(result.nextOffset<=offset)throw new Error('invalid share page');offset=result.nextOffset
   }})().catch(()=>{})
   await Promise.race([Promise.all([control,shares]),new Promise<void>(resolve=>controller.signal.addEventListener('abort',()=>resolve(),{once:true}))])
   return homeDecision({admin:!!ctx.user?.isAdmin,settings,visible})
  }finally{clearTimeout(timeout);provider.dispose()}
 })()
 cache.set(ctx,{at:Date.now(),value});return value
}

export function openUserHome(ctx:Context){
 const target=new URL('/',location.origin)
 target.searchParams.set('launchApp','SYNO.SDS.AdminCenter.Application')
 target.searchParams.set('launchParam','fn='+encodeURIComponent('SYNO.SDS.AdminCenter.User.Main')+'&tab='+encodeURIComponent('advanced'))
 return ctx.host.openURL(target.href)
}
