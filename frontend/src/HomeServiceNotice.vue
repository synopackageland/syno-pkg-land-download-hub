<script setup lang="ts">
import {onMounted,ref} from 'vue'
import {Button,Callout} from '@synopackageland/ui'
import type {Context} from '@synopackageland/sdk'
import {checkHome,openUserHome} from './home-status'
import {appCopy} from './app-copy'
const props=defineProps<{ctx:Context}>(),emit=defineEmits<{retry:[]}>()
const off=ref(false),busy=ref(false),dismissed=ref(false)
const copy=(key:string)=>appCopy(props.ctx.locale,'folders.'+key)
onMounted(async()=>{off.value=await checkHome(props.ctx)==='disabled'})
async function retry(){busy.value=true;try{off.value=await checkHome(props.ctx,true)==='disabled';emit('retry')}finally{busy.value=false}}
</script>
<template>
 <Callout v-if="off&&!dismissed" tone="warning" :close-label="copy('dismiss')" @close="dismissed=true" data-home-service-notice>
  {{ copy(ctx.user?.isAdmin?'homeOffAdmin':'homeOffUser') }}
  <template #actions><Button v-if="ctx.user?.isAdmin" @click="openUserHome(ctx)">{{ copy('controlPanel') }}</Button><Button :loading="busy" @click="retry">{{ copy('retry') }}</Button></template>
 </Callout>
</template>
