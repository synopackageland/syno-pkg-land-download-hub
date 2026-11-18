<script setup lang="ts">
import {computed} from 'vue'
import {Button,Callout} from '@synopackageland/ui'
import {appCopy} from './app-copy'
import {githubText,type GitHubFailure} from './github'
// A GitHub limit or a rejected token, with the one step that helps: the
// token field in Settings.
const props=defineProps<{failure:GitHubFailure;locale:string;admin?:boolean}>()
const emit=defineEmits<{token:[]}>()
const copy=(key:string)=>appCopy(props.locale,key)
const text=computed(()=>githubText(props.failure,props.locale))
</script>
<template>
 <Callout tone="warning" data-github-notice :title="text">
  <template v-if="admin" #actions><Button data-github-token-action @click="emit('token')">{{ copy(failure.kind==='invalid'?'github.replaceAction':'github.add') }}</Button></template>
 </Callout>
</template>
