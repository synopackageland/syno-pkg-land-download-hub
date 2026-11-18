<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Badge, Button, Callout, DataGrid, DropdownMenu, IconButton, PathDisplay, Progress, formatDateTime, formatDuration, formatFileSize, formatSpeed, fileTypeIcon } from '@synopackageland/ui'
import { Download, Eye, FolderOpen, Link2, MoreHorizontal, Pause, Play, RefreshCw, X } from '@lucide/vue'
import type { Context } from '@synopackageland/sdk'
import { appCopy } from './app-copy'
import { format as fill } from '@synopackageland/sdk'
import { barTone, copyableLink, createSampler, sparkline } from './downloads-view'
import { previewType } from './preview'
import type { ListRow } from './DownloadsList.vue'

// sheet: shown in a phone's bottom sheet, whose header closes it.
// task: the Downloads row the owner opened, refreshed with the list; view is
// its list row. stats reads the engine's counters while the panel is open.
const props = defineProps<{ ctx: Context; task: any; view: ListRow; method: string; host: string; tone: string; busy: boolean; stats: (id: string) => Promise<any>; focusFiles?: number; sheet?: boolean }>()
const emit = defineEmits<{ close: []; act: [op: string, file?: string] }>()
const copy = (key: string) => appCopy(props.ctx.locale, key)
const format = (key: string, values: Record<string, string | number>) => fill(copy(key), values, props.ctx.locale)
const size = (n: unknown) => formatFileSize(Number(n) > 0 ? Number(n) : undefined)
const when = (ms: unknown) => formatDateTime(ms as number | null, { locale: props.ctx.locale })
const group = computed(() => props.view.group)
const done = computed(() => group.value === 'done')
const mobileShell = props.ctx.host.platform().host === 'native-shell'
const singleSaved = computed(() => mobileShell && done.value && props.task.publication?.published && !(props.task.publication?.files?.length > 1))
const failed = computed(() => group.value === 'failed')
const media = computed(() => props.task.protocol === 'media')
const p2p = computed(() => props.task.protocol === 'bt' || props.task.protocol === 'ed2k')
const progress = computed(() => props.task.progress || {})
const link = computed(() => (done.value ? '' : copyableLink(props.task)))

// Saved files carry their own save state; other multi-file tasks list what the engine fetches.
const files = computed(() => {
  const saved = props.task.publication?.files
  if (saved?.length) return saved.map((f: any, i: number) => ({ key: String(i), name: f.rel || f.name, file: f.state === 'committed' ? f.name : '', size: f.size, state: fileState(f.state) }))
  const listed = props.task.meta?.res?.files || []
  return listed.length > 1 ? listed.map((f: any, i: number) => ({ key: String(i), name: [f.path, f.name].filter(Boolean).join('/'), file: '', size: f.size, state: '' })) : []
})
// A torrent's files can be chosen until its files are planned for saving.
const choosable = computed(() => {
  const t = props.task, p = t.publication
  return t.protocol === 'bt' && !p?.published && !p?.files?.length && !p?.deleting && t.status !== 'done' && files.value.length > 1
})
const engineChoice = () => {
  const chosen = props.task.meta?.opts?.selectFiles
  return (Array.isArray(chosen) && chosen.length ? chosen : files.value.map((_: unknown, i: number) => i)).map(String)
}
// The owner's ticks stay while the list refreshes; only Apply sends them.
const chosen = ref<string[]>(engineChoice()), sent = ref(engineChoice().join()), applying = ref(false), failure = ref('')
const changed = computed(() => [...chosen.value].sort((a, b) => +a - +b).join() !== sent.value)
async function apply() {
  const list = chosen.value.map(Number).sort((a, b) => a - b)
  applying.value = true
  failure.value = ''
  try {
    await props.ctx.call('downloads.select_files', { id: props.task.id, files: list })
    sent.value = list.join()
  } catch (e: any) {
    failure.value = copy('error.' + e?.code) || copy('error.unknown')
  } finally {
    applying.value = false
  }
}
const columns = computed(() => [
  { key: 'name', label: copy('ui.column.name'), flex: 1, minWidth: 140 },
  { key: 'size', label: copy('ui.column.size'), width: 80, align: 'right' as const },
  ...(props.task.publication?.files?.length ? [{ key: 'state', label: copy('ui.column.status'), width: 96 }] : []),
  ...(mobileShell && done.value ? [{ key: 'phone', label: '', width: 48 }] : []),
])
function fileState(state: string) {
  if (state === 'committed') return copy('ui.state.saved')
  if (state === 'uncertain') return copy('ui.state.saveUncertain')
  if (state === 'save_failed' || state === 'access_denied' || state === 'canceled') return copy('ui.state.saveFailed')
  return copy('ui.state.savePending')
}
const filesBox = ref<HTMLElement>()
watch(() => props.focusFiles, async (n) => {
  if (!n) return
  await Promise.resolve()
  filesBox.value?.scrollIntoView?.({ block: 'nearest' })
}, { immediate: true, flush: 'post' })

