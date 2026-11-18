<script setup lang="ts">
import {inject,computed,ref,watch} from 'vue'
import {BottomSheet,Button,Callout,Checkbox,DropdownMenu,EmptyState,Fieldset,Form,IconButton,Input,LoadingArea,SegmentControl,SegmentItem} from '@synopackageland/ui'
import {ChevronRight,ClipboardPaste,Download,FolderDown,ListPlus,MoreHorizontal} from '@lucide/vue'
import type {Context,DestinationState} from '@synopackageland/sdk'
import DownloadsList,{type ListRow} from './DownloadsList.vue'
import DestinationRow from './DestinationRow.vue'
import MediaOptionsForm from './MediaOptionsForm.vue'
import {defaultMediaOptions,type MediaOptions} from './media-options'
import {appCopy} from './app-copy'
import {activePath,ensureDestinationKey,type EnsureDestination} from './destination'
import {counted,linkKey,submitLinks} from './add-download'
import {clipboardReadable,pastedLinks,quickLinks} from './quick-add'
import type {DownloadTab} from './downloads-view'

// The phone's Downloads page: one line to paste a link and start it with the
// saved destination, then the list under a compact filter. Options that are
// not needed for one tap (the folder, several links, a torrent file and
// media options) wait in one bottom sheet.
const ensureDestination=inject<EnsureDestination|undefined>(ensureDestinationKey,undefined)
const props=defineProps<{
 ctx:Context;rows:ListRow[];tab:DownloadTab;counts:Record<DownloadTab,number>;taskIds:string[]
 destination?:DestinationState|null;busy:boolean;loading:boolean;empty:boolean;selected:string;summary:string
 actions:{key:string;label:string}[];reason:(e:any)=>string;format:(key:string,values:Record<string,string|number>)=>string
}>()
const emit=defineEmits<{'update:tab':[tab:DownloadTab];open:[id:string];act:[id:string,op:string];menu:[key:string];added:[ids:string[],text:string];refused:[e:any];authorized:[];several:[text:string,mediaOptions?:MediaOptions]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const text=defineModel<string>('text',{default:''})
const field=ref<HTMLElement>(),hint=ref(''),failure=ref(''),optionsOpen=ref(false)
const useMediaDefaults=ref(true),mediaOptions=ref<MediaOptions>(defaultMediaOptions())
const mediaLoading=ref(false),mediaLoaded=ref(false),mediaError=ref('')
watch(optionsOpen,async open=>{
 if(!open||mediaLoaded.value||mediaLoading.value)return
 mediaLoading.value=true;mediaError.value=''
 try{
  const prefs=await props.ctx.call<{defaults?:MediaOptions}>('media.settings.get',{})
  if(useMediaDefaults.value)mediaOptions.value={...defaultMediaOptions(),...prefs?.defaults}
  mediaLoaded.value=true
 }catch(e){mediaError.value=props.reason(e)}finally{mediaLoading.value=false}
})
const path=computed(()=>activePath(props.destination))
const tabs:DownloadTab[]=['all','active','done','failed']

// Links on their way to the engine show at the top of the list at once.
type Pending={key:string;url:string;ids:string[]}
const pending=ref<Pending[]>([])
watch(()=>props.taskIds,ids=>{pending.value=pending.value.filter(p=>!p.ids.length||!p.ids.some(id=>ids.includes(id)))})
const pendingRows=computed<ListRow[]>(()=>pending.value.map(p=>({id:'pending:'+p.key,name:p.url,group:'active',status:copy('phone.adding'),tone:'info',kind:'web',tile:'',percent:null,bar:'active',meta:[copy('phone.adding')],reason:'',controls:{clear:false}})))
const shown=computed(()=>[...pendingRows.value,...props.rows])

function focusField(){field.value?.querySelector('input')?.focus()}
// Over HTTPS Paste reads the clipboard; elsewhere the browser refuses, so it
// opens the keyboard in the field and says where its Paste is.
async function paste(){
 hint.value='';failure.value=''
 const links=clipboardReadable()?await pastedLinks():undefined
 if(links?.length){text.value=links.join(' ');return}
 focusField()
 hint.value=copy(links?'phone.noLinkCopied':'phone.pasteHint')
}
async function start(){
 hint.value='';failure.value=''
 const links=quickLinks(text.value)
 if(!links.length){failure.value=copy('phone.noLink');focusField();return}
 // Without a folder to save to, the options sheet is where to pick one.
 const selectedPath=path.value??await ensureDestination?.();if(!selectedPath){return}
 const items=links.map(url=>({key:linkKey(),url}))
 const mine=new Set(items.map(i=>i.key)),drop=(keys:Set<string>)=>{pending.value=pending.value.filter(p=>!keys.has(p.key))}
 pending.value=[...items.map(i=>({...i,ids:[]})).reverse(),...pending.value]
 text.value=''
 try{
  const results=await submitLinks((m,p)=>props.ctx.call(m,m==='downloads.create_many'&&!useMediaDefaults.value?{...p,mediaOptions:mediaOptions.value}:p),selectedPath,items)
  const created=results.filter(r=>r.status==='created'&&r.ids?.length),failed=results.filter(r=>!created.includes(r))
  // Each placeholder stays until the list shows its task.
  pending.value=pending.value.map(p=>{const r=created.find(c=>c.key===p.key);return r?{...p,ids:r.ids!}:p})
  drop(new Set(failed.map(f=>f.key)))
  if(created.length){emit('added',created.flatMap(r=>r.ids!),counted(props.ctx.locale,'add.added',created.length));emit('authorized')}
  if(created.length&&!failed.length)useMediaDefaults.value=true
  if(failed.length){
   text.value=failed.map(f=>f.url).join(' ')
   failure.value=[...new Set(failed.map(f=>props.reason({code:f.code})))].join(' · ')
  }
 }catch(e){
  drop(mine)
  text.value=links.join(' ');emit('refused',e);failure.value=props.reason(e)
 }
 // A placeholder whose task never shows (a slow list) goes after a while.
 setTimeout(()=>drop(mine),10_000)
}
function several(){optionsOpen.value=false;emit('several',text.value,useMediaDefaults.value?undefined:JSON.parse(JSON.stringify(mediaOptions.value)));useMediaDefaults.value=true}
defineExpose({focusField,start})
</script>
<template>
 <div class="phone-dl" data-phone-downloads>
  <form class="phone-add" data-phone-add @submit.prevent="start">
   <div class="phone-add-line">
    <div ref="field" class="phone-add-field"><Input v-model="text" type="url" enterkeyhint="go" autocomplete="off" :aria-label="copy('phone.link')" :placeholder="copy('phone.link')" data-phone-link @update:model-value="hint='';failure=''" /></div>
    <IconButton type="button" variant="secondary" :label="copy('phone.paste')" data-phone-paste @click="paste"><ClipboardPaste :size="18" /></IconButton>
    <Button type="submit" variant="primary" data-phone-start :disabled="loading"><Download :size="16" />{{ copy('search.download') }}</Button>
   </div>
   <p v-if="hint" class="phone-add-hint" role="status" data-phone-hint>{{ hint }}</p>
   <Callout v-if="failure" tone="danger" :close-label="copy('details.close')" data-phone-failure @close="failure=''"><span role="alert">{{ failure }}</span></Callout>
   <button type="button" class="phone-add-options" data-phone-options @click="optionsOpen=true">
    <FolderDown :size="16" aria-hidden="true" />
    <span class="phone-add-path">{{ path || (destination===undefined ? '' : copy('phone.chooseFolder')) }}</span>
    <span class="phone-add-more">{{ copy('phone.options') }}</span><ChevronRight :size="16" aria-hidden="true" />
   </button>
  </form>
  <SegmentControl class="phone-dl-filter" :model-value="tab" :aria-label="copy('downloads.tabs')" data-phone-filter @update:model-value="emit('update:tab',$event as DownloadTab)">
   <SegmentItem v-for="t in tabs" :key="t" :value="t" :dot="t==='failed'&&counts.failed?`${copy('downloads.tab.failed')} (${counts.failed})`:undefined">{{ copy('phone.filter.'+t) }}</SegmentItem>
  </SegmentControl>
  <div v-if="!empty" class="phone-dl-bar">
   <span class="phone-dl-summary" data-downloads-summary>{{ summary }}</span>
   <DropdownMenu :items="actions" align="end" @select="emit('menu',$event)">
    <template #trigger><IconButton :label="copy('phone.actions')" data-phone-menu><MoreHorizontal :size="18" /></IconButton></template>
   </DropdownMenu>
  </div>
  <div class="phone-dl-scroll">
   <LoadingArea v-if="loading&&empty" :text="copy('ui.loading')" role="status" />
   <EmptyState v-else-if="empty&&!pending.length" :title="copy('ui.downloads.empty')" :icon="Download" />
   <EmptyState v-else-if="!shown.length" :title="copy('downloads.tabEmpty')" :icon="Download" />
   <DownloadsList v-else phone :rows="shown" :selected="selected" :busy="busy" :copy="copy" :format="format" @open="id=>id.startsWith('pending:')||emit('open',id)" @act="(id,op)=>emit('act',id,op)" />
  </div>
  <BottomSheet v-model:open="optionsOpen" :title="copy('phone.options')" data-phone-options-sheet>
   <div class="phone-options">
    <DestinationRow :ctx="ctx" :destination="destination" @authorized="emit('authorized')" />
    <Form class="phone-media" data-phone-media-options>
     <Fieldset :collapsible="false" :title="copy('mediaOptions.advanced')" :description="copy('mediaOptions.overrideHelp')">
      <Checkbox v-model="useMediaDefaults" :disabled="mediaLoading" :label="copy('mediaOptions.useDefaults')" data-media-use-defaults />
      <MediaOptionsForm v-if="!useMediaDefaults" v-model="mediaOptions" :locale="ctx.locale" :disabled="mediaLoading" />
      <Callout v-if="mediaError" tone="danger" :close-label="copy('details.close')">{{ mediaError }}</Callout>
     </Fieldset>
    </Form>
    <slot name="options" />
    <Button class="phone-options-several" data-phone-several @click="several"><ListPlus :size="16" />{{ copy('phone.several') }}</Button>
   </div>
  <template #footer><Button variant="primary" data-phone-options-apply @click="optionsOpen=false">{{ copy('ui.settings.apply') }}</Button></template>
  </BottomSheet>
 </div>
</template>
<style scoped>
.phone-dl {display:flex;flex:1;flex-direction:column;gap:var(--ui-space-3);min-height:0;}
.phone-add {display:grid;gap:var(--ui-space-2);}
.phone-add-line {display:flex;align-items:center;gap:var(--ui-space-2);}
.phone-add-field {flex:1;min-width:0;}
.phone-add-field > * {width:100%;}
.phone-add-line > :deep(button) {flex:none;}
.phone-add-hint {margin:0;color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);}
.phone-add-options {display:flex;align-items:center;gap:var(--ui-space-2);min-height:var(--ui-hit-min,44px);padding:0;border:0;background:none;color:var(--ui-text-secondary);font:inherit;font-size:var(--ui-font-size-sm);text-align:start;cursor:pointer;}
.phone-add-path {flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}
.phone-add-more {color:var(--ui-primary);}
.phone-dl-bar {display:flex;align-items:center;justify-content:space-between;gap:var(--ui-space-2);margin-block:calc(-1 * var(--ui-space-2));}
.phone-dl-summary {flex:1;min-width:0;color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);font-variant-numeric:tabular-nums;}
.phone-dl-scroll {flex:1;min-height:0;overflow:auto;}
.phone-options {display:grid;gap:var(--ui-space-4);padding-bottom:var(--ui-space-2);}
.phone-options :deep(.add-location) {padding-top:0;border-top:0;}
.phone-options-several {justify-self:stretch;}
.phone-media {min-width:0;}
</style>
