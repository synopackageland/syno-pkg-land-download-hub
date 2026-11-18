<script setup lang="ts">
import { computed, onBeforeUnmount, ref, shallowRef } from 'vue'
import { Button, Dialog, EmptyState, LoadingArea, SegmentControl, SegmentItem, formatFileSize } from '@synopackageland/ui'
import { FileQuestion, FolderOpen } from '@lucide/vue'
import type { Context } from '@synopackageland/sdk'
import { appCopy } from './app-copy'
import { decodeText, fetchSaved, fileStationURL, htmlDocument, playable, previewLimits, previewType, withinLimit } from './preview'

// name/size: the saved file. path resolves its File Station path through the
// owner-bound reveal; token reads the user's DSM session token.
const props = defineProps<{ ctx: Context; name: string; size: number; path: () => Promise<string>; token: (signal: AbortSignal) => Promise<string>; fetcher?: typeof fetch; canPlay?: (mime: string) => string }>()
const emit = defineEmits<{ close: []; reveal: [] }>()
const copy = (key: string) => appCopy(props.ctx.locale, key)
const type = previewType(props.name)
const state = ref<'loading' | 'ready' | 'unsupported' | 'tooLarge' | 'failed'>('loading')
const failure = ref('')
const view = ref<'page' | 'source'>('page')
const blobURL = ref(''), mediaURL = ref(''), text = ref(''), truncated = ref(false), srcdoc = ref('')
const abort = new AbortController()
const ext = computed(() => (/\.([a-z0-9]{1,8})$/i.exec(props.name)?.[1] || '').toUpperCase())
const meta = computed(() => [ext.value, props.size > 0 ? formatFileSize(props.size) : ''].filter(Boolean).join(' · '))
const mediaEl = shallowRef<HTMLMediaElement>()
const canPlay = props.canPlay ?? ((mime: string) => document.createElement(type?.kind === 'audio' ? 'audio' : 'video').canPlayType(mime))

async function load() {
  if (!type) return (state.value = 'unsupported')
  if ((type.kind === 'video' || type.kind === 'audio') && !playable(type.mime, canPlay)) return (state.value = 'unsupported')
  if (!withinLimit(type.kind, props.size)) return (state.value = 'tooLarge')
  try {
    const [path, token] = await Promise.all([props.path(), props.token(abort.signal)])
    if (abort.signal.aborted) return
    if (type.kind === 'video' || type.kind === 'audio') {
      mediaURL.value = fileStationURL(path, token)
      state.value = 'ready'
      return
    }
    const max = previewLimits[type.kind as keyof typeof previewLimits]
    const got = await fetchSaved(path, token, type.kind === 'text' ? max : max + 1, abort.signal, props.fetcher)
    if (abort.signal.aborted) return
    if (type.kind !== 'text' && got.truncated) return (state.value = 'tooLarge')
    if (type.kind === 'text') {
      text.value = decodeText(got.bytes)
      truncated.value = got.truncated
    } else if (type.kind === 'html') {
      text.value = decodeText(got.bytes)
      srcdoc.value = htmlDocument(text.value)
    } else {
      blobURL.value = URL.createObjectURL(new Blob([got.bytes as BlobPart], { type: type.mime }))
    }
    state.value = 'ready'
  } catch (e: any) {
    if (abort.signal.aborted) return
    failure.value = copy('preview.' + (e?.code === 'preview_missing' ? 'missing' : e?.code === 'preview_denied' ? 'denied' : 'failed'))
    state.value = 'failed'
  }
}
void load()
function mediaFailed() {
  state.value = 'unsupported'
}
onBeforeUnmount(() => {
  abort.abort()
  if (mediaEl.value) {
    mediaEl.value.pause()
    mediaEl.value.removeAttribute('src')
    mediaEl.value.load()
  }
  if (blobURL.value) URL.revokeObjectURL(blobURL.value)
})
</script>

<template>
  <Dialog :open="true" :title="name" size="xl" :description="meta" data-preview @update:open="$event || emit('close')">
    <div class="gopeed-preview" :data-preview-kind="type?.kind" :data-preview-state="state">
      <div v-if="type?.kind === 'html' && state === 'ready'" class="gopeed-preview-tools">
        <SegmentControl v-model="view" :aria-label="copy('preview.view')">
          <SegmentItem value="page">{{ copy('preview.page') }}</SegmentItem>
          <SegmentItem value="source">{{ copy('preview.source') }}</SegmentItem>
        </SegmentControl>
      </div>
      <LoadingArea v-if="state === 'loading'" :text="copy('ui.loading')" role="status" />
      <template v-else-if="state === 'ready'">
        <img v-if="type?.kind === 'image'" class="gopeed-preview-image" :src="blobURL" :alt="name" />
        <!-- A saved page runs no script, has an opaque origin and loads nothing. -->
        <iframe v-else-if="type?.kind === 'html' && view === 'page'" class="gopeed-preview-frame" sandbox="" referrerpolicy="no-referrer" :srcdoc="srcdoc" :title="name" />
        <pre v-else-if="type?.kind === 'html'" class="gopeed-preview-text" data-preview-source>{{ text }}</pre>
        <!-- The browser's own PDF viewer does not run inside a sandboxed frame; the PDF is a Blob. -->
        <iframe v-else-if="type?.kind === 'pdf'" class="gopeed-preview-frame" :src="blobURL" :title="name" />
        <video v-else-if="type?.kind === 'video'" ref="mediaEl" class="gopeed-preview-media" controls preload="metadata" :src="mediaURL" @error="mediaFailed" />
        <audio v-else-if="type?.kind === 'audio'" ref="mediaEl" class="gopeed-preview-audio" controls preload="metadata" :src="mediaURL" @error="mediaFailed" />
        <template v-else-if="type?.kind === 'text'">
          <pre class="gopeed-preview-text" data-preview-text>{{ text }}</pre>
          <p v-if="truncated" class="gopeed-preview-note">{{ copy('preview.truncated') }}</p>
        </template>
      </template>
      <EmptyState v-else :icon="FileQuestion" :title="state === 'unsupported' ? copy('preview.unsupported') : state === 'tooLarge' ? copy('preview.tooLarge') : failure">
        <template #actions><Button @click="emit('reveal')"><FolderOpen :size="14" />{{ copy('ui.action.openInFileStation') }}</Button></template>
      </EmptyState>
    </div>
    <template #footer>
      <Button data-preview-reveal @click="emit('reveal')"><FolderOpen :size="14" />{{ copy('ui.action.openInFileStation') }}</Button>
    </template>
  </Dialog>
</template>
