<script setup lang="ts">
import {computed,ref} from 'vue'
import {Button,PathDisplay,Space} from '@synopackageland/ui'
import {appCopy} from './app-copy'
import {activePath,destinationCopy,type DownloadLocation} from './destination'
import DownloadFolderDialog from './DownloadFolderDialog.vue'
import type {Context} from '@synopackageland/sdk'
const props=defineProps<{ctx:Context;destination?:DownloadLocation|null;busy?:boolean}>()
const emit=defineEmits<{authorized:[]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key),places=computed(()=>destinationCopy(props.ctx.locale))
const open=ref(false),path=computed(()=>activePath(props.destination))
</script>
<template>
 <Space class="add-location" wrap spacing="compact" data-download-location>
  <span>{{ copy('add.location') }}</span>
  <PathDisplay v-if="path" :path="path" />
  <span v-else role="status">{{ destination===undefined?places.loading:destination===null?places.error:places[destination.status as keyof typeof places] }}</span>
  <Button size="sm" :disabled="busy||destination===undefined||!destination?.selection||destination.status==='disabled'||destination.status==='needs-scope-confirmation'" @click="open=true">{{ copy(path?'add.change':'add.reselect') }}</Button>
 </Space>
 <DownloadFolderDialog :ctx="ctx" :destination="destination" :open="open" @close="open=false" @changed="emit('authorized')" @saved="open=false;emit('authorized')" />
</template>
<style scoped>.add-location{min-width:0;min-height:var(--ui-control-sm);padding-block:var(--ui-space-2)}.add-location :deep([data-ui-path-display]){flex:1;min-width:0}</style>
