<script setup lang="ts">
import {nextTick,onMounted,ref,watch} from 'vue'
import {Button,Callout,Form,FormActions,FormItem,Input,Link,SettingsSection,Space,useToast} from '@synopackageland/ui'
import {format,type Context} from '@synopackageland/sdk'
import {appCopy} from './app-copy'
import {GITHUB_TOKEN_URL} from './github'
// The administrator's optional GitHub token. It is checked with GitHub once
// when saved; the page only ever sees its last four characters.
const props=defineProps<{ctx:Context;focus?:number}>()
const emit=defineEmits<{refused:[e:unknown]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const toast=useToast()
type Status={saved:boolean;last4?:string;limit?:number}
const status=ref<Status>(),token=ref(''),editing=ref(false),busy=ref(false),error=ref(''),limit=ref<number>()
const section=ref<HTMLElement>()
async function read(){
 try{status.value=await props.ctx.call<Status>('gopeed.github.status',{})}
 catch(e){emit('refused',e)}
}
function failure(e:any){
 const code=e?.code
 return copy(code==='github_token_invalid'?'github.invalid':code==='github_token_format'?'github.format':code==='github_unavailable'?'github.unavailable':'github.saveFailed')
}
async function save(){
 if(busy.value||!token.value.trim())return
 busy.value=true;error.value=''
 try{
  const out=await props.ctx.call<Status>('gopeed.github.set',{token:token.value.trim()})
  status.value={saved:out.saved,last4:out.last4};limit.value=out.limit;token.value='';editing.value=false
  toast.success(copy('github.saved'))
 }catch(e){emit('refused',e);error.value=failure(e)}
 finally{busy.value=false}
}
async function remove(){
 if(busy.value)return
 busy.value=true;error.value=''
 try{status.value=await props.ctx.call<Status>('gopeed.github.remove',{});limit.value=undefined;editing.value=false}
 catch(e){emit('refused',e);error.value=failure(e)}
 finally{busy.value=false}
}
function replace(){editing.value=true;error.value='';void focusField()}
async function focusField(){
 await nextTick()
 section.value?.scrollIntoView?.({block:'center'})
 section.value?.querySelector<HTMLInputElement>('input')?.focus()
}
// "Add a GitHub token" elsewhere opens Settings here, at the field.
watch(()=>props.focus,n=>{if(n){if(status.value?.saved)editing.value=true;void focusField()}})
onMounted(async()=>{await read();if(props.focus){if(status.value?.saved)editing.value=true;void focusField()}})
</script>
<template>
 <div ref="section" id="github-token">
 <SettingsSection data-section="github" :title="copy('github.title')" :description="copy('github.help')" :loading="!status">
  <Form direction="horizontal" @submit.prevent="save">
   <FormItem :label="copy('github.token')">
    <Space direction="column" spacing="compact">
     <Space v-if="status?.saved&&!editing" spacing="compact" align-items="center" wrap>
      <span data-github-saved>{{ format(copy('github.savedAs'),{last4:status.last4||''},ctx.locale) }}</span>
      <Button :disabled="busy" @click="replace">{{ copy('github.replace') }}</Button>
      <Button :disabled="busy" @click="remove">{{ copy('github.remove') }}</Button>
     </Space>
     <Input v-else v-model="token" data-github-token type="password" autocomplete="off" spellcheck="false" :disabled="busy" :placeholder="'github_pat_…'" />
     <span v-if="limit" data-github-limit>{{ format(copy('github.limit'),{limit:limit.toLocaleString()},ctx.locale) }}</span>
     <Link :href="GITHUB_TOKEN_URL" external>{{ copy('github.create') }}</Link>
    </Space>
   </FormItem>
   <Callout v-if="error" tone="danger" :title="error" />
   <FormActions v-if="!status?.saved||editing" inset>
    <Button v-if="editing" :disabled="busy" @click="editing=false;token='';error=''">{{ copy('github.cancel') }}</Button>
    <Button variant="primary" type="submit" :disabled="busy||!token.trim()">{{ copy('github.save') }}</Button>
   </FormActions>
  </Form>
 </SettingsSection>
 </div>
</template>
