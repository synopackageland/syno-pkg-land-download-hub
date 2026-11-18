import {defaultMediaOptions} from '../src/media-options'
const clone=<T>(value:T):T=>JSON.parse(JSON.stringify(value))
import {installDsmFixture} from './dsm-fixture'
// The backend's kinds for the links the fixtures use.
function fixtureKind(url:string){
  if(/youtube\.com/.test(url))return 'media'
  if(/^(magnet:|data:application\/x-bittorrent)|\.torrent$/.test(url))return 'torrent'
  if(url.startsWith('ed2k:'))return 'ed2k'
  if(/^https?:\/\/127\./.test(url))return 'blocked'
  if(/^https?:\/\//.test(url)&&!url.includes('unsupported'))return 'file'
  return 'unsupported'
}
// Search sources, a small catalog and per-source answers; a source named in
// `errors` fails with that code, one in `slow` answers only when released.
function searchFixture(){
  const sources:any[]=[
    {id:'linux',kind:'builtin',name:'LinuxTracker',enabled:true,definitionId:'linuxtracker',configured:false,supported:true,reasons:[],settings:[]},
    {id:'private',kind:'builtin',name:'Private tracker',enabled:true,definitionId:'privatehd',configured:true,supported:true,reasons:[],settings:[{name:'username',label:'Username',type:'text'},{name:'password',label:'Password',type:'password'}]},
    {id:'feed',kind:'rss',name:'Release feed',enabled:true,configured:false,supported:true,reasons:[],settings:[]},
    {id:'off',kind:'ytsearch',name:'YouTube',enabled:false,configured:false,supported:true,reasons:[],settings:[]},
  ]
  const catalog={commit:'43359e0d7330606ec5aea76d5b9c5d274a73e7a8',generation:'a'.repeat(64),schema:11,total:4,supported:3,rollbackAvailable:true,updatedAt:'2026-10-16T08:30:00Z',sites:[
    {id:'linuxtracker',name:'LinuxTracker',type:'public',supported:true,reasons:[],settings:[]},
    {id:'privatehd',name:'PrivateHD',type:'private',supported:true,reasons:[],settings:[{name:'username',label:'Username',type:'text'},{name:'password',label:'Password',type:'password'},{name:'cookie',label:'Cookie',type:'text'}]},
    {id:'semi',name:'Semi Open',type:'semi-private',supported:true,reasons:[],settings:[{name:'cookie',label:'Cookie',type:'text'}]},
    {id:'andmatch',name:'Old Tracker',type:'public',supported:false,reasons:['filter:andmatch'],settings:[]},
  ]}
  const errors:Record<string,string>={private:'search_login'},slow:Record<string,()=>void>={},hold=new Set<string>()
  const results=(sourceId:string,query:string)=>Array.from({length:3},(_,i)=>({resultId:`${sourceId}-${i}`,sourceId,title:`${query||'Ubuntu'} ${sourceId} ${24-i}.04 desktop amd64 — a very long release name that needs its tooltip`,...(i<2?{category:i?'series':'movie'}:{}),...(i===2?{}:{size:(i+1)*1610612736,seeders:40-i*7,date:'2026-10-1'+(5+i)+'T08:00:00Z'})}))
  async function batch(params:any){
    const pages=params.sourceIds.map((sourceId:string)=>errors[sourceId]?{sourceId,page:1,results:[],error:{code:errors[sourceId]}}:{sourceId,page:1,morePossible:false,results:results(sourceId,params.query)})
    await Promise.all(params.sourceIds.filter((id:string)=>hold.has(id)).map((id:string)=>new Promise<void>(r=>{slow[id]=r})))
    return {pages}
  }
  return {sources,catalog,errors,hold,slow,batch}
}
// Local UI fixture only: no DSM transport, credentials, downloads or filesystem writes.
export function mockHost(admin = true,user = admin?'fixture-admin':'normal1',entryAllowed=true) {
  const calls: {method:string; params:any}[] = []
  const tasks:any[] = [
    {id:'active',status:'running',meta:{opts:{name:'Example download.zip',path:'/home/Downloads'},res:{size:4096}},progress:{downloaded:1024,speed:512}},
    {id:'paused',status:'pause',meta:{opts:{name:'Paused download.zip',path:'/home/Downloads'}}},
    {id:'done',status:'done',publication:{published:true,filename:'Saved document.pdf',folder:'/home/Downloads'}},
    {id:'pending',status:'done',meta:{opts:{name:'Ready to save.txt',path:'/home/Downloads'}}},
  ]
  if(!admin)for(const task of tasks){if(task.meta?.opts?.name)task.meta.opts.name=user+' · '+task.meta.opts.name;if(task.publication?.filename)task.publication.filename=user+' · '+task.publication.filename}
  const plugins = [{identity:'sample',title:'Example extension',version:'1.0.0',disabled:true,settings:[{name:'label',title:'Label',type:'string',value:'example'},{name:'mode',title:'Mode',type:'string',value:'safe',options:[{label:'Safe',value:'safe'}]}]}]
  let subfolder='', subscriptions = 0, disabled=false, revoked=false, destination='/home/Downloads', confirmedScope='home',generation=1
  const selection=()=>({scope:confirmedScope,root:confirmedScope==='home'?'/home':'/'+confirmedScope.slice(7),generation})
  const search=searchFixture()
  const jobs=new Map<string,any>()
  const jobView=(j:any)=>({id:j.id,key:j.key,operation:j.operation,sourceId:j.sourceId,state:j.state,progress:{done:j.pages.length,total:j.total},cancelRequested:j.state==='canceled'})
  let solverURL="",githubToken=''
 const media:any={defaults:defaultMediaOptions(),rules:[],follows:[]}
 let cookies:any={domains:[],importedAt:''}
  const ctx:any = {
    locale:'en',displayName:'Download Hub',user:{name:user,isAdmin:admin},
    executor:{async status(){return {state:'ready'}},async authorize(_password:string,scope?:string){if(scope&&scope!==confirmedScope){confirmedScope=scope;generation++;revoked=true}return {state:'ready'}},async control(){return {state:'ready'}}},
    reopen:() => {subscriptions++; return () => {subscriptions--}},
    host:{platform:()=>({host:'dsm',dsmStandalone:false}),activateWindow:()=>{calls.push({method:'fixture.host.activateWindow',params:{}});return {status:'requested'}},openFileManager:()=>({status:'requested'}),revealFile:()=>({status:'requested'}),openURL:(url:string)=>{calls.push({method:'fixture.host.openURL',params:{url}});return {status:'requested'}},openApp:(id:string)=>{calls.push({method:'fixture.host.openApp',params:{id}});return {status:'requested'}}},
    call:async(method:string,params:any={})=>{
      calls.push({method,params})
      if(!admin&&!entryAllowed)throw {code:"app_privilege_denied"}
      if(method==='gopeed.session') return {user,admin}
      if(method==='media.settings.get')return clone(media)
      if(method==='media.settings.save'){media.defaults=clone(params);return clone(params)}
      if(method==='media.cookies.status')return clone(cookies)
      if(method==='media.cookies.import'){cookies={domains:[{domain:'video.example',count:2}],importedAt:'2026-11-01T00:00:00Z'};return clone(cookies)}
      if(method==='media.cookies.delete'){cookies.domains=params.domain?cookies.domains.filter((d:any)=>d.domain!==params.domain):[];return clone(cookies)}
      if(method==='media.rules.save'){const rule={...params,id:'rule-'+media.rules.length};media.rules.push(rule);return clone(rule)}
      if(method==='media.rules.delete'){media.rules=media.rules.filter((r:any)=>r.id!==params.id);return {deleted:true}}
      if(method==='media.follows.add'){const f={...params,id:'follow-'+media.follows.length,nextCheck:'2026-11-02T00:00:00Z'};media.follows.push(f);return clone(f)}
      if(method==='media.follows.delete'){media.follows=media.follows.filter((f:any)=>f.id!==params.id);return {deleted:true}}

      if(!admin&&/^(gopeed\.(settings|plugins|github)|search\.definitions\.|binaries\.|delegate\.(enable|disable)|executor\.control)/.test(method)) throw {code:'admin_required'}
      if(method==='delegate.status')return {state:disabled?'disabled':'ready',disabled,lifetime:'until-revoked',destinations:['save'],scope_mode:'install-selected',preselection:'home',selection:selection()}
      if(method==='downloads.location'||method==='delegate.destination')return {subfolder,key:'save',selection:selection(),destination:disabled||revoked?null:{id:'fixture-grant',key:'save',path:destination,username:user,lifetime:'until-revoked'},status:disabled?'disabled':revoked?'reselection-required':'active'}
      if(method==='fixture.dsm.shares')return {shares:[{name:'home',path:'/home',isdir:true},{name:'downloads',path:'/downloads',isdir:true}],offset:0,total:2}
      if(method==='delegate.list')return {delegations:revoked?[]:[{delegation_id:'fixture-grant',destination:'save',path:destination,subject:{username:user},lifetime:'until-revoked',revoked:false}]}
      if(method==='delegate.operations.list')return {operations:[]}
      if(method==='delegate.revoke'){revoked=true;return {revoked:true}}
      if(method==='delegate.disable'){disabled=true;revoked=true;return {disabled:true}}
      if(method==='delegate.enable'){disabled=false;return {disabled:false}}
      if(method==='fixture.dsm.info')return {name:params.path.split('/').pop(),path:params.path,isdir:!params.path.endsWith('.pdf'),additional:{}}
      if(method==='fixture.dsm.list')return {files:[{name:'Downloads',path:params.path+'/Downloads',isdir:!params.path.endsWith('.pdf'),additional:{}}],offset:0,total:1}
      if(method==='downloads.folder.set'){subfolder=params.subfolder;return ctx.call('downloads.location',{})}
      if(method==='gopeed.destination'){subfolder='';revoked=false;destination=params.path;return {id:'fixture-grant',key:'save',path:params.path,username:user,lifetime:'until-revoked'}}
      if(method==='gopeed.request') {
        if(params.path==='/api/v1/removals')return {code:0,data:[]}
        if(params.path==='/api/v1/config')return {code:0,data:{downloadDir:destination,backgroundSave:!revoked&&!disabled}}
        if(params.path.endsWith('/stats'))return {code:0,data:{totalPeers:12,activePeers:3,connectedSeeders:2,seedRatio:0.5,seedTime:120}}
        if(params.method==='DELETE') {tasks.splice(tasks.findIndex(t=>params.path.endsWith(t.id)),1);return {code:0}}
        if(params.path.endsWith('/pause')){const t=tasks.find(t=>params.path===`/api/v1/tasks/${t.id}/pause`);if(t)t.status='pause'}
        if(params.path.endsWith('/continue')){const t=tasks.find(t=>params.path===`/api/v1/tasks/${t.id}/continue`);if(t)t.status='running'}
        return {code:0,data:params.method==='GET'?clone(tasks):{}}
      }
      if(method==='downloads.select_files'){const t=tasks.find(t=>t.id===params.id);if(t)t.meta.opts.selectFiles=params.files;return {files:params.files.length}}
      if(method==='downloads.classify')return {results:params.urls.map((url:string)=>({url,kind:fixtureKind(url)}))}
      if(method==='downloads.create_many'&&params.items.some((i:any)=>i.url.includes('choose'))){
        params.items.forEach((item:any,i:number)=>tasks.push({id:'bt-'+i,protocol:'bt',status:'running',meta:{opts:{path:'/home/Downloads'},res:{name:'Album',files:[{name:'a.flac',size:3},{name:'b.flac',size:4}]}},progress:{downloaded:0},publication:{batch:item.key}}))
        return {results:params.items.map((item:any,i:number)=>({key:item.key,status:'created',kind:'torrent',ids:['bt-'+i]}))}
      }
      if(method==='downloads.create_many')return {results:params.items.map((item:any,i:number)=>item.url.includes('fail')?{key:item.key,status:'failed',code:'url_denied'}:{key:item.key,status:'created',kind:fixtureKind(item.url),ids:['batch-'+i]})}
      if(method==='gopeed.settings.get')return {maxRunning:2,connections:4,seedRatio:0,seedMinutes:0,btPort:16889,ed2kPort:4662,ed2kUdpPort:4672,trackers:['udp://tracker.example.org/announce']}
      if(method==='gopeed.settings.set')return params
      if(method==='gopeed.plugins.catalog')return [{id:'gopeed@github-release',title:'Official registry sample',description:'A reviewed sample from the official store',version:'1.0.0',url:'https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample'}]
      if(method==='gopeed.plugins.check')return {newVersion:'1.1.0'}
      if(method==='gopeed.plugins.update'){const x=plugins.find(x=>x.identity===params.id);if(x)x.version=params.version;return {saved:true}}
      if(method==='gopeed.plugins.list')return clone(plugins)
      if(method==='gopeed.publish'){const task=tasks.find(t=>t.id===params.id);if(task)task.publication={...task.publication,published:true};return {published:true}}
      if(method==='gopeed.pending')return [{id:'pending'},{id:'uncertain',uncertain:true}]
      if(method==='gopeed.reveal')return {folder:'/home/Downloads',filename:'document.pdf'}
      if(method==='gopeed.github.status')return githubToken?{saved:true,last4:githubToken.slice(-4)}:{saved:false}
      if(method==='gopeed.github.set'){if(!/^[A-Za-z0-9_]{20,255}$/.test(params.token))throw {code:'github_token_format'};githubToken=params.token;return {saved:true,last4:githubToken.slice(-4),limit:5000,remaining:5000,reset:'2100-01-01T00:00:00Z'}}
      if(method==='gopeed.github.remove'){githubToken='';return {saved:false}}
      if(method==='binaries.status')return {ready:true,active:{version:'1.9.3'},previous:{version:'1.9.2'}}
      if(method==='binaries.check')return {version:'1.9.3',compatible:true,sha256:'f'.repeat(64)}
      if(method==='search.sources.site')return {url:'https://source.example/search/'+encodeURIComponent(params.query??'')}
      if(['search.jobs.start','search.sources.test.start'].includes(method)){
       const operation=method==='search.jobs.start'?'search':'source-test'
       const duplicate=[...jobs.values()].find(j=>j.key===params.key)
       if(duplicate)return {outcome:duplicate.state==='succeeded'?'accepted':'busy',job:jobView(duplicate),duplicate:true}
       const active=[...jobs.values()].find(j=>['queued','running'].includes(j.state)&&(operation==='search'?j.operation==='search':j.sourceId===params.id))
       if(active)return {outcome:'busy',job:jobView(active)}
       const job={id:'job-'+(jobs.size+1),key:params.key,operation,sourceId:params.id,state:'running',pages:[] as any[],total:params.sourceIds?.length??1,result:null as any}
       jobs.set(job.id,job)
       void (async()=>{
        if(operation==='search'){
         await Promise.all(params.sourceIds.map(async(id:string)=>{const out=await search.batch({...params,sourceIds:[id]});if(job.state!=='canceled')job.pages.push(...out.pages)}))
         job.result={kind:'search',pages:job.pages.length}
        }else if(operation==='source-test')job.result=search.errors[params.id]?{kind:'probe',status:'login',code:search.errors[params.id]}:{kind:'probe',status:'available'}
        if(job.state!=='canceled')job.state='succeeded'
       })()
       return {outcome:'accepted',job:jobView(job)}
      }
      if(method==='search.jobs.list')return {jobs:[...jobs.values()].map(jobView)}
      if(method.startsWith('search.jobs.')){
       const j=jobs.get(params.id);if(!j)throw {code:'search_job_missing'}
       if(method==='search.jobs.status')return jobView(j)
       if(method==='search.jobs.cancel'){j.state='canceled';return jobView(j)}
       if(method==='search.jobs.result')return j.result
       if(method==='search.jobs.pages'){const end=Math.min(j.pages.length,params.offset+params.limit);return {items:j.pages.slice(params.offset,end),offset:params.offset,total:j.pages.length}}
      }
      if(method==='search.solver.get')return {url:solverURL,configured:!!solverURL}
      if(method==='search.solver.set'){solverURL=params.url;return {configured:!!solverURL}}
      if(method==='search.sources.list')return clone(search.sources)
      if(method==='search.catalog')return clone(search.catalog)
      if(method==='search.download'){
        const id='prepare-'+params.resultId,existing=tasks.some(t=>t.id===id)
        if(!existing)tasks.push({id,status:'running',meta:{opts:{name:params.resultId,path:destination}},progress:{downloaded:0,speed:0}})
        return {results:[{key:existing?'existing':'search-'+params.resultId,kind:'torrent',status:'created',ids:[id]}]}
      }
      if(method==='search.sources.save'){
        if(params.kind==='cardigann'&&!/^id:/m.test(params.yaml??''))throw {code:'search_definition_invalid'}
        const old=search.sources.find(x=>x.id===params.id)
        const view={id:params.id??'new-'+search.sources.length,kind:params.kind,name:params.name,enabled:params.enabled,definitionId:params.definitionId,configured:!!Object.values(params.values??{}).some(Boolean)||!!old?.configured,supported:true,reasons:[],settings:[]}
        if(old)Object.assign(old,view);else search.sources.push(view)
        return view
      }
      if(method==='search.sources.enabled'){const x=search.sources.find(x=>x.id===params.id);if(x)x.enabled=params.enabled;return {enabled:params.enabled}}
      if(method==='search.sources.delete'){search.sources.splice(search.sources.findIndex(x=>x.id===params.id),1);return {deleted:true}}
      if(method==='search.sources.export')return {yaml:'id: fixture\nname: Fixture\n'}
      if(method==='search.definitions.update')return clone(search.catalog)
      if(method==='search.definitions.rollback')return {rolledBack:true}
      return {}
    },
  }
  installDsmFixture(ctx)
  return {ctx,calls,tasks,search,get subscriptions(){return subscriptions}}
}
