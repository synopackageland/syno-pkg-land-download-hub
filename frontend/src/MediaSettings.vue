<script setup lang="ts">
import {computed,onMounted,ref,useId} from 'vue'
import {Button,Callout,Checkbox,Dialog,Fieldset,Form,FormActions,FormItem,IconButton,Input,Select,Textarea,formatDateTime} from '@synopackageland/ui'
import {Trash2} from '@lucide/vue'
import type {Context} from '@synopackageland/sdk'
import {appCopy} from './app-copy'
import DestinationRow from './DestinationRow.vue'
import {validSubfolder} from './subfolder'
import type {DownloadLocation} from './destination'
import MediaOptionsForm from './MediaOptionsForm.vue'
import {defaultMediaOptions,type CookieStatus,type MediaOptions,type MediaPreferences,type MediaRule} from './media-options'
const props=defineProps<{ctx:Context;reason:(e:any)=>string;path?:string;destination?:DownloadLocation|null}>()
const emit=defineEmits<{changed:[]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,'mediaOptions.'+key)
const common=(key:string)=>appCopy(props.ctx.locale,key)
const defaults=ref<MediaOptions>(defaultMediaOptions()),saved=ref(defaultMediaOptions())
const settings=ref<MediaPreferences>({defaults:defaultMediaOptions(),rules:[],follows:[]})
const cookies=ref<CookieStatus>({domains:[]}),error=ref(''),loading=ref(true),saving=ref(false),working=ref(false)
const modal=ref<''|'rule'|'follow'|'cookies'>(''),modalError=ref(''),cookieText=ref(''),fileInput=ref<HTMLInputElement>()
const formId='media-'+useId(),ruleKind=ref('host'),ruleMatch=ref(''),folder=ref(''),ruleOptions=ref(defaultMediaOptions())
const followURL=ref(''),hours=ref(6),consent=ref(false)
const usedFolders=computed(()=>[...new Set([...settings.value.rules.map(r=>r.path),...settings.value.follows.map(f=>f.path)])].filter(p=>p!==''&&validSubfolder(p)).sort())
const changed=computed(()=>JSON.stringify(defaults.value)!==JSON.stringify(saved.value))
const snapshot=(o:MediaOptions)=>JSON.parse(JSON.stringify(o)) as MediaOptions
async function read(){
 loading.value=true;error.value=''
 const result=await Promise.allSettled([props.ctx.call<MediaPreferences>('media.settings.get',{}),props.ctx.call<CookieStatus>('media.cookies.status',{})])
 const prefs=result[0],status=result[1]
 if(prefs.status==='fulfilled'&&prefs.value){defaults.value={...defaultMediaOptions(),...prefs.value.defaults};settings.value={defaults:defaults.value,rules:Array.isArray(prefs.value.rules)?prefs.value.rules:[],follows:Array.isArray(prefs.value.follows)?prefs.value.follows:[]};saved.value=snapshot(defaults.value)}
 else if(prefs.status==='rejected')error.value=props.reason(prefs.reason)
 if(status.status==='fulfilled'&&Array.isArray(status.value?.domains))cookies.value=status.value
 else if(status.status==='rejected')error.value=props.reason(status.reason)
 loading.value=false
}
onMounted(read)
async function save(){saving.value=true;error.value='';try{const o=await props.ctx.call<MediaOptions>('media.settings.save',defaults.value);defaults.value=o;saved.value=snapshot(o)}catch(e){error.value=props.reason(e)}finally{saving.value=false}}
function open(kind:typeof modal.value){modal.value=kind;modalError.value='';cookieText.value='';folder.value='';ruleMatch.value='';ruleKind.value='host';ruleOptions.value=snapshot(defaults.value);followURL.value='';hours.value=6;consent.value=false}
function close(){if(!working.value){modal.value='';cookieText.value='';modalError.value=''}}
async function submit(){
 if(modal.value!=='cookies'&&!validSubfolder(folder.value)){modalError.value=appCopy(props.ctx.locale,'folders.invalid');return}
 working.value=true;modalError.value=''
 try{
  if(modal.value==='cookies'){cookies.value=await props.ctx.call<CookieStatus>('media.cookies.import',{text:cookieText.value});cookieText.value=''}
  else if(modal.value==='rule'){await props.ctx.call('media.rules.save',{[ruleKind.value]:ruleMatch.value,path:folder.value,options:ruleOptions.value});emit('changed')}
  else {await props.ctx.call('media.follows.add',{url:followURL.value,path:folder.value,intervalHours:Number(hours.value),consent:consent.value});emit('changed')}
  modal.value='';await read()
 }catch(e){modalError.value=props.reason(e)}finally{working.value=false}
}
async function remove(kind:'rule'|'follow'|'cookie',id:string){
 error.value=''
 try{if(kind==='cookie')cookies.value=await props.ctx.call<CookieStatus>('media.cookies.delete',{domain:id})
 else{await props.ctx.call(kind==='rule'?'media.rules.delete':'media.follows.delete',{id});await read()}}
 catch(e){error.value=props.reason(e)}
}
async function readFile(event:Event){
 const input=event.target as HTMLInputElement,file=input.files?.[0];input.value='';if(!file)return
 if(file.size>262144){modalError.value=copy('cookieInvalid');return}
 try{cookieText.value=await file.text();modalError.value=''}catch{modalError.value=copy('cookieInvalid')}
}
const ruleSummary=(r:MediaRule)=>(r.host||r.channel||'')+' → '+r.path
</script>
<template>
 <section class="media-settings" data-media-settings :aria-busy="loading">
  <Fieldset :collapsible="false" :title="appCopy(ctx.locale,'folders.title')"><DestinationRow :ctx="ctx" :destination="destination" @authorized="emit('changed')" /></Fieldset>
  <Callout v-if="error" tone="danger" :close-label="common('details.close')"><span role="alert">{{ error }}</span></Callout>
  <Form @submit.prevent="save">
   <Fieldset :collapsible="false" :title="copy('defaults')" :description="copy('defaultsHelp')">
    <MediaOptionsForm v-model="defaults" :locale="ctx.locale" :disabled="loading||saving" />
    <Callout tone="info" :close-label="common('details.close')">{{ copy('ffmpegHelp') }}</Callout>
   </Fieldset>
   <FormActions inset><Button :disabled="!changed||saving" @click="defaults=snapshot(saved)">{{ common('ui.settings.reset') }}</Button><Button type="submit" variant="primary" :disabled="loading||!changed" :loading="saving">{{ common('ui.settings.apply') }}</Button></FormActions>
  </Form>
  <Fieldset :collapsible="false" :title="copy('rules')" :description="copy('rulesHelp')">
   <div v-for="rule in settings.rules" :key="rule.id" class="media-row" data-media-rule><span>{{ ruleSummary(rule) }} · {{ rule.options.format }}</span><IconButton :label="copy('remove')" @click="remove('rule',rule.id)"><Trash2 :size="16" /></IconButton></div>
   <p v-if="!settings.rules.length" class="media-empty">{{ copy('noRules') }}</p>
   <Button data-media-add-rule @click="open('rule')">{{ copy('addRule') }}</Button>
  </Fieldset>
  <Fieldset :collapsible="false" :title="copy('follows')" :description="copy('followsHelp')">
   <div v-for="follow in settings.follows" :key="follow.id" class="media-row" data-media-follow><span>{{ follow.url }}<small>{{ follow.path }} · {{ copy('nextCheck') }} {{ formatDateTime(follow.nextCheck) }}<template v-if="follow.error"> · {{ reason({code:follow.error}) }}</template></small></span><IconButton :label="copy('remove')" @click="remove('follow',follow.id)"><Trash2 :size="16" /></IconButton></div>
   <p v-if="!settings.follows.length" class="media-empty">{{ copy('noFollows') }}</p>
   <Button data-media-add-follow @click="open('follow')">{{ copy('addFollow') }}</Button>
  </Fieldset>
  <Fieldset :collapsible="false" :title="copy('cookies')" :description="copy('cookiesHelp')">
   <div v-for="domain in cookies.domains" :key="domain.domain" class="media-row" data-media-cookie><span>{{ domain.domain }} · {{ domain.count }}</span><IconButton :label="copy('remove')" @click="remove('cookie',domain.domain)"><Trash2 :size="16" /></IconButton></div>
   <p v-if="!cookies.domains.length" class="media-empty">{{ copy('noCookies') }}</p>
   <p v-if="cookies.domains.length&&cookies.importedAt" class="media-empty">{{ copy('imported') }} {{ formatDateTime(cookies.importedAt) }}</p>
   <div class="media-actions"><Button data-media-import @click="open('cookies')">{{ copy('import') }}</Button><Button v-if="cookies.domains.length" data-media-clear-cookies @click="remove('cookie','')">{{ copy('deleteAll') }}</Button></div>
  </Fieldset>
  <Dialog :open="!!modal" size="md" :title="copy(modal==='rule'?'addRule':modal==='follow'?'addFollow':'import')" @update:open="$event||close()">
   <Form :id="formId" class="media-modal" field-width="full" @submit.prevent="submit">
    <template v-if="modal==='rule'">
     <FormItem :label="copy('match')"><Select v-model="ruleKind" :options="[{value:'host',label:copy('host')},{value:'channel',label:copy('channel')}]" :disabled="working" /></FormItem>
     <FormItem :label="copy(ruleKind==='host'?'host':'channel')"><Input v-model="ruleMatch" :disabled="working" :placeholder="ruleKind==='host'?'www.youtube.com':'https://www.youtube.com/@channel'" required /></FormItem>
     <FormItem :label="copy('folder')" :description="copy('folderHelp')"><div class="relative-folder"><span>{{ (path||'/home')+'/' }}</span><Input v-model="folder" :disabled="working" :invalid="!validSubfolder(folder)" placeholder="Videos/YouTube/Channel" data-rule-subfolder /></div><Select v-if="usedFolders.length" :model-value="''" :placeholder="appCopy(ctx.locale,'folders.used')" :options="usedFolders.map(p=>({value:p,label:p||'/'}))" :disabled="working" @update:model-value="folder=$event" data-used-subfolders /></FormItem>
     <MediaOptionsForm v-model="ruleOptions" :locale="ctx.locale" :disabled="working" />
    </template>
    <template v-else-if="modal==='follow'">
     <FormItem :label="copy('channel')"><Input v-model="followURL" type="url" :disabled="working" required /></FormItem>
     <FormItem :label="copy('folder')" :description="copy('folderHelp')"><div class="relative-folder"><span>{{ (path||'/home')+'/' }}</span><Input v-model="folder" :disabled="working" :invalid="!validSubfolder(folder)" placeholder="Videos/YouTube/Channel" data-rule-subfolder /></div><Select v-if="usedFolders.length" :model-value="''" :placeholder="appCopy(ctx.locale,'folders.used')" :options="usedFolders.map(p=>({value:p,label:p||'/'}))" :disabled="working" @update:model-value="folder=$event" data-used-subfolders /></FormItem>
     <FormItem :label="copy('interval')"><Input v-model="hours" type="number" min="2" max="168" :disabled="working" required /></FormItem>
     <Checkbox v-model="consent" :label="copy('followConsent')" :disabled="working" />
    </template>
    <template v-else-if="modal==='cookies'">
     <Callout tone="info" :close-label="common('details.close')">{{ copy('cookieImportHelp') }}</Callout>
     <FormItem :label="copy('cookieText')"><Textarea v-model="cookieText" :rows="7" :disabled="working" autocomplete="off" spellcheck="false" data-media-cookie-text /></FormItem>
     <Button :disabled="working" @click="fileInput?.click()">{{ copy('chooseFile') }}</Button><input ref="fileInput" type="file" accept=".txt,text/plain" hidden style="display:none" @change="readFile" />
    </template>
    <Callout v-if="modalError" tone="danger" :close-label="common('details.close')"><span role="alert">{{ modalError }}</span></Callout>
   </Form>
   <template #footer><Button :disabled="working" @click="close">{{ common('add.cancel') }}</Button><Button :form="formId" type="submit" variant="primary" :loading="working" :disabled="modal==='follow'&&!consent||modal==='cookies'&&!cookieText.trim()||modal!=='cookies'&&!validSubfolder(folder)">{{ modal==='cookies'?copy('import'):common('ui.settings.apply') }}</Button></template>
  </Dialog>
 </section>
</template>
<style scoped>
.relative-folder{display:flex;align-items:center;gap:var(--ui-space-2);min-width:0}.relative-folder>span{flex:none;max-width:45%;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.relative-folder>:last-child{min-width:0;flex:1;width:0}
.media-settings {display:grid;gap:var(--ui-space-5);min-width:0;}
.media-row {display:flex;align-items:center;justify-content:space-between;gap:var(--ui-space-2);padding-block:var(--ui-space-2);border-bottom:1px solid var(--ui-border-subtle);overflow-wrap:anywhere;min-width:0;}
.media-row>span {min-width:0;}
.media-row small {display:block;color:var(--ui-text-secondary);}
.media-empty {color:var(--ui-text-secondary);margin:var(--ui-space-2) 0;}
.media-actions {display:flex;gap:var(--ui-space-2);flex-wrap:wrap;}
@media(pointer:coarse){.media-row{min-height:var(--ui-hit-min,44px)}}
</style>
