<script setup lang="ts">
import {inject,computed,onBeforeUnmount,onMounted,reactive,ref,useId,watch} from 'vue'
import {Badge,Button,Callout,Checkbox,Dialog,Form,IconButton,Link,Space,Textarea} from '@synopackageland/ui'
import {X} from '@lucide/vue'
import MediaOptionsForm from './MediaOptionsForm.vue'
import {defaultMediaOptions,type MediaOptions} from './media-options'
import {appCopy} from './app-copy'
import {activePath,ensureDestinationKey,type EnsureDestination} from './destination'
import DestinationRow from './DestinationRow.vue'
import {extractLinks,readClipboardLinks} from './links'
import {canStart,counted,linkKey,startLabel,submitLinks,type LinkItem} from './add-download'
import {collectLinks,groupOf,linkGroups,patternCap,totalCap,type LinkGroup} from './link-groups'
import {readTorrent} from './download-submission'
import type {IntakeDraft} from './mobile-intake'
import {format,type Context} from '@synopackageland/sdk'
import type {DestinationState} from '@synopackageland/sdk'
// destination: the App's read of where saves go; undefined while it reads,
// null when it could not. initial: links the dialog opens with (a deep link
// or the phone's one-line field), in place of the clipboard's.
const ensureDestination=inject<EnsureDestination|undefined>(ensureDestinationKey,undefined)
const props=defineProps<{ctx:Context;reason:(e:any)=>string;destination?:DestinationState|null;initial?:string;initialMediaOptions?:MediaOptions;shared?:IntakeDraft}>()
const emit=defineEmits<{close:[];added:[message:string];refused:[error:any];authorized:[];choose:[keys:string[]]}>()
const formId='add-download-'+useId()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const mediaOpen=ref(false),useMediaDefaults=ref(!props.initialMediaOptions),mediaOptions=ref<MediaOptions>(props.initialMediaOptions??defaultMediaOptions())
const mediaCopy=(key:string)=>copy('mediaOptions.'+key)
const checking=computed(()=>props.destination===undefined)
const text=ref(props.initial??''),fromClipboard=ref(false),torrents=ref<{key:string;name:string;data:string}[]>([])
const classified=ref<Record<string,{kind?:string;route?:string}>>({}),busy=ref(false),error=ref('')
const failures=ref<{label:string;reason:string}[]>([]),fileInput=ref<HTMLInputElement>()
// Choosing a torrent's files is off until the user turns it on; each DSM
// user's choice is kept in this browser.
const pickKey=`SynoPkgGopeed.chooseFiles.${props.ctx.user?.name??''}`
const pickFiles=ref(false)
try{pickFiles.value=localStorage.getItem(pickKey)==='1'}catch{}
watch(pickFiles,on=>{try{on?localStorage.setItem(pickKey,'1'):localStorage.removeItem(pickKey)}catch{}})

// The field's links after patterns and repeats, each in the group of the
// route the backend chose. A torrent file joins the BitTorrent group.
type Row={key:string;url:string;label:string;group:LinkGroup;reason?:string;torrent?:boolean}
const collected=computed(()=>collectLinks(text.value))
const rows=computed<Row[]>(()=>[
 ...collected.value.links.map(({url})=>{const r=classified.value[url],group=groupOf(r);return {key:url,url,label:url,group,reason:group==='none'?copy('add.reason.'+(r?.kind==='blocked'?'blocked':'unsupported')):undefined}}),
 ...torrents.value.map(t=>({key:t.key,url:t.data,label:t.name,group:'torrent' as const,torrent:true})),
])
const sections=computed(()=>linkGroups.map(key=>({key,rows:rows.value.filter(r=>r.group===key)})).filter(s=>s.rows.length))
// Links the user unticked; every other addable link is sent.
const excluded=reactive<Record<string,boolean>>({})
const chosen=computed(()=>rows.value.filter(r=>r.group!=='none'&&!excluded[r.key]))
const count=computed(()=>chosen.value.length)
const tally=computed(()=>{
 const c=collected.value
 if(!rows.value.length)return ''
 return [counted(props.ctx.locale,'add.count',rows.value.length),c.duplicates?counted(props.ctx.locale,'add.duplicates',c.duplicates):'',c.expanded?counted(props.ctx.locale,'add.expanded',c.expanded):''].filter(Boolean).join(' · ')
})
const allOn=(s:{rows:Row[]})=>s.rows.every(r=>!excluded[r.key])
const someOn=(s:{rows:Row[]})=>!allOn(s)&&s.rows.some(r=>!excluded[r.key])
function setGroup(s:{rows:Row[]},on:boolean){for(const r of s.rows)excluded[r.key]=!on}
const path=computed(()=>activePath(props.destination))
const ready=computed(()=>canStart({count:count.value,ready:(!checking.value)&&(!!path.value||!!ensureDestination),busy:busy.value}))

