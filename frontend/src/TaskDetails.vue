<script setup lang="ts">
import {computed,onBeforeUnmount,ref,watch} from 'vue'
import {Button,Callout,CellText,DataGrid,Dialog,KeyValueRow,PathDisplay,SectionTabs,Space,formatDateTime,formatDuration,formatFileSize,formatSpeed} from '@synopackageland/ui'
import {appCopy} from './app-copy'
import type {Context} from '@synopackageland/sdk'
// task: the Downloads row the owner opened, refreshed with the list.
// stats: the engine's own counters for the task, read while the panel is open.
// tab: the tab the panel opens on.
const props=defineProps<{ctx:Context;task:any;stats:(id:string)=>Promise<any>;tab?:string}>()
const emit=defineEmits<{close:[]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const tab=ref(props.tab||'general'),peers=ref<any>(null)
const p2p=computed(()=>props.task.protocol==='bt'||props.task.protocol==='ed2k')
// Saved files carry their own save state; other multi-file tasks list what the engine fetches.
const files=computed(()=>{
 const saved=props.task.publication?.files
 if(saved?.length)return saved.map((f:any,i:number)=>({key:String(i),name:f.rel||f.name,size:f.size,state:fileState(f.state)}))
 const listed=props.task.meta?.res?.files||[]
 return listed.length>1?listed.map((f:any,i:number)=>({key:String(i),name:[f.path,f.name].filter(Boolean).join('/'),size:f.size,state:''})):[]
})
// A torrent's files can be chosen until its files are planned for saving.
const choosable=computed(()=>{
 const t=props.task,p=t.publication
 return t.protocol==='bt'&&!p?.published&&!p?.files?.length&&!p?.deleting&&t.status!=='done'&&files.value.length>1
})
const engineChoice=()=>{
 const chosen=props.task.meta?.opts?.selectFiles
 return (Array.isArray(chosen)&&chosen.length?chosen:files.value.map((_:unknown,i:number)=>i)).map(String)
}
// The owner's ticks stay while the list refreshes; only Apply sends them.
const chosen=ref<string[]>(engineChoice()),sent=ref(engineChoice().join()),busy=ref(false),failure=ref('')
const changed=computed(()=>[...chosen.value].sort((a,b)=>+a-+b).join()!==sent.value)
async function apply(){
 const list=chosen.value.map(Number).sort((a,b)=>a-b)
 busy.value=true;failure.value=''
 try{await props.ctx.call('downloads.select_files',{id:props.task.id,files:list});sent.value=list.join()}
 catch(e:any){failure.value=copy('error.'+e?.code)||copy('error.unknown')}
 finally{busy.value=false}
}
const tabs=computed(()=>[
 {value:'general',label:copy('details.general')},
 {value:'transfer',label:copy('details.transfer')},
 ...(files.value.length?[{value:'files',label:`${copy('details.files')} (${files.value.length})`}]:[]),
])
const columns=computed(()=>[
 {key:'name',label:copy('ui.column.name'),flex:1,minWidth:160},
 {key:'size',label:copy('ui.column.size'),width:90,align:'right' as const},
 ...(props.task.publication?.files?.length?[{key:'state',label:copy('ui.column.status'),width:120}]:[]),
])
function fileState(state:string){
 if(state==='committed')return copy('ui.state.saved')
 if(state==='uncertain')return copy('ui.state.saveUncertain')
 if(state==='save_failed'||state==='access_denied'||state==='canceled')return copy('ui.state.saveFailed')
 return copy('ui.state.savePending')
}
const size=(n:unknown)=>formatFileSize(Number(n)>0?Number(n):undefined)
const when=(ms:unknown)=>formatDateTime(ms as number|null,{locale:props.ctx.locale})
const progress=computed(()=>props.task.progress||{})
// Gopeed counts the time spent downloading in nanoseconds.
const used=computed(()=>progress.value.used>0?formatDuration(Math.round(progress.value.used/1e9),{locale:props.ctx.locale}):'—')
const ratio=computed(()=>{
 const r=peers.value?.seedRatio
 if(typeof r==='number')return r.toFixed(2)
 const down=progress.value.downloaded||0
 return down>0?((progress.value.uploaded||0)/down).toFixed(2):'—'
})
// Peer counts only change while the panel is open; reading stops when it closes.
let timer:ReturnType<typeof setInterval>|undefined,alive=true
async function readStats(){
 try{const s=await props.stats(props.task.id);if(alive)peers.value=s}catch{if(alive)peers.value=null}
}
watch(()=>[props.task.id,p2p.value] as const,([,on])=>{
 clearInterval(timer);peers.value=null
 if(on){readStats();timer=setInterval(readStats,5000)}
},{immediate:true})
onBeforeUnmount(()=>{alive=false;clearInterval(timer)})
</script>

<template>
 <Dialog :open="true" :title="task.name" size="lg" @update:open="$event||emit('close')">
  <SectionTabs v-model="tab" :items="tabs" :aria-label="copy('details.title')">
   <template #panel-general>
    <Space direction="column" spacing="compact">
     <KeyValueRow :label="copy('ui.column.status')" :value="task.statusText" />
     <KeyValueRow v-if="task.details" :label="copy('ui.column.details')" :value="task.details" />
     <KeyValueRow :label="copy('ui.column.size')" :value="size(task.size)" />
     <KeyValueRow :label="copy('ui.column.destination')"><PathDisplay v-if="task.folder" :path="task.folder" /><span v-else>—</span></KeyValueRow>
     <KeyValueRow :label="copy('ui.column.source')"><CellText :text="task.source || '—'" wrap /></KeyValueRow>
     <KeyValueRow :label="copy('ui.column.started')" :value="when(task.started)" />
     <KeyValueRow :label="copy('ui.column.completed')" :value="when(task.completed)" />
    </Space>
   </template>
   <template #panel-transfer>
    <Space direction="column" spacing="compact">
     <KeyValueRow :label="copy('details.downloaded')" :value="`${size(progress.downloaded)} / ${size(task.size)}`" />
     <KeyValueRow :label="copy('ui.column.speed')" :value="task.speed===null?'—':formatSpeed(task.speed)" />
     <KeyValueRow :label="copy('ui.column.remaining')" :value="task.remaining===null?'—':formatDuration(task.remaining,{locale:ctx.locale})" />
     <KeyValueRow :label="copy('details.timeUsed')" :value="used" />
     <template v-if="p2p">
      <KeyValueRow :label="copy('details.uploaded')" :value="size(progress.uploaded)" />
      <KeyValueRow :label="copy('details.uploadSpeed')" :value="progress.uploadSpeed>0?formatSpeed(progress.uploadSpeed):'—'" />
      <KeyValueRow :label="copy('details.ratio')" :value="ratio" />
      <KeyValueRow :label="copy('details.peers')" :value="peers?`${peers.activePeers??0} / ${peers.totalPeers??0}`:'—'" data-details-peers />
      <KeyValueRow v-if="task.protocol==='bt'" :label="copy('details.seeders')" :value="peers?String(peers.connectedSeeders??0):'—'" />
      <KeyValueRow v-if="task.protocol==='bt'" :label="copy('details.seedTime')" :value="peers?.seedTime>0?formatDuration(peers.seedTime,{locale:ctx.locale}):'—'" />
     </template>
    </Space>
   </template>
   <template #panel-files>
    <Space direction="column" spacing="regular">
     <DataGrid v-if="choosable" v-model:selected-keys="chosen" selectable data-details-files :fill="false" :columns="columns" :rows="files" row-key="key" :page-sizes="[50,100,200]">
      <template #cell-size="{row}">{{ size(row.size) }}</template>
     </DataGrid>
     <DataGrid v-else data-details-files :fill="false" :columns="columns" :rows="files" row-key="key" :page-sizes="[50,100,200]">
      <template #cell-size="{row}">{{ size(row.size) }}</template>
     </DataGrid>
     <Callout v-if="failure" tone="danger"><span role="alert">{{ failure }}</span></Callout>
     <Space v-if="choosable" spacing="compact" align-items="center" justify-content="space-between">
      <span>{{ copy('details.chooseHelp') }}</span>
      <Button variant="primary" data-apply-files :disabled="busy||!chosen.length||!changed" @click="apply">{{ copy('details.applyFiles') }} ({{ chosen.length }})</Button>
     </Space>
    </Space>
   </template>
  </SectionTabs>
 </Dialog>
</template>

