<script setup lang="ts">
import {computed,onMounted,onUnmounted,reactive,ref} from 'vue'
import {Badge,Button,Callout,Dialog,DropdownMenu,FormNote,IconButton,KeyValueRow,ListItem,Input,SettingsRow,SettingsSection,Space,Switch,useToast,formatDateTime} from '@synopackageland/ui'
import {MoreHorizontal} from '@lucide/vue'
import SitePicker from './SitePicker.vue'
import {format,type Context} from '@synopackageland/sdk'
import {appCopy} from './app-copy'
import SourceDialog from './SourceDialog.vue'
import BrowserSearchGuide from './BrowserSearchGuide.vue'
import GitHubNotice from './GitHubNotice.vue'
import {githubFailure,type GitHubFailure} from './github'
import {startSearchOperation,followSearchJob,searchRequestKey} from './search-jobs'
import {needsSignIn,siteStatus,siteTone,sourceProblem,type Catalog,type CatalogSite,type SourceView} from './search'
const toast=useToast()
const jobsController=new AbortController()
onUnmounted(()=>jobsController.abort())
const props=defineProps<{ctx:Context;admin:boolean;reason:(e:any)=>string;query?:string}>()
const emit=defineEmits<{saved:[source:SourceView];refused:[error:any];githubToken:[]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const fill=(key:string,values:Record<string,string|number>)=>format(copy(key),values,props.ctx.locale)
const sources=ref<SourceView[]>([]),catalog=ref<Catalog>(),loading=ref(true),error=ref('')
const solverURL=ref(''),solverBusy=ref(false),solverReady=ref(false),solverError=ref('')
const solverInput=ref<HTMLElement|null>(null)
const picking=ref(false),editing=ref<{source?:SourceView;site?:CatalogSite;kinds?:string[]}|null>(null)
// Feeds and imports: every kind but a site from the list.
const feedKinds=['rss','torznab','cardigann','ytsearch','bilisearch']
const deleting=ref<SourceView|null>(null),rollingBack=ref(false),updating=ref(false),definitionError=ref(''),definitionGitHub=ref<GitHubFailure>()
const rowBusy=reactive<Record<string,boolean>>({}),tests=reactive<Record<string,string>>({}),rowErrors=reactive<Record<string,string>>({})

async function attempt<T>(fn:()=>Promise<T>,fail:(text:string)=>void){
 try{return await fn()}catch(e:any){if(e?.name!=='AbortError'){emit('refused',e);fail(props.reason(e))}}
}
async function load(){
 loading.value=true;error.value=''
 await Promise.all([
  attempt(async()=>{sources.value=await props.ctx.call<SourceView[]>('search.sources.list',{})},text=>{error.value=text}),
  attempt(async()=>{catalog.value=await props.ctx.call<Catalog>('search.catalog',{})},text=>{error.value=text}),
 ])
 loading.value=false
}
async function loadSolver(){await attempt(async()=>{const out=await props.ctx.call<{url:string}>('search.solver.get',{});solverURL.value=out.url;solverReady.value=true},text=>{solverError.value=text})}
async function saveSolver(){solverBusy.value=true;solverError.value='';await attempt(async()=>{await props.ctx.call('search.solver.set',{url:solverURL.value.trim()});toast.success(copy('solver.saved'))},text=>{solverError.value=text});solverBusy.value=false}
onMounted(()=>{void load();void loadSolver()})
defineExpose({refresh:load})

// A site's reasons, in words where the App knows them.
const reasonText=(reason:string)=>copy('sources.reason.'+reason.replace(/[^a-z_]/gi,'_'))||fill('sources.reason.other',{reason})
const added=computed(()=>new Set(sources.value.filter(x=>x.kind==='builtin'&&x.definitionId).map(x=>x.definitionId!)))
// What Add source offers: usable sites not yet turned on, public ones first.
const typeOrder=['public','semi-private','private']
const addable=computed(()=>(catalog.value?.sites??[]).filter(x=>x.supported&&!added.value.has(x.id)).sort((a,b)=>typeOrder.indexOf(a.type)-typeOrder.indexOf(b.type)||a.name.localeCompare(b.name)))
const version=computed(()=>{
 const c=catalog.value;if(!c)return ''
 const parts=[fill('sources.commit',{commit:c.commit.slice(0,7)}),fill('sources.siteCount',{supported:c.supported,total:c.total})]
 if(c.updatedAt)parts.splice(1,0,formatDateTime(Date.parse(c.updatedAt),{locale:props.ctx.locale}))
 return parts.join(' · ')
})
// A source's badge: off, or whether it can search as it is; the tooltip says why.
const status=(x:SourceView)=>siteStatus(x)
const statusText=(x:SourceView)=>!x.enabled&&x.supported?copy('sources.off'):copy('sources.status.'+status(x))
const statusTone=(x:SourceView)=>!x.enabled&&x.supported?'neutral':siteTone[status(x)]
const statusHelp=(x:SourceView)=>!x.supported?x.reasons.map(reasonText).join(' '):x.browserReason?copy('browser.unsupported.'+x.browserReason):status(x)==='ready'&&x.configured?copy('sources.configured'):copy('sources.statusHelp.'+status(x))
const menu=(x:SourceView)=>[
 {key:'edit',label:copy('sources.editShort')},
 {key:'test',label:copy('sources.test'),disabled:!x.enabled||!x.supported},
 ...(x.kind==='cardigann'?[{key:'export',label:copy('sources.export')}]:[]),
 {type:'separator' as const,key:'sep'},
 {key:'delete',label:copy('sources.delete')},
]
function pick(x:SourceView,key:string){
 if(key==='edit')editing.value={source:x}
 else if(key==='test')void test(x)
 else if(key==='export')void exportYaml(x)
 else if(key==='delete')deleting.value=x
}

// A site with nothing it needs turns on at once; one that signs in asks first.
async function enable(site:CatalogSite){
 if(needsSignIn(site.settings)){picking.value=false;editing.value={site};return}
 rowBusy[site.id]=true
 await attempt(async()=>saved(await props.ctx.call<SourceView>('search.sources.save',{kind:'builtin',name:site.name,enabled:true,definitionId:site.id})),text=>{error.value=text})
 delete rowBusy[site.id]
}
function saved(source:SourceView){
 const i=sources.value.findIndex(x=>x.id===source.id)
 if(i<0)sources.value=[...sources.value,source];else sources.value.splice(i,1,source)
 editing.value=null;emit('saved',source)
}
async function act(x:SourceView,fn:()=>Promise<void>){
 rowBusy[x.id]=true;delete rowErrors[x.id]
 await attempt(fn,text=>{rowErrors[x.id]=text})
 delete rowBusy[x.id]
}
const toggle=(x:SourceView,enabled:boolean)=>act(x,async()=>{await props.ctx.call('search.sources.enabled',{id:x.id,enabled});x.enabled=enabled})
// Probe the connector, independently of query/result availability.
const test=(x:SourceView)=>act(x,async()=>{
 delete tests[x.id]
 const admission=await startSearchOperation((m,p)=>props.ctx.call(m,p),'search.sources.test.start',{key:searchRequestKey(),id:x.id})
 const out=await followSearchJob((m,p)=>props.ctx.call(m,p),admission.job.id,{signal:jobsController.signal})
 const problem=out.code?sourceProblem(out.code):''
 tests[x.id]=out.status==='available'?copy('sources.testAvailable'):problem==='http'?'HTTP '+out.code.slice(-3):copy('search.state.'+problem)+' — '+copy('search.reason.'+problem)
})
const exportYaml=(x:SourceView)=>act(x,async()=>{
 const {yaml}=await props.ctx.call<{yaml:string}>('search.sources.export',{id:x.id})
 const url=URL.createObjectURL(new Blob([yaml],{type:'text/yaml'})),a=document.createElement('a')
 a.href=url;a.download=(x.name.replace(/[\\/:*?"<>|]+/g,'_')||'source')+'.yml';a.click()
 URL.revokeObjectURL(url)
})
async function remove(){
 const x=deleting.value;deleting.value=null;if(!x)return
 await act(x,async()=>{await props.ctx.call('search.sources.delete',{id:x.id});sources.value=sources.value.filter(y=>y.id!==x.id)})
}
// The update runs in the background of this section; the rest stays usable.
async function update(){
 updating.value=true;definitionError.value='';definitionGitHub.value=undefined
 await attempt(async()=>{try{catalog.value=await props.ctx.call<Catalog>('search.definitions.update',{})}catch(e){definitionGitHub.value=githubFailure(e);throw e}},text=>{definitionError.value=text})
 updating.value=false
}
async function rollback(){
 rollingBack.value=false;updating.value=true;definitionError.value='';definitionGitHub.value=undefined
 await attempt(async()=>{await props.ctx.call('search.definitions.rollback',{});catalog.value=await props.ctx.call<Catalog>('search.catalog',{})},text=>{definitionError.value=text})
 updating.value=false
}
</script>
<template>
 <SettingsSection data-section="search-sources" :title="copy('sources.title')" :description="copy('sources.help')" :loading="loading&&!catalog">
  <template #actions><Space spacing="compact">
   <Button size="sm" variant="ghost" :disabled="loading" @click="load">{{ copy('sources.reload') }}</Button>
   <Button size="sm" data-add-feed @click="editing={kinds:feedKinds}">{{ copy('sources.addFeed') }}</Button>
   <Button size="sm" variant="primary" data-add-site :disabled="!catalog" @click="picking=true">{{ copy('sources.addSite') }}</Button>
  </Space></template>
  <Callout v-if="error" tone="danger"><span role="alert">{{ error }}</span></Callout>
  <KeyValueRow data-definitions :label="copy('sources.version')">
   <Space wrap spacing="compact" align-items="center">
    <span>{{ version }}</span>
    <Badge v-if="updating" tone="neutral" role="status">{{ copy('sources.updating') }}</Badge>
    <template v-if="admin">
     <Button size="sm" :disabled="updating" @click="update">{{ copy('sources.update') }}</Button>
     <Button size="sm" :disabled="updating||!catalog?.rollbackAvailable" @click="rollingBack=true">{{ copy('sources.rollback') }}</Button>
    </template>
   </Space>
  </KeyValueRow>
  <GitHubNotice v-if="definitionError&&definitionGitHub" :failure="definitionGitHub" :locale="ctx.locale" :admin="admin" @token="emit('githubToken')" />
  <Callout v-else-if="definitionError" tone="danger"><span role="alert">{{ definitionError }}</span></Callout>
  <BrowserSearchGuide :locale="ctx.locale" />
  <details data-solver-advanced><summary>{{ copy('solver.settings') }}</summary><form data-cloudflare-solver @submit.prevent="saveSolver" ref="solverInput">
   <SettingsRow :label="copy('solver.url')" :description="copy('solver.help')" field-width="full"><Input v-model="solverURL" type="url" :aria-label="copy('solver.url')" placeholder="http://solver.example:8191" :disabled="!solverReady||solverBusy" /></SettingsRow>
   <FormNote>{{ copy('solver.openHelp') }}</FormNote>
   <Button type="submit" size="sm" :disabled="!solverReady||solverBusy">{{ copy('solver.save') }}</Button>
   <span v-if="solverError" role="alert">{{ solverError }}</span>
  </form></details>
  <SettingsSection :title="copy('sources.mine')" :collapsible="false">
  <Callout v-if="!sources.length&&!loading" tone="info" variant="inline">{{ copy('sources.mineEmpty') }}</Callout>
  <div role="list">
   <ListItem v-for="x in sources" :key="x.id" data-source :title="x.name">
    <template #leading><Switch :model-value="x.enabled" :aria-label="`${copy('sources.on')} ${x.name}`" :disabled="!!rowBusy[x.id]||!x.supported" @update:model-value="toggle(x,$event)" /></template>
    <template #meta><Badge>{{ copy('sources.kind.'+x.kind) }}</Badge><Badge :tone="statusTone(x)" :title="statusHelp(x)" data-source-status>{{ statusText(x) }}</Badge></template>
    <template #description>
     <span v-if="!x.supported">{{ x.reasons.map(reasonText).join(' ') }}</span>
     <span v-if="tests[x.id]" role="status">{{ tests[x.id] }}</span>
     <Callout v-if="rowErrors[x.id]" tone="danger" variant="inline"><span role="alert">{{ rowErrors[x.id] }}</span></Callout>
    </template>
    <template #actions>
     <DropdownMenu :items="menu(x)" align="end" @select="pick(x,$event)">
      <template #trigger><IconButton size="sm" data-source-menu :disabled="!!rowBusy[x.id]" :label="fill('sources.more',{name:x.name})"><MoreHorizontal :size="15" /></IconButton></template>
     </DropdownMenu>
    </template>
   </ListItem>
  </div>
  </SettingsSection>
  <SitePicker v-if="picking&&catalog" :ctx="ctx" :sites="catalog.sites" :added="added" :busy="rowBusy" :reason-text="reasonText" @close="picking=false" @enable="enable" />
  <SourceDialog v-if="editing" :ctx="ctx" :reason="reason" :source="editing.source" :site="editing.site" :sites="addable" :kinds="editing.kinds" @close="editing=null" @saved="saved" @refused="emit('refused',$event)" />
  <Dialog :open="!!deleting" :title="copy('sources.deleteTitle')" :description="deleting?fill('sources.deleteHelp',{name:deleting.name}):''" @update:open="$event||(deleting=null)">
   <template #footer><Button @click="deleting=null">{{ copy('add.cancel') }}</Button><Button variant="danger" @click="remove">{{ copy('sources.delete') }}</Button></template>
  </Dialog>
  <Dialog :open="rollingBack" :title="copy('sources.rollback')" :description="copy('sources.rollbackHelp')" @update:open="$event||(rollingBack=false)">
   <template #footer><Button @click="rollingBack=false">{{ copy('add.cancel') }}</Button><Button variant="primary" @click="rollback">{{ copy('sources.rollback') }}</Button></template>
  </Dialog>
 </SettingsSection>
</template>