// Engine counters: connections for a direct download, peers for P2P. Read
// only while the panel is open on a task still moving.
const stats = ref<any>(null)
let timer: ReturnType<typeof setInterval> | undefined, alive = true
async function readStats() {
  try {
    const s = await props.stats(props.task.id)
    if (alive) stats.value = s
  } catch {
    if (alive) stats.value = null
  }
}
const moving = computed(() => !done.value && !failed.value && !media.value)
watch(() => [props.task.id, moving.value] as const, ([, on]) => {
  clearInterval(timer)
  stats.value = null
  if (on) {
    readStats()
    timer = setInterval(readStats, 4000)
  }
}, { immediate: true })
onBeforeUnmount(() => {
  alive = false
  clearInterval(timer)
})
const connections = computed(() => {
  const list: any[] = Array.isArray(stats.value?.connections) ? stats.value.connections : []
  return { total: list.length, completed: list.filter((c) => c.completed).length, failed: list.filter((c) => c.failed).length, retrying: list.filter((c) => !c.completed && !c.failed && c.retryTimes > 0).length }
})
const peerText = computed(() => (stats.value ? `${stats.value.activePeers ?? 0} / ${stats.value.totalPeers ?? 0}` : '—'))
const connectionText = computed(() => (stats.value && connections.value.total ? `${connections.value.total - connections.value.completed - connections.value.failed} / ${connections.value.total}` : '—'))
// One line about the connections; no per-segment picture.
const connectionLine = computed(() => {
  if (!stats.value) return ''
  if (p2p.value) return format('details.peerLine', { active: stats.value.activePeers ?? 0, total: stats.value.totalPeers ?? 0, seeders: stats.value.connectedSeeders ?? 0 })
  const c = connections.value
  return c.total ? format('details.connectionLine', { total: c.total, done: c.completed, retrying: c.retrying }) : ''
})

// The speed line, sampled from the list's own refreshes while the panel is open.
const sampler = createSampler()
const points = ref<{ t: number; v: number }[]>([])
watch(() => [props.task.id, props.task.progress?.speed, props.task.updatedAt] as const, ([id], old) => {
  if (old && old[0] !== id) sampler.clear()
  if (group.value !== 'active') return
  sampler.add(Date.now(), props.task.status === 'running' ? props.task.progress?.speed || 0 : 0)
  points.value = [...sampler.points]
}, { immediate: true })
const line = computed(() => sparkline(points.value, 300, 48))
const peak = computed(() => points.value.reduce((n, p) => Math.max(n, p.v), 0))

const used = computed(() => (progress.value.used > 0 ? formatDuration(Math.round(progress.value.used / 1e9), { locale: props.ctx.locale }) : '—'))
const ratio = computed(() => {
  const r = stats.value?.seedRatio
  if (typeof r === 'number') return r.toFixed(2)
  const down = progress.value.downloaded || 0
  return down > 0 ? ((progress.value.uploaded || 0) / down).toFixed(2) : '—'
})
const c = computed(() => props.view.controls)
const menu = computed(() => [
  ...(c.value.reveal && !done.value ? [{ key: 'reveal', label: copy('ui.action.openInFileStation'), icon: FolderOpen }] : []),
  ...(choosable.value ? [{ key: 'files', label: copy('details.chooseFiles') }] : []),
  ...(c.value.retrySave ? [{ key: 'retrySave', label: c.value.retrySave }] : []),
  ...(c.value.clear ? [{ type: 'separator' as const, key: 'sep' }, { key: 'clear', label: copy('downloads.remove'), disabled: !!c.value.clearing }] : []),
])
function pick(key: string) {
  if (key === 'files') filesBox.value?.scrollIntoView?.({ block: 'nearest' })
  else emit('act', key)
}
async function copyLink() {
  try {
    await navigator.clipboard.writeText(link.value)
    emit('act', 'copied')
  } catch {
    emit('act', 'copyFailed')
  }
}
const pctText = computed(() => (props.view.percent === null ? '—' : `${Math.floor(props.view.percent)}%`))
</script>

