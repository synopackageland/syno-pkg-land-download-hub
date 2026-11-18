<script setup lang="ts">
import {computed} from 'vue'
import {Checkbox,FormItem,Input,Select} from '@synopackageland/ui'
import {appCopy} from './app-copy'
import type {MediaOptions} from './media-options'
const props=defineProps<{modelValue:MediaOptions;locale:string;disabled?:boolean}>()
const emit=defineEmits<{'update:modelValue':[value:MediaOptions]}>()
const copy=(key:string)=>appCopy(props.locale,'mediaOptions.'+key)
function update(key:keyof MediaOptions,value:any){const next={...props.modelValue,[key]:value};if(key==='format')next.audioFormat=value==='audio'?'m4a':'';if(key==='subtitles'&&value!=='off'&&!next.subtitleLanguages?.length)next.subtitleLanguages=['en'];emit('update:modelValue',next)}
const languages=computed(()=>props.modelValue.subtitleLanguages?.join(', ')??'')
const qualities=computed(()=>[{value:'best',label:copy('best')},...['2160','1440','1080','720','480','360'].map(value=>({value,label:value+'p'})),{value:'audio',label:copy('audio')}])
const subtitles=computed(()=>['off','embed','srt','vtt'].map(value=>({value,label:copy('sub.'+value)})))
const sponsors=computed(()=>['off','mark','remove'].map(value=>({value,label:copy('sponsor.'+value)})))
const toggles=['embedThumbnail','embedMetadata','splitChapters','nfo'] as const
</script>
<template>
 <div class="media-options" data-media-options>
  <FormItem :label="copy('quality')"><Select :model-value="modelValue.format" :disabled="disabled" :options="qualities" @update:model-value="update('format',$event)" /></FormItem>
  <FormItem v-if="modelValue.format==='audio'" :label="copy('audioFormat')"><Select :model-value="modelValue.audioFormat||'m4a'" :disabled="disabled" :options="['mp3','m4a','opus','flac'].map(value=>({value,label:value.toUpperCase()}))" @update:model-value="update('audioFormat',$event)" /></FormItem>
  <FormItem :label="copy('subtitles')"><Select :model-value="modelValue.subtitles||'off'" :disabled="disabled" :options="subtitles" @update:model-value="update('subtitles',$event)" /></FormItem>
  <FormItem v-if="modelValue.subtitles&&modelValue.subtitles!=='off'" :label="copy('languages')" :description="copy('languagesHelp')"><Input :model-value="languages" :disabled="disabled" placeholder="en, zh-TW" @update:model-value="update('subtitleLanguages',String($event).split(',').map(s=>s.trim()).filter(Boolean))" /></FormItem>
  <FormItem :label="copy('sponsorBlock')"><Select :model-value="modelValue.sponsorBlock||'off'" :disabled="disabled" :options="sponsors" @update:model-value="update('sponsorBlock',$event)" /></FormItem>
  <FormItem v-for="key in toggles" :key="key" :label="false"><Checkbox :model-value="!!modelValue[key]" :disabled="disabled||(key==='nfo'&&modelValue.format==='audio')" :label="copy(key)" @update:model-value="update(key,$event)" /></FormItem>
 </div>
</template>
<style scoped>
.media-options {display:grid;gap:var(--ui-space-2);min-width:0;}
</style>