// The backend names each link's kind and route; the dialog asks once per
// new link, shortly after typing stops.
let classifyTimer:number|undefined,disposed=false
watch(()=>collected.value.links.map(l=>l.url).filter(url=>!(url in classified.value)),urls=>{
 clearTimeout(classifyTimer)
 if(urls.length)classifyTimer=window.setTimeout(()=>void classify(urls.slice(0,totalCap)),250)
})
async function classify(urls:string[]){
 try{
  const out:any=await props.ctx.call('downloads.classify',{urls})
  if(!disposed)classified.value={...classified.value,...Object.fromEntries((out?.results??[]).map((r:any)=>[r.url,{kind:r.kind,route:r.route}]))}
 }catch{/* The links then wait in "Checking" and are still sent. */}
}
onMounted(async()=>{
 void props.ctx.call<any>('media.settings.get',{}).then(p=>{if(p?.defaults&&!disposed&&!props.initialMediaOptions&&useMediaDefaults.value)mediaOptions.value={...defaultMediaOptions(),...p.defaults}}).catch(()=>{})
 const links=props.shared?[]:await readClipboardLinks()
 if(links.length&&!disposed&&!text.value){text.value=links.join('\n');fromClipboard.value=true}
})
onBeforeUnmount(()=>{disposed=true;clearTimeout(classifyTimer)})

function clear(){text.value='';fromClipboard.value=false}
// A paste with links keeps only them, one per line, where the cursor is.
function paste(event:ClipboardEvent){
 const links=extractLinks(event.clipboardData?.getData('text/plain')||event.clipboardData?.getData('text')||'')
 if(!links.length)return
 event.preventDefault()
 const field=event.target as HTMLTextAreaElement,start=field.selectionStart??text.value.length,end=field.selectionEnd??start
 const before=text.value.slice(0,start),after=text.value.slice(end)
 text.value=before+(before&&!before.endsWith('\n')?'\n':'')+links.join('\n')+(after&&!after.startsWith('\n')?'\n':'')+after
}
// A .txt file is a list of links, as Download Station opens; it is read
// here and never uploaded.
async function addList(file:File){
 if(file.size>256*1024){error.value=copy('add.listTooLarge');return}
 const content=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result));reader.onerror=()=>reject(reader.error);reader.readAsText(file)})
 const links=extractLinks(content)
 if(!links.length)return
 text.value=[text.value.replace(/\n+$/,''),...links].filter(Boolean).join('\n');fromClipboard.value=false;error.value=''
}
async function addTorrents(files:ArrayLike<File>){
 for(const file of Array.from(files)){
  if(file.name.toLowerCase().endsWith('.txt')){await addList(file).catch(e=>{error.value=props.reason(e)});continue}
  if(!file.name.toLowerCase().endsWith('.torrent'))continue
  try{torrents.value=[...torrents.value,{key:linkKey(),name:file.name,data:await readTorrent(file)}];error.value=''}
  catch(e){error.value=props.reason(e)}
 }
}
function drop(event:DragEvent){event.preventDefault();void addTorrents(event.dataTransfer?.files??[])}
function choose(event:Event){const input=event.target as HTMLInputElement;void addTorrents(input.files??[]).then(()=>{input.value=''})}
function remove(key:string){torrents.value=torrents.value.filter(t=>t.key!==key)}

// A warm share joins the open draft without replacing the owner's edits.
watch(()=>props.shared,draft=>{
 if(!draft)return
 text.value=[text.value.replace(/\n+$/,''),draft.text].filter(Boolean).join('\n')
 fromClipboard.value=false
 void addTorrents(draft.files)
},{immediate:true})

