<script setup lang="ts">
import {computed,onBeforeUnmount,ref,useId,watch} from 'vue'
import {Button,Callout,Dialog,Form,FormItem,Input} from '@synopackageland/ui'
import {NasPicker,scopedPicker} from '@synopackageland/sdk/vue'
import type {Context} from '@synopackageland/sdk'
import {destinationCopy,createDestinationAdapter,type DownloadLocation} from './destination'
import {appCopy} from './app-copy'
import {checkHome,openUserHome,type HomeStatus} from './home-status'
import {validSubfolder} from './subfolder'
const props=defineProps<{ctx:Context;open:boolean;destination?:DownloadLocation|null}>()
const emit=defineEmits<{close:[];saved:[state:DownloadLocation];changed:[state:DownloadLocation]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,'folders.'+key)
const formId='download-folder-'+useId(),subfolder=ref('Downloads'),error=ref(''),busy=ref(false),picking=ref(false),home=ref<HomeStatus>('unknown'),checking=ref(false),dismissed=ref(false)
const adapter=createDestinationAdapter(props.ctx)
const localLocation=ref<DownloadLocation>()
const current=computed(()=>localLocation.value??props.destination)
const root=computed(()=>current.value?.destination?.path??'/home')
const statusText=computed(()=>{const places=destinationCopy(props.ctx.locale);return places[current.value?.status as keyof typeof places]||copy('refused')})
const first=computed(()=>current.value?.status==='missing')
const homeAllowed=computed(()=>current.value?.selection?.scope==='home'&&current.value.selection.root==='/home')
const usesHome=computed(()=>root.value==='/home'||root.value.startsWith('/home/'))
const canSet=computed(()=>(current.value?.status==='active'||first.value&&homeAllowed.value)&&(!usesHome.value||home.value!=='disabled'))
const provider=computed(()=>current.value?.selection?scopedPicker(adapter.provider,current.value.selection.root):undefined)
async function retry(){checking.value=true;dismissed.value=false;try{home.value=await checkHome(props.ctx,true)}finally{checking.value=false}}
watch(()=>props.open,async open=>{if(!open)return;localLocation.value=undefined;subfolder.value=props.destination?.subfolder??(first.value?'Downloads':'');error.value='';dismissed.value=false;if(usesHome.value&&homeAllowed.value){checking.value=true;try{home.value=await checkHome(props.ctx)}finally{checking.value=false}}})
async function save(){
 if(!validSubfolder(subfolder.value)){error.value=copy('invalid');return}
 busy.value=true;error.value=''
 try{emit('saved',await props.ctx.call<DownloadLocation>('downloads.folder.set',{subfolder:subfolder.value}))}
 catch(e:any){home.value=await checkHome(props.ctx,true);if(first.value&&e?.code==='not-found')home.value='disabled';error.value=home.value==='disabled'?'':e?.code==='APP_DENIED'?appCopy(props.ctx.locale,'destination.appDenied'):copy(e?.code==='home_scope_required'?'scope':'refused')}
 finally{busy.value=false}
}
async function selected(){
 picking.value=false
 try{const state=await props.ctx.call<DownloadLocation>('downloads.location',{});localLocation.value=state;emit('changed',state)}catch{error.value=copy('refused')}
}
function refused(e:any){picking.value=false;error.value=e?.code==='APP_DENIED'?appCopy(props.ctx.locale,'destination.appDenied'):copy('refused')}
onBeforeUnmount(()=>adapter.provider.dispose())
</script>
<template>
 <Dialog :open="open" size="sm" :title="copy('title')" :description="copy('help')" data-download-folder-dialog @update:open="!$event&&!busy&&emit('close')">
  <Form :id="formId" @submit.prevent="save">
   <Callout v-if="usesHome&&homeAllowed&&home==='disabled'&&!dismissed" tone="warning" :close-label="copy('dismiss')" @close="dismissed=true" data-home-off>
    {{ copy(ctx.user?.isAdmin?'homeOffAdmin':'homeOffUser') }}
    <template #actions><Button v-if="ctx.user?.isAdmin" @click="openUserHome(ctx)">{{ copy('controlPanel') }}</Button><Button :loading="checking" @click="retry">{{ copy('retry') }}</Button></template>
   </Callout>
   <Callout v-if="!first&&current?.status!=='active'" tone="warning">{{ statusText }}</Callout>
   <Callout v-if="first&&!homeAllowed" tone="warning">{{ copy('scope') }}</Callout>
   <FormItem v-if="canSet" :label="copy('subfolder')">
    <div class="relative-folder"><span :title="root+'/'">{{ root+'/' }}</span><Input v-model="subfolder" :disabled="busy||checking" :invalid="!validSubfolder(subfolder)" placeholder="Downloads" data-download-subfolder /></div>
   </FormItem>
   <p v-if="checking" role="status">{{ copy('checking') }}</p>
   <Callout v-if="error" tone="warning">{{ error }}<template #actions><Button :loading="checking" @click="retry">{{ copy('retry') }}</Button></template></Callout>
   <Button :disabled="busy||!provider||current?.status==='disabled'" @click="picking=true" data-another-folder>{{ copy('another') }}</Button>
  </Form>
  <template #footer><Button :disabled="busy" @click="emit('close')">{{ copy('cancel') }}</Button><Button v-if="canSet" :form="formId" type="submit" variant="primary" :loading="busy" :disabled="checking||!validSubfolder(subfolder)" data-confirm-download-folder>{{ copy('confirm') }}</Button></template>
 </Dialog>
 <NasPicker v-if="provider" v-model:open="picking" :locale="ctx.locale" :provider="provider" mode="folder" :destination="{key:'save',selectDestination:adapter.selectDestination}" @authorized="selected" @refused="refused" />
</template>
<style scoped>
.relative-folder{display:flex;align-items:center;gap:var(--ui-space-2);min-width:0}.relative-folder>span{flex:none;max-width:45%;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.relative-folder>:last-child{flex:1;min-width:0;width:0}
</style>
