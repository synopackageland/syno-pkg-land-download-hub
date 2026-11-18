<script setup lang="ts">
import {computed} from 'vue'
import {Badge,Link} from '@synopackageland/ui'
import {appCopy} from './app-copy'
import {useBrowserHelper} from './browser-helper'
// Ready RTO needs nothing from the user: only its Ready badge shows. Until it
// is ready the title and steps stay on the page, where they are the next action.
const props=defineProps<{locale:string}>()
const helper=useBrowserHelper()
const status=computed(()=>helper?.state.status??'not-installed')
const ready=computed(()=>status.value==='ready')
const copy=(key:string)=>appCopy(props.locale,'browser.'+key)
</script>
<template>
 <section data-browser-guide :data-open="ready?'false':'true'" :aria-label="copy('title')">
  <div class="guide-line">
   <span v-if="!ready" class="guide-title">{{ copy('title') }}</span>
   <Badge :tone="ready?'success':'warning'" data-browser-state :title="ready?copy('ready'):undefined">{{ copy(ready?'readyShort':status) }}</Badge>
   <span class="guide-actions"><slot name="actions" /></span>
  </div>
  <div v-if="!ready" data-browser-steps>
   <ol class="helper-steps">
    <li><svg viewBox="0 0 120 56" aria-hidden="true"><rect x="4" y="4" width="112" height="48" rx="4"/><path d="M4 16h112M16 10h2m6 0h2M52 24v20m-10-10h20"/></svg><strong>1 · {{ copy('extension') }}</strong><Link href="https://chromewebstore.google.com/detail/remote-tab-opener-manager/gmofalnpnpcannbeieijjgpbapafneho" target="_blank" rel="noopener noreferrer">Chrome / Edge</Link><Link href="https://addons.mozilla.org/firefox/addon/rto-remote-tab-opener/" target="_blank" rel="noopener noreferrer">Firefox</Link></li>
    <li><svg viewBox="0 0 120 56" aria-hidden="true"><rect x="4" y="4" width="112" height="48" rx="4"/><path d="M14 18h40m-40 12h55m-55 10h40"/><rect x="80" y="23" width="24" height="12" rx="6"/><circle cx="98" cy="29" r="4"/></svg><strong>2 · {{ copy('allow') }}</strong><span>{{ copy('allowHelp') }}</span></li>
    <li><svg viewBox="0 0 120 56" aria-hidden="true"><rect x="4" y="4" width="112" height="48" rx="4"/><path d="m45 28 10 10 22-22"/></svg><strong>3 · {{ copy('install') }}</strong><Link href="https://remote-tab-opener.com/documentation/allowlist.php" target="_blank" rel="noopener noreferrer" data-rto-help>{{ copy('installLink') }}</Link></li>
   </ol>
   <p class="footnote">{{ copy('privacy') }} <Link href="https://remote-tab-opener.com/documentation/" target="_blank" rel="noopener noreferrer">{{ copy('sites') }}</Link></p>
  </div>
  <p v-if="helper?.state.activity==='permission'" class="guide-note" role="status">{{ copy('permissionHelp') }}</p>
  <p v-else-if="helper?.state.activity==='delivering'" class="guide-note" data-browser-delivering role="status">{{ copy('delivering') }}</p>
  <p v-if="helper?.state.unclosed" class="guide-note" data-browser-unclosed role="status">{{ copy('unclosed').replace('{count}',String(helper.state.unclosed)) }}</p>
 </section>
</template>
<style scoped>
.guide-line{display:flex;flex-wrap:wrap;align-items:center;gap:var(--ui-space-2);min-height:28px}
.guide-title{color:var(--ui-text-secondary)}
.guide-actions{display:inline-flex;align-items:center;gap:var(--ui-space-2);margin-inline-start:auto}
.helper-steps{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:var(--ui-space-3);padding:var(--ui-space-3) 0;margin:0;list-style:none}
.helper-steps li{display:flex;flex-direction:column;align-items:flex-start;gap:var(--ui-space-1);min-width:0}
.helper-steps svg{height:56px;width:120px;fill:none;stroke:var(--ui-text-secondary);stroke-width:2}
.footnote,.guide-note{color:var(--ui-text-secondary);margin:0;overflow-wrap:anywhere}
@container (max-width:500px){.helper-steps{grid-template-columns:1fr}.helper-steps li{display:grid;grid-template-columns:80px 1fr}.helper-steps svg{grid-row:span 3;width:80px}}
</style>
