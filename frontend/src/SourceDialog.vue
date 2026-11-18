<script setup lang="ts">
import {computed,reactive,ref,useId,watch} from 'vue'
import {Button,Callout,Dialog,FilePicker,Form,FormItem,FormNote,Input,SectionTabs,Select,Textarea} from '@synopackageland/ui'
import type {Context} from '@synopackageland/sdk'
import {appCopy} from './app-copy'
import {secretSetting,sourceFields,type CatalogSite,type SourceView} from './search'
// source: the one being edited; site: a catalog site to turn on; neither
// adds a new source of the kind picked here, a built-in site from `sites`
// (the usable ones not yet turned on) unless another kind is picked.
// kinds: the kinds offered for a new source, in tab order (all by default).
const props=defineProps<{ctx:Context;reason:(e:any)=>string;source?:SourceView;site?:CatalogSite;sites?:CatalogSite[];kinds?:string[]}>()
const emit=defineEmits<{close:[];saved:[source:SourceView];refused:[error:any]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const formId='source-'+useId()
const kinds=props.kinds??['builtin','cardigann','torznab','rss','ytsearch','bilisearch']
const kindTabs=computed(()=>kinds.map(value=>({value,label:copy('sources.kind.'+value)})))
const kind=ref(props.source?.kind??kinds[0])
const name=ref(props.source?.name??props.site?.name??'')
const url=ref(props.source?.url??''),yaml=ref(''),files=ref<File[]>([]),busy=ref(false),error=ref('')
const values=reactive<Record<string,string>>({})
const editing=!!props.source
// The site being turned on: given, or picked from the list.
const picked=ref(''),picking=computed(()=>kind.value==='builtin'&&!editing&&!props.site)
const chosen=computed(()=>props.site??props.sites?.find(x=>x.id===picked.value))
const siteOptions=computed(()=>(props.sites??[]).map(x=>({label:x.name+' · '+(copy('sources.type.'+x.type)||x.type),value:x.id})))
watch(picked,()=>{name.value=chosen.value?.name??'';for(const k of Object.keys(values))delete values[k]})
const fields=computed(()=>sourceFields(kind.value,props.source?.settings??chosen.value?.settings??[],copy).filter(f=>!(props.source?.cloudflare||chosen.value?.cloudflare)||!['cookie','useragent'].includes(f.name)))
const needsUrl=computed(()=>kind.value==='torznab'||kind.value==='rss')
// A new import needs its YAML; an edit keeps the old one when left empty.
const ready=computed(()=>!busy.value&&!!name.value.trim()&&(!picking.value||!!chosen.value)&&(!needsUrl.value||!!url.value.trim())&&(kind.value!=='cardigann'||editing||!!yaml.value.trim()))
const title=computed(()=>copy(editing?'sources.edit':props.site?'sources.enable':'sources.add'))

async function pick(list:File[]){
 files.value=list
 const file=list[0];if(!file)return
 yaml.value=await file.text()
}
async function save(){
 if(!ready.value)return
 busy.value=true;error.value=''
 const filled=Object.fromEntries(Object.entries(values).filter(([,v])=>v!==''))
 const input:Record<string,unknown>={...(props.source?{id:props.source.id}:{}),kind:kind.value,name:name.value.trim(),enabled:props.source?.enabled??true}
 if(kind.value==='builtin')input.definitionId=props.source?.definitionId??chosen.value?.id
 if(needsUrl.value&&url.value.trim()!==(props.source?.url??''))input.url=url.value.trim()
 if(kind.value==='cardigann'&&yaml.value.trim())input.yaml=yaml.value
 if(Object.keys(filled).length)input.values=filled
 try{emit('saved',await props.ctx.call('search.sources.save',input))}
 catch(e){emit('refused',e);error.value=props.reason(e)}
 finally{busy.value=false}
}
</script>
<template>
 <Dialog :open="true" :title="title" size="md" @update:open="$event||emit('close')">
  <Form :id="formId" direction="vertical" @submit.prevent="save">
   <SectionTabs v-if="!editing&&!site" v-model="kind" :items="kindTabs" :aria-label="copy('sources.kind')" />
   <FormItem v-if="picking" :label="copy('sources.site')">
    <Select v-model="picked" search :options="siteOptions" :aria-label="copy('sources.site')" :placeholder="copy('sources.sitePlaceholder')" />
   </FormItem>
   <FormItem v-if="!picking" :label="copy('sources.name')"><Input v-model="name" :aria-label="copy('sources.name')" maxlength="120" required /></FormItem>
   <FormItem v-if="needsUrl" :label="copy('sources.url')"><Input v-model="url" :aria-label="copy('sources.url')" type="url" required spellcheck="false" /></FormItem>
   <template v-if="kind==='cardigann'">
    <FormItem :label="copy('sources.yaml')" :description="copy(editing?'sources.yamlKeep':'sources.yamlHelp')">
     <Textarea v-model="yaml" :rows="8" :aria-label="copy('sources.yaml')" spellcheck="false" />
    </FormItem>
    <FilePicker :model-value="files" variant="compact" accept=".yml,.yaml" :locale="ctx.locale as any" @update:model-value="pick" />
   </template>
   <FormItem v-for="f in fields" :key="f.name" :label="f.label">
    <Input v-model="values[f.name]" :aria-label="f.label" :type="secretSetting(f)?'password':'text'" autocomplete="off" :placeholder="secretSetting(f)&&source?.configured?copy('sources.secretSet'):undefined" />
   </FormItem>
   <FormNote v-if="fields.some(secretSetting)">{{ copy('sources.secretHelp') }}</FormNote>
   <Callout v-if="error" tone="danger"><span role="alert">{{ error }}</span></Callout>
  </Form>
  <template #footer>
   <Button @click="emit('close')">{{ copy('add.cancel') }}</Button>
   <Button :form="formId" type="submit" variant="primary" :disabled="!ready">{{ copy('sources.save') }}</Button>
  </template>
 </Dialog>
</template>