// Every link is sent; the ones that fail stay in the field with their reason.
async function start(){
 if(!ready.value)return
 const selectedPath=path.value??await ensureDestination?.();if(!selectedPath)return
 const links:LinkItem[]=chosen.value.filter(r=>!r.torrent).map(r=>({key:linkKey(),url:r.url}))
 const items=[...links,...chosen.value.filter(r=>r.torrent).map(r=>({key:r.key,url:r.url}))]
 busy.value=true;error.value='';failures.value=[]
 try{
  // A closed dialog stops sending; links still waiting then fail quietly.
  const call=(method:string,params:Record<string,unknown>)=>disposed?Promise.reject({code:'canceled'}):props.ctx.call(method,method==='downloads.create_many'&&!useMediaDefaults.value?{...params,mediaOptions:mediaOptions.value}:params)
  const results=await submitLinks(call,selectedPath,items)
  if(disposed)return
  const created=results.filter(r=>r.status==='created').length,failed=new Set(results.filter(r=>r.status==='failed').map(r=>r.key))
  if(created){emit('added',counted(props.ctx.locale,'add.added',created));emit('authorized')}
  if(pickFiles.value)emit('choose',results.filter(r=>r.status==='created'&&r.kind==='torrent').map(r=>r.key))
  if(!failed.size){emit('close');return}
  const names=new Map(torrents.value.map(t=>[t.key,t.name]))
  failures.value=results.filter(r=>failed.has(r.key)).map(r=>({label:names.get(r.key)??r.url,reason:props.reason({code:r.code})}))
  text.value=links.filter(l=>failed.has(l.key)).map(l=>l.url).join('\n');fromClipboard.value=false
  torrents.value=torrents.value.filter(t=>failed.has(t.key))
 }catch(e){emit('refused',e);if(!disposed)error.value=props.reason(e)}
 finally{if(!disposed){busy.value=false}}
}
</script>
<template>
 <Dialog :open="true" :title="copy('add.title')" :description="copy('add.description')" size="lg" @update:open="$event||emit('close')">
  <form :id="formId" class="dialog-form add-download" @submit.prevent="start">
   <div class="add-links" @paste="paste" @dragover.prevent @drop="drop">
    <Space class="add-links-head" wrap spacing="compact" justify-content="space-between">
     <span>{{ copy('add.links') }}</span>
     <span class="add-clipboard">
      <template v-if="fromClipboard"><Badge tone="info">{{ copy('add.fromClipboard') }}</Badge><Button size="sm" variant="ghost" :disabled="busy" @click="clear">{{ copy('add.clear') }}</Button></template>
     </span>
    </Space>
    <Textarea v-model="text" :rows="4" :aria-label="copy('add.links')" :placeholder="copy('add.placeholder')" :disabled="busy" spellcheck="false" />
    <Space class="add-links-foot" wrap spacing="compact" justify-content="space-between">
     <span role="status" data-add-tally>{{ tally }}</span>
     <Link href="#" @click.prevent="fileInput?.click()">{{ copy('add.addTorrent') }}</Link>
     <input ref="fileInput" class="add-file" type="file" hidden multiple accept=".torrent,application/x-bittorrent,.txt,text/plain" @change="choose" />
    </Space>
    <Callout v-if="collected.unknown.length" tone="warning" data-add-unknown :close-label="copy('details.close')">{{ format(copy('add.unknownLines'),{lines:collected.unknown.join(', ')}) }}</Callout>
    <Callout v-if="collected.cappedPattern||collected.cappedTotal" tone="warning" variant="inline" data-add-cap>{{ [collected.cappedPattern?format(copy('add.patternCap'),{n:patternCap}):'',collected.cappedTotal?format(copy('add.totalCap'),{n:totalCap}):''].filter(Boolean).join(' · ') }}</Callout>
   </div>
   <div v-if="sections.length" class="add-groups" data-add-groups>
    <section v-for="s in sections" :key="s.key" class="add-group" :data-add-group="s.key">
     <header class="add-ghead">
      <Checkbox v-if="s.key!=='none'" class="add-gcheck" :model-value="allOn(s)" :indeterminate="someOn(s)" :disabled="busy" :label="copy('add.group.'+s.key)" data-add-group-check @update:model-value="setGroup(s,$event)" />
      <span v-else class="add-gname">{{ copy('add.group.none') }}</span>
      <span class="add-gcount">{{ s.rows.length }}</span>
      <span class="add-grow" />
      <Checkbox v-if="s.key==='torrent'" v-model="pickFiles" data-choose-files :disabled="busy" :label="copy('add.chooseFiles')" />
     </header>
     <ul class="add-rows">
      <li v-for="r in s.rows" :key="r.key" class="add-row" :data-add-row="r.label">
       <Checkbox v-if="s.key!=='none'" :model-value="!excluded[r.key]" :disabled="busy" :label="r.label" @update:model-value="excluded[r.key]=!$event" />
       <span v-else class="add-row-off"><span class="add-row-url">{{ r.label }}</span><span class="add-row-reason">{{ r.reason }}</span></span>
       <IconButton v-if="r.torrent" size="sm" :label="format(copy('add.remove'),{name:r.label})" :disabled="busy" @click="remove(r.key)"><X :size="14" /></IconButton>
      </li>
     </ul>
    </section>
   </div>
   <Button data-media-advanced :disabled="busy" @click="mediaOpen=true">{{ mediaCopy('advanced') }}</Button>
   <Callout v-if="failures.length" tone="danger" :dismissible="false"><div role="alert" class="add-failures">
    <p>{{ counted(ctx.locale,'add.failed',failures.length) }}</p>
    <ul><li v-for="(f,i) in failures" :key="i"><span class="add-failed-link">{{ f.label }}</span> — {{ f.reason }}</li></ul>
   </div></Callout>
   <Callout v-else-if="error" tone="danger" :dismissible="false"><span role="alert">{{ error }}</span></Callout>
  </form>
  <template #footer>
   <div class="add-foot"><DestinationRow :ctx="ctx" :destination="destination" :busy="busy" @authorized="emit('authorized')" /></div>
   <Button @click="emit('close')">{{ copy('add.cancel') }}</Button>
   <Button :form="formId" type="submit" variant="primary" data-add-start :disabled="!ready">{{ startLabel(ctx.locale,count) }}</Button>
  </template>
 </Dialog>
 <Dialog v-model:open="mediaOpen" :title="mediaCopy('advanced')" :description="mediaCopy('overrideHelp')" size="md">
  <Form><Checkbox v-model="useMediaDefaults" :label="mediaCopy('useDefaults')" /><MediaOptionsForm v-if="!useMediaDefaults" v-model="mediaOptions" :locale="ctx.locale" /></Form>
  <template #footer><Button variant="primary" @click="mediaOpen=false">{{ copy('ui.settings.apply') }}</Button></template>
 </Dialog>
