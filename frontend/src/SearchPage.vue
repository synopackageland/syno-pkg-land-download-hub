<script setup lang="ts">
import {inject,computed,onMounted,onUnmounted,reactive,ref,watch} from 'vue'
import {Badge,Button,Callout,Dialog,EmptyState,Link,LoadingArea,MultipleSelect,SearchField,Select,Space,Tag,formatDateTime,formatFileSize,formatRelativeTime,usePhone,BottomSheet,IconButton} from '@synopackageland/ui'
import {Check,CircleAlert,Clock,Download,Globe,LoaderCircle,LogIn,RefreshCw,Search as SearchIcon,SlidersHorizontal,X} from '@lucide/vue'
import {format,type Context,type DestinationState} from '@synopackageland/sdk'
import {appCopy} from './app-copy'
import {activePath,ensureDestinationKey,type EnsureDestination} from './destination'
import DestinationRow from './DestinationRow.vue'
import {type SearchJob} from './search-jobs'
import BrowserSearchGuide from './BrowserSearchGuide.vue'
import {useBrowserHelper,type BrowserRequest} from './browser-helper'
import {sourceProblem,defaultSourceIDs,beginSearch,mergeHeld,receivePage,searchSources,activeSearch,openSearchTab,closeSearchTab,searchUsed,shownRows,queryOptional,searchCategories,searchSorts,sortRows,seederTone,chipAction,emptyKind,enoughFound,skippedSources,browserOnly,phoneSourceIDs,desktopOnlyCount,searchSummary,searchTuned,phoneFilterFrom,type SearchRow,type SearchState,type SearchTabs,type SourceView} from './search'
// tabs: held by the App so each query and its results outlive the page.
const ensureDestination=inject<EnsureDestination|undefined>(ensureDestinationKey,undefined)
const props=defineProps<{ctx:Context;tabs:SearchTabs;reason:(e:any)=>string;destination?:DestinationState|null}>()
const emit=defineEmits<{showTask:[id:string,batch?:string];addSource:[];solverSettings:[];refused:[error:any];authorized:[]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
// The shown tab; work for a tab keeps writing to that tab while another shows.
const tab=computed(()=>activeSearch(props.tabs))
const controllers=new Map<string,AbortController>()
const loaded=ref(tab.value.sources.length>0),loadError=ref(''),shown=ref(''),choosing=ref<SearchRow|null>(null)
const sending=reactive<Record<string,boolean>>({}),failed=reactive<Record<string,string>>({})

const helper=useBrowserHelper()
const assisting=reactive<Record<string,boolean>>({})
async function assist(id:string){
 const s=tab.value
 if(assisting[id]||!s.jobId)return
 const source=s.sources.find(x=>x.id===id)
 if(source?.browserReason){s.statuses[id]={state:'cloudflare',count:0,message:copy('browser.unsupported.'+source.browserReason)};shown.value=id;return}
 if(helper.state.status!=='ready'){shown.value=id;return}
 assisting[id]=true
 const generation=s.generation
 try{
  const jobId=s.jobId,trigger=s.statuses[id]?.code
  s.statuses[id]={state:'browser',count:0}
  const pending=helper.request(()=>props.ctx.call<BrowserRequest>('search.browser.start',{id:jobId,sourceId:id}).then(ticket=>{s.siteURLs[id]=ticket.url;return ticket}),false,id)
  helper.state.timings[id].trigger=trigger
  const page=await pending
  receivePage(s,page,generation)
 }catch(e:any){if(generation===s.generation){s.statuses[id]={state:(e?.code??e?.errorCode)?.startsWith('search_')&&!/^search_(?:rto|browser)_/.test(e?.code??e?.errorCode)?sourceProblem(e?.code??e?.errorCode):'cloudflare',count:0,code:e?.code??e?.errorCode,message:['search_empty','search_no_data'].includes(e?.code)?undefined:props.reason(e)};shown.value=id}}
 finally{delete assisting[id]}
}
// A phone browser cannot run Remote Tab Opener: its searches leave out the
// sources that need it, and the page drops the browser guide and states.
const phone=usePhone()
const usable=computed(()=>tab.value.sources.filter(x=>x.enabled&&x.supported))
const options=computed(()=>usable.value.filter(x=>!phone.value||!browserOnly(x,tab.value.solverConfigured)).map(x=>({label:x.name,value:x.id})))
function searchIDs(s:SearchState){const ids=defaultSourceIDs(s,s.solverConfigured||helper.state.status==='ready');return phone.value?phoneSourceIDs(s,ids):ids}
const chosen=computed({get:()=>searchIDs(tab.value),set:ids=>{tab.value.selected=ids}})
const desktopOnly=computed(()=>phone.value?desktopOnlyCount(tab.value):0)
// The phone keeps one field and one status line; category, sources, sort
// and each source's state wait in the options sheet.
const optionsOpen=ref(false)
const summary=computed(()=>searchSummary(tab.value))
const tuned=computed(()=>searchTuned(tab.value))
const counted=(n:number,one:string,many:string,key='count')=>n===1?copy(one):format(copy(many),{[key]:n},props.ctx.locale)
const summaryText=computed(()=>{
 const x=summary.value
 if(!x.sources)return ''
 // Narrowed to one source: its name and the rows it shows.
 const only=tab.value.sourceFilter
 if(only)return (names.value[only]??only)+' · '+counted(rows.value.length,'phone.search.oneResult','search.results')
 return counted(x.sources,'phone.search.oneSource','search.sourceCount')+' · '+(x.running&&!x.results?copy('search.state.running'):counted(x.results,'phone.search.oneResult','search.results'))
})
const desktopOnlyText=computed(()=>desktopOnly.value===1?copy('phone.desktopOnly.one'):format(copy('phone.desktopOnly'),{n:desktopOnly.value},props.ctx.locale))
function resetOptions(){const s=tab.value;s.category='';s.selected=null;s.sort=null;s.sourceFilter=''}
function pickSource(id:string){const x=tab.value.statuses[id];if(x?.state==='done'){tab.value.sourceFilter=tab.value.sourceFilter===id?'':id;optionsOpen.value=false}}
const names=computed(()=>Object.fromEntries(tab.value.sources.map(x=>[x.id,x.name])))
const categories=computed(()=>[{label:copy('search.allCategories'),value:'all'},...searchCategories.map(value=>({label:copy('search.type.'+value),value}))])
const category=computed({get:()=>tab.value.category||'all',set:v=>{tab.value.category=v==='all'?'':v}})
// Category, the chosen source chip and the filter words narrow the rows; the
// sort menu orders them.
const assistOn=computed(()=>tab.value.solverConfigured||helper.state.status==='ready')
// What a chip offers; the browser is no option on a phone.
const chipOffer=(x:Parameters<typeof chipAction>[0])=>{const a=chipAction(x);return phone.value&&a==='browser'?'':a}
const rows=computed(()=>{
 const s=tab.value,words=s.filter.trim().toLowerCase()
 let list=shownRows(s.rows,s.category)
 if(s.sourceFilter)list=list.filter(r=>r.sourceIds.includes(s.sourceFilter))
 if(words)list=list.filter(r=>r.title.toLowerCase().includes(words)||r.source.toLowerCase().includes(words))
 return sortRows(list,s.sort)
})
const pageSize=100,limit=ref(pageSize)
watch(()=>[tab.value.id,tab.value.generation,tab.value.filter,tab.value.sourceFilter,tab.value.sort],()=>{limit.value=pageSize})
const sortOptions=computed(()=>[{label:copy('search.sort.relevance'),value:'none'},...searchSorts.map(value=>({label:copy('search.sort.'+value),value}))])
const sortValue=computed({get:()=>tab.value.sort?.key??'none',set:v=>{tab.value.sort=v==='none'?null:{key:v,direction:v==='title'?'asc':'desc'}}})
const empty=computed(()=>emptyKind(tab.value))
// The search service runs one search per user at a time.
const runningElsewhere=computed(()=>props.tabs.tabs.some(x=>x!==tab.value&&x.running))
const wordsOptional=computed(()=>queryOptional(tab.value.sources,chosen.value))
// A finished search that found nothing points to the sources, not a dead end.
const finishedEmpty=computed(()=>!tab.value.running&&!tab.value.rows.length&&!tab.value.held.length&&Object.keys(tab.value.statuses).length>0)
const canSearch=computed(()=>(!!tab.value.query.trim()||wordsOptional.value)&&chosen.value.length>0&&!runningElsewhere.value)
// The mask stays only until the first source answers.
const enough=computed(()=>enoughFound(tab.value))
const left=computed(()=>Object.values(tab.value.statuses).filter(x=>x.state==='running'||x.state==='browser').length)
const masked=computed(()=>tab.value.running&&Object.values(tab.value.statuses).every(x=>x.state==='running'))
const tabLabel=(x:SearchState)=>x.searched||(searchUsed(x)?copy('search.latest'):copy('search.newTab'))
const statusTone=(state:string)=>state==='browser'?'info':state==='running'?'info':state==='done'?'success':['empty','noData'].includes(state)?'neutral':state==='busy'?'warning':state==='stopped'?'neutral':'danger'
function statusLabel(id:string){
 const x=tab.value.statuses[id]
 if(x.state==='done')return format(copy('search.results'),{count:x.count},props.ctx.locale)
 if(x.state==='http')return 'HTTP '+x.code?.slice(-3)
 if(x.state==='browser')return copy('browser.waiting')
 if(x.state!=='cloudflare')return copy('search.state.'+x.state)
 if(phone.value)return copy('phone.computerOnly')
 if(x.code==='search_empty')return copy('search.state.empty')
 if(x.code==='search_browser_size'||x.code==='search_rto_extract_limit_exceeded')return copy('browser.tooLarge')
 if(x.code==='search_browser_verification')return copy('browser.challenge')
 if(/user_cancelled|user_canceled|allowlist|domain|permission|refus/.test(x.code??''))return copy('browser.permission')
 return copy(x.code?.startsWith('search_rto_')?'browser.failed':'browser.needs')
}
const chipIcon=(state:string)=>state==='done'?Check:state==='running'||state==='browser'?LoaderCircle:['empty','noData','stopped'].includes(state)?Clock:state==='login'?LogIn:state==='cloudflare'?Globe:CircleAlert
// A chip with results narrows the list to its source; one with a problem shows why.
function pickChip(id:string){
 const s=tab.value,x=s.statuses[id]
 if(x?.state==='done'){s.sourceFilter=s.sourceFilter===id?'':id;shown.value='';return}
 shown.value=shown.value===id?'':id
}
function chipAct(id:string){
 const action=chipAction(tab.value.statuses[id])
 if(action==='signin')emit('addSource')
 else if(action==='retry')void retrySource(id)
 else if(action==='browser'){
  // A source skipped for want of a browser is searched again once one is there.
  if(tab.value.statuses[id]?.code==='search_browser_skipped'&&helper.state.status==='ready')void retrySource(id)
  else void assist(id)
 }
}
const statusReason=(id:string)=>{const x=tab.value.statuses[id];if(phone.value&&x.state==='cloudflare')return copy('phone.computerOnlyHelp');return x.message||(x.state==='http'?'HTTP '+x.code?.slice(-3):copy('search.reason.'+x.state))}
async function openChallenge(){const s=tab.value,id=shown.value,generation=s.generation;assisting[id]=true;try{const page=await helper.focus(s.siteURLs[id]);receivePage(s,page,generation)}catch(e){emit('refused',e)}finally{delete assisting[id]}}

// Unknown values read "—".
const dash=(v:unknown)=>v===null||v===undefined?'—':String(v)
const when=(ms:number|null)=>ms===null?'':formatRelativeTime(ms,{locale:props.ctx.locale})
function meta(r:SearchRow){
 return [r.type?copy('search.type.'+r.type):'',r.size===null?'':formatFileSize(r.size),r.date===null?'':when(r.date)].filter(Boolean)
}
const sourceText=(r:SearchRow)=>r.sourceIds.length>1?format(copy('search.sourceCount'),{count:r.sourceIds.length},props.ctx.locale):r.source
const sourceTitle=(r:SearchRow)=>r.sourceIds.map(id=>names.value[id]).filter(Boolean).join(', ')

async function loadSources(){
 try{
  const [sources,solver]=await Promise.all([props.ctx.call<SourceView[]>('search.sources.list',{}),props.ctx.call<{configured:boolean}>('search.solver.get',{})])
  for(const x of props.tabs.tabs){x.sources=sources;x.solverConfigured=!!solver.configured}
  loadError.value=''
 }
 // A list that failed to load is not an empty one: say why and offer another try.
 catch(e){loadError.value=props.reason(e)||copy('search.loadFailed')}
 finally{loaded.value=true}
}
onMounted(async()=>{
 await loadSources()
 // A search still running from before the page opened belongs to the shown
 // tab, unless another tab already follows a job.
 const s=tab.value
 if(!s.jobId&&!props.tabs.tabs.some(x=>x.jobId)){
  try{const out=await props.ctx.call<{jobs:SearchJob[]}>('search.jobs.list',{});const active=[...out.jobs].reverse().find(j=>j.operation==='search'&&['queued','running','paused','succeeded'].includes(j.state));if(active){s.jobId=active.id;s.running=true;s.statuses={}}}
  catch(e){emit('refused',e)}
 }
 for(const x of props.tabs.tabs)if(x.running&&x.jobId)void follow(x,x.jobId)
 if(s.pending){s.pending=false;void start(s)}
})
onUnmounted(()=>{for(const c of controllers.values())c.abort();controllers.clear();helper.cancel()})
watch(()=>props.tabs.active,()=>{shown.value=''})

async function follow(s:SearchState,existingId?:string,only?:string[]){
 controllers.get(s.id)?.abort()
 const controller=new AbortController();controllers.set(s.id,controller)
 const generation=s.generation,ids=only??searchIDs(s)
 try{
  await searchSources((m,p)=>props.ctx.call(m,p),ids,s.query.trim(),page=>{
   if(generation!==s.generation)return
   if(!(page.sourceId in s.statuses))s.statuses[page.sourceId]={state:'running',count:0}
   receivePage(s,page,generation)
   if(page.status==='needs_browser'||page.error?.code==='search_cloudflare'){s.statuses[page.sourceId].state='cloudflare';if(helper.state.status==='ready'&&s===tab.value)void assist(page.sourceId)}
  },controller.signal,id=>{s.jobId=id},existingId,job=>{
   if(job.startedAt){
    if(s.jobStartedAt&&s.jobStartedAt!==job.startedAt){s.rows=[];s.held=[];for(const id of Object.keys(s.statuses))s.statuses[id]={state:'running',count:0}}
    s.jobStartedAt=job.startedAt
   }
  },s.category)
  if(generation===s.generation)s.running=false
 }catch(e:any){if(e?.name==='AbortError')return;if(generation===s.generation){s.running=false;for(const id of ids)if(s.statuses[id]?.state==='running')receivePage(s,{sourceId:id,results:[],error:{code:e?.errorCode??e?.code??'search_client_error',message:props.reason(e)}},generation)};if(e?.code!=='search_job_canceled')emit('refused',e)}
 finally{if(controllers.get(s.id)===controller)controllers.delete(s.id)}
}
async function cancelJob(s:SearchState){helper.cancel();if(s.jobId){await props.ctx.call('search.browser.cancel',{id:s.jobId}).catch(()=>{});if(s.running)await props.ctx.call('search.jobs.cancel',{id:s.jobId})}}
const cancelSearch=()=>cancelJob(tab.value)
// A tab that already searched keeps its results; the words open a new tab.
async function search(){
 if(!canSearch.value||tab.value.running)return
 if(searchUsed(tab.value)){
  // The reactive copy, so the page shows what the search writes.
  openSearchTab(props.tabs)
  const next=tab.value;next.pending=false
  await start(next)
 }else await start(tab.value)
}
async function start(s:SearchState){
 helper.cancel()
 if(s.jobId)await props.ctx.call('search.browser.cancel',{id:s.jobId}).catch(()=>{})
 const ids=[...chosen.value];beginSearch(s,ids);s.searched=s.query.trim();s.jobId='';s.jobStartedAt='';s.sourceFilter=''
 for(const id of phone.value?[]:skippedSources(s,ids,s.solverConfigured||helper.state.status==='ready'))s.statuses[id]={state:'cloudflare',count:0,code:'search_browser_skipped'}
 shown.value=''
 await follow(s)
}
// One source searched again into the shown results; the others keep theirs.
async function retrySource(id:string){
 const s=tab.value
 if(s.running||runningElsewhere.value)return
 helper.cancel()
 s.statuses[id]={state:'running',count:0};s.running=true;s.jobStartedAt='';shown.value=''
 await follow(s,undefined,[id])
}
// Closing a tab stops its search; its results go with it.
function closeTab(x:SearchState){
 controllers.get(x.id)?.abort();controllers.delete(x.id)
 if(x.running)void cancelJob(x).catch(e=>emit('refused',e))
 closeSearchTab(props.tabs,x.id)
}
watch(()=>helper.state.status,status=>{if(status==='ready')for(const [id,x]of Object.entries(tab.value.statuses))if(x.state==='cloudflare'&&['search_cloudflare','search_timeout','search_network','search_connection','search_dns','search_tls'].includes(x.code??''))void assist(id)})
// A cleared sort lets held answers in at once.
watch(()=>tab.value.sort,sort=>{if(!sort)mergeHeld(tab.value)})

async function download(row:SearchRow){
 if(activePath(props.destination)||await ensureDestination?.())void send(row)
 else choosing.value=row
}
// A repeated result answers with the task it already has.
async function send(row:SearchRow){
 const s=tab.value
 sending[row.id]=true;delete failed[row.id]
 try{
  let out:any
  try{out=await props.ctx.call('search.download',{resultId:row.id})}catch(e:any){
   if((e.code??e.errorCode)!=='search_browser_required')throw e
   const request=await props.ctx.call<BrowserRequest>('search.browser.download',{resultId:row.id})
   await helper.request(request)
   out=await props.ctx.call('search.download',{resultId:row.id})
  }
  const r=out?.results?.[0]
  if(r?.status==='failed')failed[row.id]=props.reason({code:r.code})
  else {s.added[row.id]=r?.ids?.[0]??'';s.addedBatches[row.id]=r?.key??''}
 }catch(e){emit('refused',e);failed[row.id]=props.reason(e)}
 finally{delete sending[row.id]}
}
function confirmChoice(){const row=choosing.value;choosing.value=null;if(row)void send(row)}
</script>
<template>
 <div class="flex min-h-0 flex-col gap-3" :class="{'is-phone':phone}" data-search-page data-gopeed-native>
  <BrowserSearchGuide v-if="!phone" :locale="ctx.locale"><template #actions><slot name="toolbar" /></template></BrowserSearchGuide>
  <EmptyState v-if="loaded&&loadError" data-search-load-error :title="copy('search.loadFailed')" :description="loadError" :icon="CircleAlert">
   <template #actions><Button variant="primary" @click="loaded=false;loadSources()"><RefreshCw :size="14" />{{ copy('browser.retry') }}</Button></template>
  </EmptyState>
  <EmptyState v-else-if="loaded&&!usable.length" :title="copy('search.none')" :description="copy('search.noneHelp')" :icon="SearchIcon">
   <template #actions><Button variant="primary" @click="emit('addSource')">{{ copy('search.manage') }}</Button></template>
  </EmptyState>
  <template v-else>
   <form v-if="phone" class="search-bar is-phone" data-search-bar @submit.prevent="search">
    <div class="search-query" data-search-query><SearchField v-model="tab.query" :aria-label="copy('search.query')" :placeholder="copy(wordsOptional?'search.queryOptional':'search.query')" maxlength="256" /></div>
    <span class="search-tune"><IconButton variant="secondary" :label="copy('phone.search.options')" data-search-tune @click="optionsOpen=true"><SlidersHorizontal :size="18" /></IconButton><span v-if="tuned" class="search-tune-dot" data-search-tuned aria-hidden="true" /></span>
   </form>
   <div v-if="phone&&(summaryText||desktopOnly)" class="search-status" data-search-status role="status">
    <button v-if="summaryText" type="button" class="search-status-main" @click="optionsOpen=true">
     <LoaderCircle v-if="summary.running" :size="14" class="search-chip-icon is-spinning" aria-hidden="true" />
     <span>{{ summaryText }}</span>
     <span v-if="summary.problems&&!tab.sourceFilter" class="search-status-warn" :title="counted(summary.problems,'phone.search.problems.one','phone.search.problems','n')"><CircleAlert :size="14" aria-hidden="true" />{{ summary.problems }}</span>
    </button>
    <IconButton v-if="tab.sourceFilter" size="sm" variant="ghost" :label="copy('phone.search.allSources')" data-search-unfilter @click="tab.sourceFilter=''"><X :size="14" /></IconButton>
    <span v-if="desktopOnly&&!tab.sourceFilter" class="search-status-muted" data-search-desktop-only>{{ desktopOnlyText }}</span>
    <Button v-if="(tab.running||Object.keys(assisting).some(id=>assisting[id]))&&tab.jobId" size="sm" variant="ghost" class="search-status-stop" data-search-cancel @click="cancelSearch">{{ copy(tab.rows.length||tab.held.length?'search.stopRest':'add.cancel') }}</Button>
   </div>
   <form v-else-if="!phone" class="search-bar" @submit.prevent="search">
    <div class="search-query" data-search-query><SearchField v-model="tab.query" :aria-label="copy('search.query')" :placeholder="copy(wordsOptional?'search.queryOptional':'search.query')" maxlength="256" /></div>
    <div class="search-category" data-search-category><Select v-model="category" :options="categories" :aria-label="copy('search.category')" /></div>
    <div class="search-sources" data-search-sources><MultipleSelect v-model="chosen" :options="options" :aria-label="copy('search.sources')" :placeholder="copy('search.sources')" :display-limit="2" /></div>
    <Button data-search-submit type="submit" variant="primary" :disabled="!canSearch||tab.running"><SearchIcon :size="14" />{{ copy('search.submit') }}</Button>
    <Button v-if="(tab.running||Object.keys(assisting).some(id=>assisting[id]))&&tab.jobId" data-search-cancel @click="cancelSearch">{{ copy(tab.rows.length||tab.held.length?'search.stopRest':'add.cancel') }}</Button>
   </form>
   <div v-if="!phone&&Object.keys(tab.statuses).length" class="search-statuses" data-search-statuses>
    <span v-for="(x,id) in tab.statuses" :key="id" class="search-chip" :data-state="x.state" :data-tone="statusTone(x.state)" :data-search-chip="id">
     <button type="button" class="search-chip-main" :title="statusReason(String(id))" :aria-expanded="x.state==='done'?undefined:shown===id" :aria-pressed="x.state==='done'?tab.sourceFilter===id:undefined" @click="pickChip(String(id))">
      <component :is="chipIcon(x.state)" :size="14" class="search-chip-icon" :class="{'is-spinning':x.state==='running'||x.state==='browser'}" aria-hidden="true" />
      <span class="search-chip-name">{{ names[id] }}</span><span class="search-chip-state">{{ x.state==='done'?x.count:statusLabel(String(id)) }}</span>
     </button>
     <Button v-if="chipOffer(x)&&!(chipOffer(x)==='retry'&&(tab.running||runningElsewhere))" size="sm" variant="ghost" :data-chip-action="chipOffer(x)" :disabled="!!assisting[id]" @click="chipAct(String(id))">{{ copy(chipOffer(x)==='signin'?'search.signIn':chipOffer(x)==='browser'?'search.openBrowser':'browser.retry') }}</Button>
    </span>
   </div>
   <p v-if="!phone&&enough" class="search-enough" data-search-enough role="status">{{ format(copy('search.enough'),{count:tab.rows.length+tab.held.length,left},ctx.locale) }}</p>
   <div v-if="!phone&&shown&&tab.statuses[shown]" class="search-reason" data-search-reason role="status">
    <p><strong>{{ names[shown] }}</strong> — {{ tab.statuses[shown].state==='done'?format(copy('search.results'),{count:tab.statuses[shown].count},ctx.locale):statusReason(shown) }}</p>
    <details v-if="tab.statuses[shown].code||helper.state.timings?.[shown]"><summary>{{ copy('browser.advanced') }}</summary><code v-if="tab.statuses[shown].code">{{ tab.statuses[shown].code }}</code><pre v-if="helper.state.timings?.[shown]">{{ JSON.stringify(helper.state.timings?.[shown],null,2) }}</pre></details>
    <Space wrap spacing="compact" align-items="center">
    <template v-if="!phone&&tab.statuses[shown].state==='cloudflare'&&tab.statuses[shown].code!=='search_browser_skipped'">
     <Button size="sm" :disabled="helper.state.status!=='ready'||assisting[shown]" @click="assist(shown)">{{ copy('browser.retry') }}</Button>
     <Link v-if="tab.siteURLs[shown]&&tab.statuses[shown].code==='search_browser_verification'" data-open-site :href="tab.siteURLs[shown]" @click.prevent="openChallenge">{{ copy('solver.openSite') }}</Link>
    </template>
    </Space>
   </div>
   <Callout v-if="!phone&&!assistOn&&usable.some(x=>x.cloudflare)" tone="info" data-assist-notice dismiss-key="assist-off" :close-label="copy('search.closeTab')">{{ copy('search.assistOffHelp') }}</Callout>
   <Space v-if="!phone||tab.rows.length>=phoneFilterFrom||tab.held.length" wrap spacing="compact" align-items="center" justify-content="space-between" data-search-results-bar>
    <div v-if="!phone&&(tabs.tabs.length>1||searchUsed(tab))" class="search-tabs" role="group" :aria-label="copy('search.tabs')" data-search-tabs>
     <Tag v-for="x in tabs.tabs" :key="x.id" :selected="x.id===tabs.active" :label="tabLabel(x)" tooltip removable :remove-label="copy('search.closeTab')" @click="tabs.active=x.id" @remove="closeTab(x)" />
    </div>
    <Space class="search-tools" spacing="compact" align-items="center">
     <Badge v-if="!phone&&!assistOn&&usable.some(x=>x.cloudflare)" tone="neutral" data-assist-off><Globe :size="12" aria-hidden="true" />{{ copy('search.assistOff') }}</Badge>
     <div v-if="!phone||tab.rows.length>=phoneFilterFrom" class="search-filter"><SearchField v-model="tab.filter" data-search-filter variant="filter" :placeholder="copy('search.filter')" /></div>
     <div v-if="!phone" class="search-sort" data-search-sort><Select v-model="sortValue" :options="sortOptions" :aria-label="copy('search.sort')" /></div>
     <Button v-if="tab.held.length" class="search-held" data-search-held size="sm" variant="secondary" @click="mergeHeld(tab)">{{ format(copy('search.newResults'),{count:tab.held.length},ctx.locale) }}</Button>
    </Space>
   </Space>
   <div class="search-results" data-search-results>
    <LoadingArea v-if="empty==='searching'||masked" :text="copy('search.state.running')" role="status" />
    <EmptyState v-else-if="empty==='start'" :icon="SearchIcon" :title="copy('search.start')" />
    <EmptyState v-else-if="empty==='login'" :icon="LogIn" :title="copy('search.loginTitle')" :description="copy('search.loginHelp')">
     <template #actions><Button variant="primary" data-search-manage @click="emit('addSource')">{{ copy('search.signIn') }}</Button></template>
    </EmptyState>
    <EmptyState v-else-if="empty==='failed'" :icon="CircleAlert" :title="copy('search.allFailed')" :description="copy('search.allFailedHelp')" />
    <EmptyState v-else-if="empty==='none'" :icon="SearchIcon" :title="copy('search.state.empty')" :description="copy('search.noResultsHelp')">
     <template #actions><Button data-search-manage @click="emit('addSource')">{{ copy('search.manage') }}</Button></template>
    </EmptyState>
    <EmptyState v-else-if="!rows.length" :icon="SearchIcon" :title="copy('search.noMatch')" />
    <ul v-else class="search-list" role="list">
     <li v-for="r in rows.slice(0,limit)" :key="r.id" class="search-row" data-search-row :data-row-key="r.id">
      <div class="search-row-main">
       <div class="search-title" data-search-title :title="r.title">{{ r.title }}</div>
       <div class="search-meta">
        <template v-for="(m,i) in meta(r)" :key="i"><span v-if="i" class="search-dot" aria-hidden="true">·</span><span>{{ m }}</span></template>
        <template v-if="r.seeders!==null||r.leechers!==null"><span v-if="meta(r).length" class="search-dot" aria-hidden="true">·</span><span class="search-peers" :title="copy('search.column.peers')" data-search-peers><b :data-tone="seederTone(r.seeders)">↑{{ dash(r.seeders) }}</b> ↓{{ dash(r.leechers) }}</span></template>
        <span v-if="meta(r).length||r.seeders!==null||r.leechers!==null" class="search-dot" aria-hidden="true">·</span><span class="search-source" :title="sourceTitle(r)" data-search-source>{{ sourceText(r) }}</span>
        <span v-if="failed[r.id]" class="search-failed" role="alert">{{ copy('search.failed') }} — {{ failed[r.id] }}</span>
       </div>
      </div>
      <template v-if="phone">
       <IconButton v-if="r.id in tab.added" :label="copy('search.addedView')" data-search-view @click="emit('showTask',tab.added[r.id],tab.addedBatches[r.id])"><Check :size="18" /></IconButton>
       <IconButton v-else variant="secondary" data-search-download :label="`${copy('search.download')} ${r.title}`" :disabled="!!sending[r.id]" @click="download(r)"><Download :size="18" /></IconButton>
      </template>
      <Button v-else-if="r.id in tab.added" size="sm" variant="ghost" data-search-view @click="emit('showTask',tab.added[r.id],tab.addedBatches[r.id])"><Check :size="14" />{{ copy('search.addedView') }}</Button>
      <Button v-else data-search-download size="sm" variant="secondary" :aria-label="`${copy('search.download')} ${r.title}`" :disabled="!!sending[r.id]" @click="download(r)"><Download :size="14" />{{ copy('search.download') }}</Button>
     </li>
     <li v-if="rows.length>limit" class="search-more"><Button size="sm" variant="ghost" @click="limit+=pageSize">{{ copy('search.showMore') }} ({{ rows.length-limit }})</Button></li>
    </ul>
   </div>
  </template>
  <BottomSheet v-if="phone" v-model:open="optionsOpen" :title="copy('phone.search.options')" data-search-options>
   <div class="search-options">
    <label class="search-option"><span>{{ copy('search.category') }}</span><div data-search-category><Select v-model="category" :options="categories" :aria-label="copy('search.category')" /></div></label>
    <label class="search-option"><span>{{ copy('search.sources') }}</span><div data-search-sources><MultipleSelect v-model="chosen" :options="options" :aria-label="copy('search.sources')" :placeholder="copy('search.sources')" :display-limit="2" /></div></label>
    <label class="search-option"><span>{{ copy('search.sort') }}</span><div data-search-sort><Select v-model="sortValue" :options="sortOptions" :aria-label="copy('search.sort')" /></div></label>
    <ul v-if="Object.keys(tab.statuses).length" class="search-source-list" data-search-statuses>
     <li v-for="(x,id) in tab.statuses" :key="id" :data-search-chip="id" :data-state="x.state" :data-tone="statusTone(x.state)" class="search-source-item" :class="{'is-filtered':tab.sourceFilter===id}">
      <button type="button" class="search-source-main" :disabled="x.state!=='done'" @click="pickSource(String(id))">
       <component :is="chipIcon(x.state)" :size="16" class="search-chip-icon" :class="{'is-spinning':x.state==='running'||x.state==='browser'}" aria-hidden="true" />
       <span class="search-source-text"><span class="search-source-name">{{ names[id] }}</span><span v-if="x.state!=='done'&&x.state!=='running'" class="search-source-reason">{{ statusReason(String(id)) }}</span></span>
       <span class="search-chip-state">{{ x.state==='done'?x.count:statusLabel(String(id)) }}</span>
      </button>
      <Button v-if="chipOffer(x)&&!(chipOffer(x)==='retry'&&(tab.running||runningElsewhere))" size="sm" variant="ghost" :data-chip-action="chipOffer(x)" @click="chipAct(String(id))">{{ copy(chipOffer(x)==='signin'?'search.signIn':'browser.retry') }}</Button>
     </li>
    </ul>
    <p v-if="desktopOnly" class="search-status-muted">{{ desktopOnlyText }}</p>
   </div>
   <template v-if="tuned" #footer><Button data-search-reset @click="resetOptions">{{ copy('ui.settings.reset') }}</Button></template>
  </BottomSheet>
  <Dialog :open="!!choosing" :title="copy('search.chooseTitle')" :description="copy('search.chooseHelp')" @update:open="$event||(choosing=null)">
   <DestinationRow :ctx="ctx" :destination="destination" @authorized="emit('authorized')" />
   <template #footer>
    <Button @click="choosing=null">{{ copy('add.cancel') }}</Button>
    <Button variant="primary" :disabled="!activePath(destination)" @click="confirmChoice">{{ copy('search.download') }}</Button>
   </template>
  </Dialog>
 </div>
</template>
<style scoped>
.search-bar {display:flex;flex-wrap:wrap;gap:var(--ui-space-2);align-items:center;}
.search-query {flex:2 1 220px;min-width:0;}
.search-sources {flex:1 1 200px;min-width:0;}
.search-category {flex:0 1 160px;min-width:120px;}
.search-tabs {display:flex;flex:1 1 200px;min-width:0;gap:var(--ui-space-1);overflow-x:auto;padding-bottom:2px;}
.search-tabs > * {flex:0 0 auto;max-width:220px;}
.search-statuses {display:flex;flex-wrap:wrap;gap:var(--ui-space-2);}
.search-chip {--chip:var(--ui-text-secondary);display:inline-flex;align-items:center;max-width:100%;min-height:28px;border:1px solid var(--ui-border-subtle);border-radius:var(--ui-radius-pill);background:var(--ui-surface-raised,#fff);}
.search-chip[data-tone="success"] {--chip:var(--ui-success,#2e9e5b);}
.search-chip[data-tone="danger"] {--chip:var(--ui-danger);}
.search-chip[data-tone="warning"],.search-chip[data-state="cloudflare"],.search-chip[data-state="login"] {--chip:var(--ui-orange-9,#d97706);}
.search-chip[data-tone="info"] {--chip:var(--ui-primary);}
.search-chip-main {display:inline-flex;align-items:center;gap:6px;min-width:0;padding:3px 10px;border:0;border-radius:var(--ui-radius-pill);background:transparent;color:inherit;font:inherit;cursor:pointer;}
.search-chip-main[aria-pressed="true"],.search-chip-main[aria-expanded="true"] {background:var(--ui-blue-3,#e6f0fb);}
.search-chip:has([aria-pressed="true"]) {border-color:var(--ui-primary);}
.search-chip-icon {flex:none;color:var(--chip);}
.search-chip-icon.is-spinning {animation:search-spin 1s linear infinite;}
@media (prefers-reduced-motion: reduce) {.search-chip-icon.is-spinning {animation:none;}}
@keyframes search-spin {to {transform:rotate(360deg);}}
.search-chip-name {min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.search-chip-state {color:var(--chip);font-weight:600;white-space:nowrap;}
.search-chip > :deep(button:not(.search-chip-main)) {margin-inline-end:2px;}
.search-sort {flex:0 1 160px;min-width:130px;}
/* Filter and sort stay on one line beside the tabs. */
.search-tools {flex:0 1 auto;min-width:0;margin-inline-start:auto;flex-wrap:nowrap;}
.search-tools :deep([data-assist-off]) {white-space:nowrap;flex:none;}
.search-filter {flex:0 1 220px;min-width:140px;}
.search-filter > * {width:100%;min-width:0;}
.search-results {display:flex;flex:1 1 auto;min-height:0;flex-direction:column;overflow:auto;}
.search-list {margin:0;padding:0;list-style:none;}
.search-row {display:flex;align-items:center;gap:var(--ui-space-3);padding:var(--ui-space-2) var(--ui-space-1);border-bottom:1px solid var(--ui-border-subtle);}
.search-row:hover {background:var(--ui-surface-hover,#f5f8fb);}
.search-row-main {flex:1;min-width:0;}
.search-title {display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;overflow-wrap:anywhere;font-weight:600;line-height:var(--ui-line-height);}
.search-meta {display:flex;flex-wrap:wrap;align-items:center;gap:0 6px;margin-top:2px;color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);}
.search-dot {color:var(--ui-text-tertiary,#9aa5b1);}
.search-peers b {font-weight:600;}
.search-peers b[data-tone="success"] {color:var(--ui-success,#2e9e5b);}
.search-peers b[data-tone="warning"] {color:var(--ui-orange-9,#d97706);}
.search-peers b[data-tone="danger"] {color:var(--ui-danger);}
.search-failed {flex-basis:100%;color:var(--ui-danger);}
.search-more {display:flex;justify-content:center;padding:var(--ui-space-2);}
.search-enough{margin:0;color:var(--ui-text-secondary)}
.search-held{flex-shrink:0;white-space:nowrap}
.search-bar.is-phone {flex-wrap:nowrap;}
.search-bar.is-phone .search-query {flex:1;}
.search-tune {position:relative;display:inline-flex;flex:none;}
.search-tune-dot {position:absolute;top:4px;right:4px;width:8px;height:8px;border:2px solid var(--ui-surface);border-radius:50%;background:var(--ui-primary);pointer-events:none;}
/* The results fill the rest of the page's height-limited column, so the status
   line must not shrink: a wrapped second line would spill over the filter. */
.search-status {flex:none;display:flex;flex-wrap:wrap;align-items:center;gap:0 var(--ui-space-2);min-height:var(--ui-hit-min,44px);margin-block:calc(-1 * var(--ui-space-2));color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);}
.search-status-main {display:inline-flex;align-items:center;gap:6px;min-height:var(--ui-hit-min,44px);padding:0;border:0;background:none;color:var(--ui-text);font:inherit;cursor:pointer;}
.search-status-warn {display:inline-flex;align-items:center;gap:2px;color:var(--ui-warning-text,#b45309);}
.search-status-muted {color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);}
.search-status-main + .search-status-muted::before {content:"·";margin-inline-end:var(--ui-space-2);}
.search-status-stop {margin-inline-start:auto;}
.is-phone .search-title {font-weight:500;}
.is-phone .search-row {padding-inline:0;}
.search-options {display:grid;gap:var(--ui-space-4);padding-bottom:var(--ui-space-2);}
.search-option {display:grid;gap:6px;}
.search-option > span {color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);}
.search-source-list {margin:0;padding:0;list-style:none;border-top:1px solid var(--ui-border-subtle);}
.search-source-item {--chip:var(--ui-text-secondary);display:flex;align-items:center;gap:var(--ui-space-2);border-bottom:1px solid var(--ui-border-subtle);}
.search-source-item[data-tone="success"] {--chip:var(--ui-success,#2e9e5b);}
.search-source-item[data-tone="danger"] {--chip:var(--ui-danger);}
.search-source-item[data-tone="warning"],.search-source-item[data-state="cloudflare"],.search-source-item[data-state="login"] {--chip:var(--ui-orange-9,#d97706);}
.search-source-item[data-tone="info"] {--chip:var(--ui-primary);}
.search-source-item.is-filtered {background:var(--ui-surface-selected);}
.search-source-main {display:flex;flex:1;align-items:center;gap:var(--ui-space-3);min-width:0;min-height:var(--ui-list-row-height,48px);padding:var(--ui-space-1) 0;border:0;background:none;color:inherit;font:inherit;text-align:start;}
.search-source-main:disabled {cursor:default;opacity:1;}
.search-source-text {display:grid;flex:1;min-width:0;}
.search-source-name {overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.search-source-reason {color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);}
.search-reason {margin:0;color:var(--ui-text-secondary);overflow-wrap:anywhere;}
</style>