<template>
  <aside class="gopeed-panel" :class="{ 'is-sheet': sheet }" data-task-panel :aria-label="task.name">
    <header class="gopeed-panel-head">
      <span class="gopeed-dl-tile" :data-kind="view.kind" aria-hidden="true"><component :is="fileTypeIcon(task.name)" :size="16" /><b v-if="view.tile">{{ view.tile }}</b></span>
      <div class="gopeed-panel-title">
        <div v-if="!sheet" class="gopeed-panel-name" :title="task.name">{{ task.name }}</div>
        <div class="gopeed-panel-sub">{{ [host, method].filter(Boolean).join(' · ') }}</div>
      </div>
      <IconButton v-if="!sheet" size="sm" :label="copy('details.close')" @click="emit('close')"><X :size="15" /></IconButton>
    </header>
    <div class="gopeed-panel-body">
      <div v-if="!done" class="gopeed-panel-progress">
        <div class="gopeed-panel-big"><b data-panel-percent>{{ pctText }}</b><Badge :tone="tone as any">{{ task.statusText }}</Badge></div>
        <Progress size="sm" :tone="barTone[view.bar]" :value="view.percent ?? 0" :indeterminate="view.percent === null" :aria-label="task.name" />
        <div class="gopeed-panel-meta">{{ size(progress.downloaded) }} / {{ size(task.size) }}</div>
      </div>
      <div v-else class="gopeed-panel-big"><Badge :tone="tone as any">{{ task.statusText }}</Badge><span class="gopeed-panel-meta">{{ size(task.size) }}</span></div>
      <div v-if="failed && task.details" class="gopeed-panel-error" role="note">{{ task.details }}</div>
      <div v-if="!done && !failed" class="gopeed-panel-tiles" :data-tiles="media ? 2 : 3">
        <div class="gopeed-panel-tile"><span>{{ copy('ui.column.speed') }}</span><b>{{ task.speed === null ? '—' : formatSpeed(task.speed) }}</b></div>
        <div class="gopeed-panel-tile"><span>{{ copy('ui.column.remaining') }}</span><b>{{ task.remaining === null ? '—' : formatDuration(task.remaining, { locale: ctx.locale }) }}</b></div>
        <div v-if="!media" class="gopeed-panel-tile" :data-details-peers="p2p ? '' : undefined"><span>{{ copy(p2p ? 'details.peersShort' : 'details.connections') }}</span><b>{{ p2p ? peerText : connectionText }}</b></div>
      </div>
      <div v-if="group === 'active' && view.percent !== null && line" class="gopeed-panel-card" data-speed-chart>
        <div class="gopeed-panel-cardhead"><span>{{ copy('details.speedRecent') }}</span><span>{{ format('details.peak', { speed: formatSpeed(peak) }) }}</span></div>
        <svg viewBox="0 0 300 48" preserveAspectRatio="none" width="100%" height="48" aria-hidden="true"><polygon :points="`0,48 ${line} 300,48`" class="gopeed-spark-fill" /><polyline :points="line" class="gopeed-spark-line" /></svg>
      </div>
      <div v-if="connectionLine && !done && !failed" class="gopeed-panel-line" data-connection-line>{{ connectionLine }}</div>
      <dl class="gopeed-panel-kv">
        <dt>{{ copy('ui.column.size') }}</dt><dd>{{ size(task.size) }}</dd>
        <dt>{{ copy('ui.column.destination') }}</dt><dd><PathDisplay v-if="task.folder" :path="task.folder" /><span v-else>—</span></dd>
        <dt>{{ copy('downloads.methodLabel') }}</dt><dd>{{ method }}</dd>
        <dt>{{ copy('ui.column.started') }}</dt><dd>{{ when(task.started) }}</dd>
        <template v-if="task.completed"><dt>{{ copy('ui.column.completed') }}</dt><dd>{{ when(task.completed) }}</dd></template>
        <template v-if="progress.used > 0"><dt>{{ copy('details.timeUsed') }}</dt><dd>{{ used }}</dd></template>
        <template v-if="p2p">
          <dt>{{ copy('details.uploaded') }}</dt><dd>{{ size(progress.uploaded) }}</dd>
          <dt>{{ copy('details.ratio') }}</dt><dd>{{ ratio }}</dd>
          <template v-if="task.protocol === 'bt' && stats?.seedTime > 0"><dt>{{ copy('details.seedTime') }}</dt><dd>{{ formatDuration(stats.seedTime, { locale: ctx.locale }) }}</dd></template>
        </template>
        <template v-if="task.details && !failed"><dt>{{ copy('ui.column.details') }}</dt><dd data-panel-details>{{ task.details }}</dd></template>
      </dl>
      <div v-if="files.length" ref="filesBox" class="gopeed-panel-files" data-panel-files>
        <div class="gopeed-panel-cardhead"><span>{{ copy('details.files') }} ({{ files.length }})</span></div>
        <DataGrid v-if="choosable" v-model:selected-keys="chosen" selectable data-details-files :fill="false" :columns="columns" :rows="files" row-key="key" :page-sizes="[50, 100, 200]">
          <template #cell-size="{ row }">{{ size(row.size) }}</template>
        </DataGrid>
        <DataGrid v-else data-details-files :fill="false" :columns="columns" :rows="files" row-key="key" :page-sizes="[50, 100, 200]">
          <template #cell-phone="{ row }"><IconButton v-if="row.file" data-save-phone-file :label="copy('mobile.save')" :disabled="busy" @click="emit('act', 'savePhone', row.file as string)"><Download :size="14" /></IconButton></template>
          <template #cell-name="{ row }"><button v-if="row.file && previewType(row.file as string)" type="button" class="gopeed-link" :title="copy('preview.action')" @click="emit('act', 'previewFile', row.file as string)">{{ row.name }}</button><span v-else>{{ row.name }}</span></template>
          <template #cell-size="{ row }">{{ size(row.size) }}</template>
        </DataGrid>
        <Callout v-if="failure" tone="danger"><span role="alert">{{ failure }}</span></Callout>
        <div v-if="choosable" class="gopeed-panel-apply">
          <Button variant="primary" data-apply-files :disabled="applying || !chosen.length || !changed" @click="apply">{{ copy('details.applyFiles') }} ({{ chosen.length }})</Button>
        </div>
      </div>
    </div>
    <footer class="gopeed-panel-foot">
      <template v-if="done">
        <Button v-if="singleSaved" data-save-phone :disabled="busy" @click="emit('act', 'savePhone')"><Download :size="14" />{{ copy('mobile.save') }}</Button>
        <Button v-if="c.preview" variant="primary" :disabled="busy" @click="emit('act', 'preview')"><Eye :size="14" />{{ copy('preview.action') }}</Button>
        <Button v-if="c.reveal" :variant="c.preview ? 'secondary' : 'primary'" :disabled="busy" @click="emit('act', 'reveal')"><FolderOpen :size="14" />{{ copy('ui.action.openInFileStation') }}</Button>
      </template>
      <Button v-else-if="c.retry" variant="primary" :disabled="busy" @click="emit('act', 'continue')"><RefreshCw :size="14" />{{ copy('media.retry') }}</Button>
      <Button v-else-if="c.resume" variant="primary" :disabled="busy" @click="emit('act', 'continue')"><Play :size="14" />{{ copy('ui.action.resume') }}</Button>
      <Button v-else-if="c.pause" :disabled="busy" @click="emit('act', 'pause')"><Pause :size="14" />{{ copy('ui.action.pause') }}</Button>
      <Button v-if="link" data-copy-link @click="copyLink"><Link2 :size="14" />{{ copy('details.copyLink') }}</Button>
      <span class="gopeed-grow" />
      <DropdownMenu v-if="menu.length" :items="menu" align="end" @select="pick">
        <template #trigger><IconButton size="sm" :label="copy('details.more')"><MoreHorizontal :size="15" /></IconButton></template>
      </DropdownMenu>
    </footer>
  </aside>
</template>