</template>
<style scoped>
.add-download {gap:var(--ui-space-3);}
.add-links {display:grid;gap:var(--ui-space-2);}
.add-links-head,.add-links-foot {min-height:var(--ui-control-sm);}
.add-links-foot [role=status] {color:var(--ui-text-secondary);}
.add-clipboard {display:inline-flex;align-items:center;gap:var(--ui-space-1);}
.add-groups {display:grid;gap:var(--ui-space-2);max-height:min(320px,40vh);overflow:auto;padding:var(--ui-space-1) var(--ui-space-2);border:1px solid var(--ui-border-subtle);border-radius:var(--ui-radius-md);}
.add-group + .add-group {padding-top:var(--ui-space-2);border-top:1px solid var(--ui-border-subtle);}
.add-ghead {display:flex;flex-wrap:wrap;align-items:center;gap:var(--ui-space-2);min-height:var(--ui-control-sm);font-weight:600;}
.add-gname {color:var(--ui-text-secondary);}
.add-gcount {min-width:20px;padding:0 6px;border-radius:var(--ui-radius-pill);background:var(--ui-surface-sunken,#eef2f6);color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);font-weight:400;text-align:center;}
.add-grow {flex:1;}
.add-ghead [data-choose-files] {font-weight:400;}
.add-rows {display:grid;gap:2px;margin:0;padding:0 0 0 var(--ui-space-5);list-style:none;}
.add-row {display:flex;align-items:center;justify-content:space-between;gap:var(--ui-space-2);min-width:0;overflow-wrap:anywhere;}
.add-row :deep([data-ui-checkbox]) {min-width:0;}
.add-row-off {display:grid;min-width:0;}
.add-row-url {color:var(--ui-text-secondary);}
.add-row-reason {font-size:var(--ui-font-size-sm);color:var(--ui-warning-text);}
.add-failures p {margin:0 0 var(--ui-space-1);}
.add-failures ul {margin:0;padding-inline-start:var(--ui-space-4);}
.add-failed-link {overflow-wrap:anywhere;font-weight:600;}
.add-file {display:none;}
.add-foot {flex:1 1 240px;min-width:0;margin-inline-end:auto;}
.add-foot :deep(.add-location) {padding-top:0;border-top:0;}
</style>
