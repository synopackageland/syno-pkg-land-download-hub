<script setup lang="ts">
import { computed, ref } from 'vue'
import { Button, IconButton, Progress, fileTypeIcon } from '@synopackageland/ui'
import { Eye, FolderOpen, Pause, Play, RefreshCw, RotateCcw, Trash2 } from '@lucide/vue'
import { barTone, type DownloadGroup } from './downloads-view'

// One row of a Downloads tab's list, as App.vue computes it.
export interface ListRow {
  id: string
  name: string
  group: DownloadGroup
  // The status in words and its tone, read out with the row.
  status: string
  tone: string
  kind: string
  tile: string
  // 0–100, or null while the size is not known yet.
  percent: number | null
  bar: 'active' | 'paused' | 'failed' | 'none'
  meta: string[]
  // A failed row's reason, shown in red in place of the meta line.
  reason: string
  controls: { pause?: boolean; resume?: boolean; retry?: boolean; retrySave?: string; preview?: boolean; reveal?: boolean; clear: boolean; clearing?: boolean }
}
// phone: one primary action per row, a 44px tap box; the rest are in the
// task's details sheet.
const props = defineProps<{ rows: ListRow[]; selected: string; busy: boolean; copy: (key: string) => string; format: (key: string, values: Record<string, string | number>) => string; phone?: boolean }>()
const emit = defineEmits<{ open: [id: string]; act: [id: string, op: string] }>()
// A long list shows its first rows until the owner asks for all.
const limit = 100
const expanded = ref(false)
const shown = computed(() => (expanded.value ? props.rows : props.rows.slice(0, limit)))
type Op = { op: string; label: string; icon: any }
// A phone row's one action: what the row needs next.
function primary(r: ListRow): Op | undefined {
  const c = r.controls
  if (c.retry) return { op: 'continue', label: props.copy('media.retry'), icon: RefreshCw }
  if (c.retrySave) return { op: 'retrySave', label: c.retrySave, icon: RotateCcw }
  if (c.resume) return { op: 'continue', label: props.copy('ui.action.resume'), icon: Play }
  if (c.pause) return { op: 'pause', label: props.copy('ui.action.pause'), icon: Pause }
  if (c.preview) return { op: 'preview', label: props.copy('preview.action'), icon: Eye }
  if (c.reveal) return { op: 'reveal', label: props.copy('ui.action.openInFileStation'), icon: FolderOpen }
  return undefined
}
function key(e: KeyboardEvent, id: string) {
  if (e.target !== e.currentTarget) return
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    emit('open', id)
  }
}
</script>

<template>
  <div class="gopeed-dl-list" :class="{ 'is-phone': phone }" data-downloads-list>
      <div role="list" class="gopeed-dl-rows">
<div
          v-for="r in shown"
          :key="r.id"
          role="listitem"
          tabindex="0"
          class="gopeed-dl-row"
          :class="{ 'is-selected': r.id === selected }"
          :data-row-key="r.id"
          :data-group="r.group"
          :aria-current="r.id === selected ? 'true' : undefined"
          @click="emit('open', r.id)"
          @keydown="key($event, r.id)"
        >
          <span class="gopeed-dl-tile" :data-kind="r.kind" aria-hidden="true"><component :is="fileTypeIcon(r.name)" :size="16" /><b v-if="r.tile">{{ r.tile }}</b></span>
          <div class="gopeed-dl-main">
            <div class="gopeed-dl-name" :title="r.name">{{ r.name }}<span class="gopeed-sr" data-row-status :data-tone="r.tone">{{ r.status }}</span></div>
            <Progress v-if="r.bar !== 'none'" size="sm" :tone="barTone[r.bar]" :value="r.percent ?? 0" :indeterminate="r.percent === null && r.group === 'active'" :aria-label="r.name" />
            <div v-if="r.reason" class="gopeed-dl-meta is-failed" :title="r.reason">{{ r.reason }}</div>
            <div v-else class="gopeed-dl-meta" :title="r.meta.join(' · ')"><template v-for="(m, i) in r.meta" :key="i"><span v-if="i" class="gopeed-dl-dot" aria-hidden="true">·</span><span>{{ m }}</span></template></div>
          </div>
          <div v-if="phone" class="gopeed-dl-actions" @click.stop>
            <IconButton v-if="primary(r)" :disabled="busy" :label="`${primary(r)!.label} ${r.name}`" :data-row-action="primary(r)!.op" @click="emit('act', r.id, primary(r)!.op)"><component :is="primary(r)!.icon" :size="18" /></IconButton>
          </div>
          <div v-else class="gopeed-dl-actions" :class="{ 'is-pinned': r.controls.preview || r.controls.retry || r.controls.retrySave }" @click.stop>
            <Button v-if="r.controls.preview" size="sm" :aria-label="`${copy('preview.action')} ${r.name}`" :disabled="busy" @click="emit('act', r.id, 'preview')"><Eye :size="14" />{{ copy('preview.action') }}</Button>
            <IconButton v-if="r.controls.reveal" size="sm" :disabled="busy" :label="copy('ui.action.openInFileStation')" @click="emit('act', r.id, 'reveal')"><FolderOpen :size="15" /></IconButton>
            <Button v-if="r.controls.retrySave" size="sm" :aria-label="r.controls.retrySave" :disabled="busy" @click="emit('act', r.id, 'retrySave')"><RotateCcw :size="14" />{{ r.controls.retrySave }}</Button>
            <IconButton v-if="r.controls.pause" size="sm" :disabled="busy" :label="copy('ui.action.pause')" @click="emit('act', r.id, 'pause')"><Pause :size="15" /></IconButton>
            <IconButton v-if="r.controls.resume" size="sm" :disabled="busy" :label="copy('ui.action.resume')" @click="emit('act', r.id, 'continue')"><Play :size="15" /></IconButton>
            <Button v-if="r.controls.retry" size="sm" :aria-label="copy('media.retry')" :disabled="busy" @click="emit('act', r.id, 'continue')"><RefreshCw :size="14" />{{ copy('media.retry') }}</Button>
            <IconButton v-if="r.controls.clear" size="sm" class="gopeed-dl-clear" :disabled="busy || r.controls.clearing" :label="copy('ui.action.clearRecord')" @click="emit('act', r.id, 'clear')"><Trash2 :size="15" /></IconButton>
          </div>
        </div>
        <div v-if="rows.length > limit && !expanded" class="gopeed-dl-more">
          <Button size="sm" variant="ghost" @click="expanded = true">{{ format('downloads.showAll', { count: rows.length }) }}</Button>
        </div>
      </div>
  </div>
</template>
