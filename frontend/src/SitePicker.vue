<script setup lang="ts">
import {computed,ref} from 'vue'
import {Badge,Button,DataGrid,Dialog,SearchField,Select} from '@synopackageland/ui'
import {format,type Context} from '@synopackageland/sdk'
import {appCopy} from './app-copy'
import {siteStatus,siteTone,type CatalogSite} from './search'
// The Add site picker: the site list in one scrolling grid. Sites already
// turned on read "Added"; sites Download Hub cannot use stay out of the way
// until asked for, and then show why.
const props=defineProps<{ctx:Context;sites:CatalogSite[];added:Set<string>;busy:Record<string,boolean>;reasonText:(reason:string)=>string}>()
const emit=defineEmits<{close:[];enable:[site:CatalogSite]}>()
const copy=(key:string)=>appCopy(props.ctx.locale,key)
const filter=ref(''),type=ref('all'),showUnavailable=ref(false)
const types=computed(()=>['all','public','semi-private','private'].map(value=>({value,label:copy('sources.type.'+value)})))
const unavailable=computed(()=>props.sites.filter(x=>!x.supported).length)
const order=['public','semi-private','private']
const rows=computed(()=>props.sites
 .filter(x=>(showUnavailable.value||x.supported)&&(type.value==='all'||x.type===type.value))
 .map(x=>({...x,status:siteStatus(x),typeText:copy('sources.type.'+x.type)||x.type,reasonText:x.reasons.map(props.reasonText).join(' ')}))
 .sort((a,b)=>Number(b.supported)-Number(a.supported)||order.indexOf(a.type)-order.indexOf(b.type)||a.name.localeCompare(b.name)))
const columns=computed(()=>[
 {key:'name',label:copy('sources.column.site'),sortable:true,flex:2,minWidth:150,hideable:false},
 {key:'typeText',label:copy('search.column.type'),sortable:true,width:110,priority:2,searchable:false},
 {key:'status',label:copy('ui.column.status'),flex:1,minWidth:130,priority:1,searchable:false},
 {key:'actions',label:copy('ui.column.actions'),width:110,hideable:false,searchable:false,resizable:false},
])
</script>
<template>
 <Dialog :open="true" :title="copy('sources.addSite')" :description="copy('sources.addSiteHelp')" size="lg" data-site-picker @update:open="$event||emit('close')">
  <div class="site-picker">
   <div class="site-picker-tools">
    <SearchField v-model="filter" class="site-picker-filter" data-catalog-filter variant="filter" :placeholder="copy('sources.findSite')" />
    <div class="site-picker-type"><Select v-model="type" :options="types" :aria-label="copy('search.column.type')" /></div>
   </div>
   <DataGrid data-catalog :columns="columns" :rows="rows" row-key="id" :filter="filter" :page-sizes="[50,100,200]" max-height="min(56vh,460px)" :fill="false" :labels="{empty:copy('sources.noSites'),noMatch:copy('sources.noSites')}">
    <template #cell-name="{row}"><span :class="{'site-off':!row.supported}">{{ row.name }}</span></template>
    <template #cell-status="{row}">
     <Badge :tone="siteTone[row.status as keyof typeof siteTone]" :title="row.supported?copy('sources.statusHelp.'+row.status):(row.reasonText as string)" :data-site-status="row.status">{{ copy('sources.status.'+row.status) }}</Badge>
     <span v-if="!row.supported" class="site-reason">{{ row.reasonText }}</span>
    </template>
    <template #cell-actions="{row}">
     <Badge v-if="added.has(row.id as string)" tone="success">{{ copy('search.added') }}</Badge>
     <Button v-else size="sm" variant="secondary" :disabled="!row.supported||!!busy[row.id as string]" @click="emit('enable',row as unknown as CatalogSite)">{{ copy('sources.turnOn') }}</Button>
    </template>
   </DataGrid>
   <div v-if="unavailable" class="site-picker-more">
    <Button size="sm" variant="ghost" data-show-unavailable @click="showUnavailable=!showUnavailable">{{ showUnavailable?copy('sources.hideUnavailable'):format(copy('sources.showUnavailable'),{count:unavailable},ctx.locale) }}</Button>
   </div>
  </div>
  <template #footer><Button @click="emit('close')">{{ copy('details.close') }}</Button></template>
 </Dialog>
</template>
<style scoped>
.site-picker {display:flex;min-height:0;flex-direction:column;gap:var(--ui-space-2);}
.site-picker-tools {display:flex;flex-wrap:wrap;align-items:center;gap:var(--ui-space-2);}
.site-picker-filter {flex:1 1 220px;min-width:0;}
.site-picker-type {flex:0 0 160px;}
.site-off {color:var(--ui-text-secondary);}
.site-reason {display:block;overflow:hidden;color:var(--ui-text-secondary);font-size:var(--ui-font-size-sm);text-overflow:ellipsis;white-space:nowrap;}
.site-picker-more {display:flex;justify-content:flex-start;}
</style>
