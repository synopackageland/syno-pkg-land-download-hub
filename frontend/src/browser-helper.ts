import {inject,reactive,type InjectionKey} from 'vue'
import type {Context} from '@synopackageland/sdk'
import type {SourcePage} from './search'

export type BrowserRequest={id:string;token:string;url:string;operation:'search'|'download';expires:string;hosts:string[];selector?:string;maxBytes?:number;allowSameOriginRedirect?:boolean}
export type BrowserTrace={phases:Array<{phase:string;ms:number}>;code?:string;trigger?:string;redirect?:{from:string;to:string}}
export type BrowserHelper=ReturnType<typeof createBrowserHelper>
export const BrowserHelperKey:InjectionKey<BrowserHelper>=Symbol('Download Hub RTO')
export const useBrowserHelper=()=>inject(BrowserHelperKey)!
export type RtoSender=(message:Record<string,unknown>,timeout?:number,signal?:AbortSignal)=>Promise<any>
// Public RTO page bridge; no extension internals, eval, cookies or remote JS.
// https://remote-tab-opener.com/documentation/api-reference.php
export const sendRto:RtoSender=(message,timeout=8000,signal)=>new Promise((resolve,reject)=>{
 const requestId='download-hub-'+Array.from(crypto.getRandomValues(new Uint32Array(4))).join('-')
 const finish=(error?:unknown,value?:unknown)=>{clearTimeout(timer);window.removeEventListener('message',receive);signal?.removeEventListener('abort',abort);error?reject(error):resolve(value)}
 const abort=()=>finish(new DOMException('Browser assistance canceled','AbortError'))
 const receive=(event:MessageEvent)=>{
  const d=event.data
  if(event.source!==window||event.origin!==location.origin||!d||d.origin!=='rto-extension'||d.requestId!==requestId)return
  if(d.ok===true)finish(undefined,d.data??{});else finish({code:'search_rto_'+String(d.error?.code??'ACTION_FAILED').toLowerCase()})
 }
 const timer=setTimeout(()=>finish({code:'search_rto_timeout'}),timeout)
 window.addEventListener('message',receive);signal?.addEventListener('abort',abort,{once:true})
 if(signal?.aborted){abort();return}
 try{window.postMessage({...message,origin:'rto-page',requestId},location.origin)}catch{finish({code:'search_rto_disconnected'})}
})
export function sanitizeBrowserHTML(raw:string){
 // Bound transport input separately; measure only inert HTML for delivery.
 if(new TextEncoder().encode(raw).length>1024*1024)throw {code:'search_browser_size'}
 const doc=new DOMParser().parseFromString(raw,'text/html')
 doc.querySelectorAll('script,style,link,meta,base,form,input,textarea,select,button,iframe,object,embed,noscript').forEach(n=>n.remove())
 for(const n of Array.from(doc.body.querySelectorAll('*')))for(const a of Array.from(n.attributes)){
  // Imported definitions can select rows or read fields through data attributes.
  // Keep these inert parsing inputs, while excluding obvious credential fields.
  const data=a.value.length<=8192&&a.name.startsWith('data-')&&!/(?:^|[-_])(?:payload|json|state|props|cookie|session(?:id)?|token|secret|password|auth(?:orization)?)(?:$|[-_])/i.test(a.name)
  // src and bgcolor are inert inputs to existing category/row selectors.
  // This detached HTML is parsed offline and is never inserted into the App.
  if(!data&&!['class','id','href','src','bgcolor','title','datetime','colspan','rowspan','alt','rel','aria-label'].includes(a.name))n.removeAttribute(a.name)
 }
 const html=doc.body.innerHTML
 if(new TextEncoder().encode(html).length>256*1024)throw {code:'search_browser_size'}
 return html
}
export function createBrowserHelper(ctx:Context,send:RtoSender=sendRto){
 // unclosed: App-created source tabs RTO would not close, for a manual-close hint.
 const state=reactive({status:'not-installed' as 'not-installed'|'disconnected'|'ready',version:'',activity:'',error:'',timings:{} as Record<string,BrowserTrace>,unclosed:0})
 let controller=new AbortController(),timer:ReturnType<typeof setInterval>|undefined,checking=false,disposed=false
 const client=Array.from(crypto.getRandomValues(new Uint32Array(4))).join('-')
 const keys=new Map<string,number|undefined>(),offered=new Map<string,{incoming:BrowserRequest|(()=>Promise<BrowserRequest>);debugKey:string}>()
 const queue:Array<()=>void>=[]
 let active=0,serial=0,traceSerial=0
 function pause(ms:number,signal:AbortSignal){
  return new Promise<void>((resolve,reject)=>{const finish=()=>{clearTimeout(t);signal.removeEventListener('abort',abort);resolve()};const abort=()=>{clearTimeout(t);signal.removeEventListener('abort',abort);reject(signal.reason??new DOMException('Canceled','AbortError'))};const t=setTimeout(finish,ms);signal.addEventListener('abort',abort,{once:true})})
 }
 function pump(){while(active<4&&queue.length)queue.shift()!()}
 function abortable<T>(promise:Promise<T>,signal:AbortSignal):Promise<T>{
  return new Promise((resolve,reject)=>{
   const abort=()=>{signal.removeEventListener('abort',abort);reject(signal.reason??new DOMException('Canceled','AbortError'))}
   if(signal.aborted){void promise.catch(()=>{});abort();return}
   signal.addEventListener('abort',abort,{once:true})
   promise.then(value=>{signal.removeEventListener('abort',abort);resolve(value)},e=>{signal.removeEventListener('abort',abort);reject(e)})
  })
 }
 // closeTab finds a tab only in RTO's in-memory registry, which an MV3 worker
 // restart empties; listTabs restores it from storage, so one retry follows.
 // A tab that still stays open is counted for the manual-close hint.
 async function close(tabKey:string){
  if(!keys.has(tabKey))return
  let tabId=keys.get(tabKey);keys.delete(tabKey)
  try{await send({type:'closeTab',tabKey,tabId},5000);return}catch{}
  try{
   const tabs=await send({type:'listTabs'},5000)
   const tab=tabs.items?.find((t:any)=>t.tabKey===tabKey)
   if(typeof tab?.tabId==='number')tabId=tab.tabId
   await send({type:'closeTab',tabKey,tabId},5000)
  }catch{state.unclosed++}
 }
 async function check(){
  if(checking||disposed||active>0)return;checking=true
  try{
   const found=await send({type:'detect'},1800,controller.signal)
   state.version=String(found.version??'');if(state.status!=='ready')state.status='disconnected'
   await send({type:'listTabs'},4000,controller.signal)
   if(!disposed)state.status='ready'
  }catch{if(!disposed&&active===0)state.status=state.version?'disconnected':'not-installed'}
  finally{checking=false}
 }
 function start(_root:HTMLElement){void check();timer=setInterval(()=>void check(),15000)}
 async function currentURL(tabKey:string,request:BrowserRequest,signal:AbortSignal,trace:BrowserTrace){
  const tabs=await abortable(send({type:'listTabs'},5000,signal),signal)
  const tab=tabs.items?.find((t:any)=>t.tabKey===tabKey)
  if(!tab)throw {code:'search_rto_tab_closed'}
  if(/^chrome-error:|^about:(?:neterror|certerror)/i.test(tab.url??''))throw {code:'search_browser_network'}
  if(!tab.url||tab.url==='about:blank')throw {code:'search_rto_tab_navigating'}
  if(tab.url!==request.url){
   const original=new URL(request.url),actual=new URL(tab.url)
   if(!request.allowSameOriginRedirect||actual.origin!==original.origin||actual.username||actual.password)throw {code:'search_rto_tab_changed'}
   trace.redirect={from:original.origin+original.pathname,to:actual.origin+actual.pathname}
  }
 }
 // A busy NAS hands the token back (search_browser_busy): the same page is
 // delivered again, backing off, until it is kept, the ticket ends or the
 // owner cancels. The page read from the site is never discarded for slowness.
 async function deliver(params:Record<string,string>,request:BrowserRequest,signal:AbortSignal){
  for(let wait=2000;;wait=Math.min(wait*2,15000)){
   try{return await abortable(ctx.call<SourcePage>('search.browser.deliver',params),signal)}
   catch(e:any){
    if((e?.code??e?.errorCode)!=='search_browser_busy'||Date.now()+wait>=Date.parse(request.expires))throw e
    state.activity='delivering'
   }
   await pause(wait,signal)
  }
 }
 async function run(request:BrowserRequest,signal:AbortSignal,foreground:boolean,opened:()=>void,mark:(phase:string)=>void,trace:BrowserTrace):Promise<SourcePage>{
  if(!Number.isFinite(Date.parse(request.expires))||Date.parse(request.expires)<=Date.now())throw {code:'search_browser_expired'}
  const url=new URL(request.url)
  if(!['http:','https:'].includes(url.protocol)||url.username||url.password||!request.hosts.includes(url.origin))throw {code:'search_browser_invalid'}
  const tabKey='download-hub-'+client+'-'+(++serial)
  state.error='';state.activity='permission'
  try{
  await abortable(send({type:'registerMasterTab',masterKey:'download-hub-'+client},5000,signal),signal)
  // Ask for a fresh background tab; ownership is confirmed by the reply.
  // RTO 8.6.0 adopts a browser tab showing the same URL whenever a tabKey is
  // given, ignoring newTab/reuse, unless forceDuplicateForSameTabKey is set.
  // Keep the open reply listener alive so a late permission reply is cleaned up.
  mark('open')
  // The open waits for the user's RTO domain answer for as long as the ticket lives.
  const opening=send({type:'openTab',tabKey,url:request.url,focus:false,newTab:true,reuse:false,forceDuplicateForSameTabKey:true},Math.max(5000,Date.parse(request.expires)-Date.now()))
  void opening.then(out=>{
   // A tab RTO still reports as reused belongs to the user: never close it.
   if(out?.reused!==true)keys.set(tabKey,typeof out?.tabId==='number'?out.tabId:undefined)
   if(signal.aborted)return close(tabKey).catch(()=>{})
  },()=>{})
  await abortable(opening,signal)
  opened();mark('opened')
  if(foreground)await abortable(send({type:'focusTab',tabKey},5000,signal),signal)
  state.activity='verification'
  // A slow site keeps loading while the ticket lives; cancel ends it sooner.
  const until=Date.parse(request.expires)
  let verificationSince=0
  let previousHTML:string|undefined
  let lastError:unknown={code:'search_browser_timeout'}
  while(Date.now()<until){
   if(signal.aborted)throw new DOMException('Browser assistance canceled','AbortError')
   try{
    await currentURL(tabKey,request,signal,trace);mark('loaded')
    if(request.operation==='download'){
     const out=await abortable(send({type:'dom.extract',tabKey,args:{fields:[{name:'magnets',selector:'a[href^="magnet:?"]',mode:'attr',attr:'href',all:true,limit:2}],maxNodes:2,maxBytes:16384}},8000,signal),signal)
     const magnets=out.fields?.magnets
     if(Array.isArray(magnets)&&magnets[0]){
      await currentURL(tabKey,request,signal,trace)
      mark('stable');mark('extracted')
      const page=await deliver({id:request.id,token:request.token,url:request.url,magnet:magnets[0]},request,signal)
      mark('delivered');state.activity='';return page
     }
     // RTO has no public binary-fetch/download-body API. Do not invent a cookie
     // proxy: users can save the torrent themselves and use Add torrent.
     const title=await abortable(send({type:'dom.extract',tabKey,args:{fields:[{name:'title',selector:'title',mode:'text'}]}},8000,signal),signal)
     if(!/just a moment|verification|security check/i.test(title.fields?.title??''))throw {code:'search_browser_torrent_manual'}
     verificationSince ||= Date.now();lastError={code:'search_browser_verification'}
     if(!foreground&&Date.now()-verificationSince>=10000)throw lastError
    }else{
     // dom.extract avoids legacy getHtml's 5,000-character truncation. One
     // bounded batch preserves the DOM context for the exact source parser.
     const maxBytes=request.maxBytes===524288?524288:262144
     let out=await abortable(send({type:'dom.extract',tabKey,args:{fields:[{name:'page',selector:request.selector||'body',mode:'html',all:true,limit:16},{name:'title',selector:'title',mode:'text'}],maxNodes:17,maxBytes}},8000,signal),signal)
     if(request.selector&&request.selector!=='body'&&(!out.fields?.page||(Array.isArray(out.fields.page)&&out.fields.page.length===0)))out=await abortable(send({type:'dom.extract',tabKey,args:{fields:[{name:'page',selector:'body',mode:'html'},{name:'title',selector:'title',mode:'text'}],maxNodes:2,maxBytes:262144}},8000,signal),signal)
     // A missing result container is a valid empty page only after two stable
     // title observations; body fallback keeps site outage notices available.
     const raw=Array.isArray(out.fields?.page)&&out.fields.page.every((x:unknown)=>typeof x==='string')?out.fields.page.join(''):out.fields?.page
     if(typeof raw!=='string')throw {code:'search_browser_resource'}
     if(/just a moment|verification|security check/i.test(out.fields.title??'')){
      verificationSince ||= Date.now();lastError={code:'search_browser_verification'}
      if(!foreground&&Date.now()-verificationSince>=10000)throw lastError
     } else {
     const html=sanitizeBrowserHTML(raw)
     // A content listener can answer while navigation has only painted its
     // header. Two matching observations avoid spending the ticket on that
     // partial DOM; navigation errors alone do not prove the page is ready.
     if(html.trim()&&html===previousHTML){
      await currentURL(tabKey,request,signal,trace)
      mark('stable');mark('extracted')
      const page=await deliver({id:request.id,token:request.token,url:request.url,html},request,signal)
      mark('delivered');state.activity='';return page
     }
     previousHTML=html;lastError={code:'search_browser_resource'}
     }
    }
   }catch(e:any){
    if(e?.name==='AbortError'||!['search_browser_resource','search_rto_tab_navigating','search_rto_no_content_listener','search_rto_background_no_response','search_rto_uninjectable_url','search_rto_element_not_found','search_rto_timeout'].includes(e?.code))throw e
    lastError=e
   }
   await pause(1500,signal)
  }
  throw lastError
  }finally{mark('close');await close(tabKey);mark('closed')}
 }
 function request(incoming:BrowserRequest|(()=>Promise<BrowserRequest>),foreground=false,debugKey='request-'+(++traceSerial)){
  const parent=controller.signal
  const queued=Date.now(),trace:BrowserTrace={phases:[]};state.timings[debugKey]=trace
  const mark=(phase:string)=>{if(!trace.phases.some(x=>x.phase===phase))trace.phases.push({phase,ms:Date.now()-queued})};mark('queued')
  return new Promise<SourcePage>((resolve,reject)=>{
   const start=()=>{
    if(parent.aborted){reject(new DOMException('Canceled','AbortError'));return}
    active++
    const scope=new AbortController(),abort=()=>scope.abort(new DOMException('Canceled','AbortError'))
    parent.addEventListener('abort',abort,{once:true})
    // No fixed deadline: a source waits while its ticket lives, shown as in
    // progress, and the owner's Cancel ends it.
    const opened=()=>{}
    void (async()=>{
     const ticket=await abortable(typeof incoming==='function'?incoming():Promise.resolve(incoming),scope.signal)
     offered.set(ticket.url,{incoming,debugKey})
     return run(ticket,scope.signal,foreground,opened,mark,trace)
    })().then(resolve,e=>{state.error=e?.code??'';trace.code=e?.code??e?.name??'error';reject(e)}).finally(()=>{
     parent.removeEventListener('abort',abort);active--;state.activity=active?'verification':'';pump()
    })
   }
   queue.push(start);pump()
  })
 }
 function cancel(){controller.abort();controller=new AbortController();state.activity='';state.unclosed=0;pump()}
 async function focus(url:string){
  const offer=offered.get(url)
  if(!offer)throw {code:'search_rto_tab_closed'}
  // Explicit interaction starts a new ticket and a fresh owned tab. It is
  // closed on completion just like an automatic background search.
  return request(offer.incoming,true,offer.debugKey)
 }
 function dispose(){disposed=true;cancel();if(timer)clearInterval(timer)}
 return {state,start,check,request,focus,cancel,dispose}
}
