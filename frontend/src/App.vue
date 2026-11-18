<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, provide, reactive, ref, useId, watch } from "vue";
import {
  formatFileSize,
  formatSpeed,
  formatPercent,
  formatDuration,
  formatDateTime,
  formatRelativeTime,
  Button,
  IconButton,
  Space,
  CellText,
  CellStatus,
  PathDisplay,
  LoadingArea,
  ListItem,
  MessageBoxProvider,
  Input,
  InputGroup,
  Checkbox,
  Dialog,
  DataGrid,
  SearchField,
  EmptyState,
  Callout,
  ToastProvider,
  SectionTabs,
  Select,
  Badge,
  Card,
  SettingsSection,
  SettingsRow,
  KeyValueRow,
  Form,
  FormItem,
  Fieldset,
  SettingsPage,
  Link,
  Textarea,
  BottomSheet,
  AppTabBar,
} from "@synopackageland/ui";
import {
  Download,
  FolderOpen,
  Plug,
  Package,
  Settings,
  RefreshCw,
  Plus,
  Pause,
  Play,
  RotateCcw,
  Trash2,
  Search,
} from "@lucide/vue";
import MediaSettings from "./MediaSettings.vue";
import type {MediaOptions} from "./media-options";
import AddDownloadDialog from "./AddDownloadDialog.vue";
import DownloadsList, {type ListRow} from "./DownloadsList.vue";
import TaskPanel from "./TaskPanel.vue";
import PhoneDownloads from "./PhoneDownloads.vue";
import { parseDeepLink, withoutDeepLink, type DeepLink } from "./deep-link";
import { addRecent, pinRecent } from "./quick-add";
import PreviewDialog from "./PreviewDialog.vue";
import {downloadComplete,downloadFailed,downloadTabs,fileKind,liveSummary,methodKey,sourceHost,tabGroups,tabRows,taskGroup,tileLabel,type DownloadGroup,type DownloadTab} from "./downloads-view";
import {previewType} from "./preview";
import {createDsmFileStation,createFileTransfers,type LaunchParams} from "@synopackageland/sdk";
import {takeIntake} from "@synopackageland/sdk/mobile";
import {intakeDraft,type IntakeDraft} from "./mobile-intake";
import {BrowserHelperKey,createBrowserHelper} from "./browser-helper";
import {createSearchTabs,activeSearch,receiveSource,type SourceView} from "./search";
import DownloadFolderDialog from "./DownloadFolderDialog.vue";
import DestinationRow from "./DestinationRow.vue";
import HomeServiceNotice from "./HomeServiceNotice.vue";
import {ensureDestinationKey,activePath,type DownloadLocation,createDestinationAdapter,destinationCopy} from "./destination";
import {AppShell,BackgroundSaveSettings,ExecutorSetupKey,PortalSettings,type AppShellPage} from "@synopackageland/sdk/vue";
import type {DestinationState} from "@synopackageland/sdk";
import { appLifecycle, bridgeErrorCopy, format as fill, lifecycleCopy, lifecycleRefusal, lifecycleRetryDelay, type AppLifecycle, type Context } from "@synopackageland/sdk";
import { revealSaved, savedFile } from "./navigation";
import {appCopy} from "./app-copy";
import {saveReasonMessage} from "./save-reason";
import {taskSize} from "./download-submission";
import {mediaRetryable,mediaTools,toolNames,toolVersion} from "./media";
import {withDeadline} from "./page-load";
import { createChunkLoader } from "./ui-update";
import { cardIcon, recommendedCards } from "./recommended";
const pluginFormId='plugin-'+useId();
const coreFormId='core-'+useId();
// The confirmation's text is its body and its accessible description.
const messageBox=ref<InstanceType<typeof MessageBoxProvider>>();
const toast=ref<InstanceType<typeof ToastProvider>>();
const props = defineProps<{ ctx: Context }>();
const chunks = createChunkLoader(props.ctx, import.meta.url);
const SearchPage = defineAsyncComponent(() => chunks.load(() => import("./SearchPage.vue")));
import GitHubNotice from "./GitHubNotice.vue";
import GitHubTokenSettings from "./GitHubTokenSettings.vue";
import { githubFailure, githubText } from "./github";
const SourceSettings = defineAsyncComponent(() => chunks.load(() => import("./SourceSettings.vue")));
onUnmounted(chunks.stop);
// Every child uses this call boundary, including SDK panels that handle their
// own errors. Keep the SDK's lifecycle parsing and wording in one place.
// Inherit live host metadata (for example a changed locale), rather than
// copying the context at mount time.
const ctx: Context = Object.create(props.ctx);
ctx.call = async function<T>(method: string, params?: Record<string, unknown>, options?: Parameters<Context["call"]>[2]) {
  try { return await props.ctx.call<T>(method, params, options); }
  catch (e) {
    if (alive && appLifecycle(e) === "starting") {
      observedLifecycle.value = "starting";
    }
    throw e;
  }
};
// The SDK notice's dialog must use the same observed call boundary too.
provide(ExecutorSetupKey,{...(ctx.executor ?? {async status(){return {state:"blocked"}},async authorize(){return {state:"blocked"}},async control(){return {state:"blocked"}}}),call:ctx.call});
const browserHelper=createBrowserHelper(ctx);
provide(BrowserHelperKey,browserHelper);
onMounted(()=>browserHelper.start(document.querySelector(".gopeed-root") as HTMLElement ?? document.body));
onUnmounted(()=>browserHelper.dispose());
const destinationAdapter=createDestinationAdapter(ctx);
const saves=destinationAdapter.sdk;
const backgroundSettings=ref<InstanceType<typeof BackgroundSaveSettings>>();
// Where this user's saves go: read once when the App opens, then from the
// settings section after each of its reads; null when it could not be read.
const saveDestination=ref<DownloadLocation|null>();
const folderDialogOpen=ref(false);
let folderResolution:((path:string|undefined)=>void)|undefined;
async function ensureDownloadDestination(){
 if(saveDestination.value===undefined)await readDestination();
 const selected=activePath(saveDestination.value);if(selected)return selected;
 if(folderResolution)return undefined;
 folderDialogOpen.value=true;
 return new Promise<string|undefined>(resolve=>{folderResolution=resolve});
}
function finishFolder(state?:DownloadLocation){
 if(state)saveDestination.value=state;
 folderDialogOpen.value=false;
 folderResolution?.(activePath(state));folderResolution=undefined;
}
provide(ensureDestinationKey,ensureDownloadDestination);

const pollNotice=ref("");
const observedLifecycle=ref<AppLifecycle>();
let lifecycleFromExecutor=false;
const pageNotice=computed(()=>observedLifecycle.value?lifecycleCopy(observedLifecycle.value,ctx.locale):pollNotice.value);
// The last poll's refusal; the next poll that reads everything clears it.
const pollError=ref("");
// The refusal behind error, so a GitHub limit can offer its token step.
const errorCause=ref<any>();
const errorGitHub=computed(()=>error.value?githubFailure(errorCause.value):undefined);
onUnmounted(()=>destinationAdapter.provider.dispose());
const copy=(key:string)=>appCopy(ctx.locale,key);
function openApplicationPrivileges(){
 const result=ctx.host.openApp('SYNO.SDS.AdminCenter.Application');
 if(result.status!=='requested') toast.value?.info(copy('entry.unavailable'));
}
const hostPlatform = ctx.host.platform();
const inDsmWindow = hostPlatform.host === "dsm" && hostPlatform.dsmStandalone !== true;
const inMobileShell = hostPlatform.host === "native-shell";
const page = ref("downloads"),
  // The quick filter's text; the tabs pick the state, this finds a name.
  search = ref(""),
  busy = ref(false),
  loading = ref(true),
  authorized = ref(false),
  privilegeDenied = ref(false),
  entryDenied = ref(false),
  error = ref("");
const session = reactive({ user: "", admin: false }),
  tasks = ref<any[]>([]),
  plugins = ref<any[]>([]),
  catalog = ref<any[]>([]),
  catalogError = ref(""),
  pluginsLoading = ref(false),
  catalogLoading = ref(false),
  pluginChecks = reactive<Record<string,boolean>>({}),
  rowBusy = reactive<Record<string,boolean>>({}),
  rowErrors = reactive<Record<string,string>>({}),
  rowCauses = reactive<Record<string,any>>({}),
  updates = reactive<Record<string,string>>({}),
  checkErrors = reactive<Record<string,string>>({}),
  createOpen = ref(false),
  searchTabs = reactive(createSearchTabs()),
  // Set when the search page sent the user to add a source.
  searchReturn = ref(false);
const pluginURL = ref(""),
  trust = ref(false),
  runtime = ref<any>(),
  release = ref<any>(),
  core = reactive({ maxRunning: 2, connections: 4, seedRatio: 0, seedMinutes: 0, btPort: 16889, ed2kPort: 4662, ed2kUdpPort: 4672, trackers: [] as string[] }),
  trackerText = ref("");
const toolStatus=ref<any>(),
  toolChecks=reactive<Record<string,any>>({});
const toolSupported=(tool:string)=>tool!=="gallery-dl"||!toolStatus.value||toolStatus.value.arch==="amd64";
const toolState=(tool:string)=>toolStatus.value?.state?.tools?.[tool];
// A tool admission and its durable job belong to this card alone.
const toolMutations=reactive<Record<string,{op:string,request?:string}>>({});
const toolReadError=ref("");
let toolTimer=0;
let toolRead:Promise<void>|undefined;
const toolJob=(tool:string)=>toolState(tool)?.job ?? (toolStatus.value?.state?.tool===tool ? {
  request:toolStatus.value.state.request,
  status:toolStatus.value.pending ? "running" : toolStatus.value.state.error ? "failed" : "succeeded",
  error:toolStatus.value.state.error,resetAt:toolStatus.value.state.resetAt,
} : undefined);
const toolPending=(tool:string)=>!!toolMutations[tool] || ["queued","running"].includes(toolJob(tool)?.status);
const toolOperation=(tool:string)=>toolMutations[tool]?.op ?? toolJob(tool)?.op ?? "install";
const toolFailure=(tool:string)=>toolPending(tool) ? undefined : toolJob(tool)?.error;
function toolFailureText(tool:string){
  const code=toolFailure(tool);
  return copy("media.err."+code) || copy("media.err.tool_install_failed");
}
// This read never waits for the core's status, downloads, or the page busy flag.
function refreshTools(){
  if(toolRead)return toolRead;
  toolRead=(async()=>{
    try {
      const result=await withDeadline(ctx.call<any>("media.tools.status",{}),toolLoad.signal);
      if(!alive || toolLoad.signal.aborted)return;
      toolStatus.value=result;toolReadError.value="";
      for(const tool of mediaTools){
        const local=toolMutations[tool],job=toolJob(tool);
        if(local?.request && job?.request===local.request && !["queued","running"].includes(job.status)) delete toolMutations[tool];
        const active=toolState(tool)?.active;
        if(!toolChecks[tool] && job?.release)toolChecks[tool]={latest:job.release,update:job.release.version!==active?.version};
        const check=toolChecks[tool];
        if(check && active)check.update=check.latest.version!==active.version;
      }
    } catch(e) {
      if(!alive || toolLoad.signal.aborted)return;
      denyAccess(e);toolReadError.value=message(e);
    }
  })().finally(()=>{toolRead=undefined});
  return toolRead;
}
const toolLoad=new AbortController();
async function pollTools(){
  if(!alive || !authorized.value || !session.admin)return;
  if(page.value==="binary" || mediaTools.some(toolPending))await refreshTools();
  if(alive && authorized.value && session.admin)toolTimer=window.setTimeout(pollTools,2000);
}
const installedIds=computed(()=>new Set(plugins.value.map(x=>x.identity)));
// Installed extensions are managed above; the catalog lists the rest.
// Recommended file-host extensions the store lists now; a missing entry or an
// unread catalog shows nothing. The catalog below leaves them out.
const recommended=computed(()=>catalogError.value?[]:recommendedCards(catalog.value,plugins.value));
const recommendedIds=computed(()=>new Set(recommended.value.map(r=>r.entry.id)));
const catalogAvailable=computed(()=>catalog.value.filter(x=>!installedIds.value.has(x.id)&&!recommendedIds.value.has(x.id)));
const catalogInstalled=computed(()=>catalog.value.filter(x=>installedIds.value.has(x.id)&&!recommendedIds.value.has(x.id)).length);
// A store icon that fails to load falls back to the extension glyph.
const brokenIcons=reactive<Record<string,boolean>>({});
// A previous selection with the active version restores nothing new.
function rollbackTarget(state:any){return state?.previous&&state.previous.version!==state.active?.version?state.previous:undefined}
const pluginSettings = ref<any>(),
  pluginDraft = reactive<Record<string, any>>({}),
  // The catalog entry being installed; the rest of the App stays usable.
  installing = ref("");
// Changes to the engine's extensions wait for an install in progress.
const extensionBusy = computed(() => busy.value || !!installing.value);
// Reads started for the current page; navigating away aborts them.
let pageLoad = new AbortController();
let alive = true,
  timer: number | undefined,
  // A stopped or starting package is read again on the SDK's schedule.
  lifecycleTimer = 0,
  lifecycleAttempt = 0,
  unsubscribe: () => void = () => {},
  stopLaunch: () => void = () => {};
// The DSM window and SDK dialog already share ExecutorSetup's published
// status. Prefer that over a download poll whose CGI can still say stopped.
ctx.executor?.subscribe?.(status => {
  if (!alive) return;
  const wasLifecycle=observedLifecycle.value;
  observedLifecycle.value=appLifecycle(status);
  lifecycleFromExecutor=!!observedLifecycle.value;
  if (observedLifecycle.value) refreshFailed(status);
  else if (wasLifecycle) pollNotice.value="";
});
// AppShell shows no page title (its hidden h1 names the page for screen
// readers); each page's own toolbar holds its actions.
const pages = computed<AppShellPage[]>(() => [
  { id: "downloads", title: copy("ui.nav.downloads"), icon: Download, fill: true },
  { id: "search", title: copy("search.nav"), icon: Search, fill: true },
  ...(session.admin
    ? [
        { id: "plugins", title: copy("ui.nav.extensions"), icon: Plug },
        { id: "binary", title: copy("ui.nav.coreUpdates"), icon: Package },
      ]
    : []),
  { id: "settings", title: copy("ui.nav.settings"), icon: Settings, placement: "bottom" },
]);
// Settings tabs, as Control Panel: each tab scrolls its sections over its footer.
const settingsTab = ref("general");
const settingsTabs = computed(() => [
  { value: "general", label: copy("ui.settings.tabGeneral") },
  { value: "search", label: copy("ui.settings.tabSearch") },
 {value:"media",label:copy("mediaOptions.title")},
  ...(session.admin ? [{ value: "github", label: copy("github.title") }] : []),
  ...(session.admin || ctx.portal ? [{ value: "access", label: copy("ui.settings.tabAccess") }] : []),
]);
watch(settingsTabs, (tabs) => {
  if (!tabs.some((tab) => tab.value === settingsTab.value)) settingsTab.value = "general";
});
// The tasks the filter finds, each with the values its row and panel show.
const rows = computed(() => {
  const needle = search.value.trim().toLocaleLowerCase(ctx.locale);
  return tasks.value.map(taskRow).filter((x) => !needle || String(x.name).toLocaleLowerCase(ctx.locale).includes(needle));
});
function taskRow(x: any) {
  const size = taskSize(x);
  return {
    ...x,
    name:
      x.publication?.filename ||
      x.meta?.res?.files?.[0]?.name ||
      x.meta?.opts?.name ||
      x.id,
    folder: x.publication?.folder || x.meta?.opts?.path,
    size,
    statusText: status(x),
    details: details(x),
    percent: percent({ ...x, size }),
    speed: downloading(x) ? x.progress?.speed || 0 : null,
    remaining: remaining(x, size),
    started: time(x.createdAt),
    // Gopeed has no completion time; a finished task last changed when it finished.
    completed: downloadComplete(x) ? time(x.updatedAt) : null,
    source: x.meta?.req?.url || "",
    group: taskGroup(x),
    host: sourceHost(x.meta?.req?.url || ""),
    method: copy(methodKey(x.protocol)),
  };
}
type TaskRow = ReturnType<typeof taskRow>;
// What a row's progress bar shows: none once saved, red when failed.
function rowBar(x: TaskRow): ListRow["bar"] {
  if (x.group === "failed") return "failed";
  if (x.group === "paused") return "paused";
  if (x.group === "done") return "none";
  return "active";
}
function rowMeta(x: TaskRow) {
  const parts: string[] = [];
  if (x.group === "done") {
    if (!x.publication?.published) return [String(x.statusText), ...(x.details ? [String(x.details)] : [])];
    if (x.host) parts.push(x.host);
    parts.push(x.method);
    if (x.completed) parts.push(formatRelativeTime(x.completed, { locale: ctx.locale }));
    return parts;
  }
  // Gopeed's size falls back to the bytes received; only a declared size is a total.
  const known = taskSize({ meta: x.meta }) > 0 && x.status !== "preparing";
  if (known) parts.push(`${bytes(x.progress?.downloaded || 0)} / ${bytes(x.size)}`);
  else if ((x.progress?.downloaded || 0) > 0) parts.push(bytes(x.progress.downloaded));
  if (x.group === "active" && x.speed !== null && x.speed > 0) parts.push(formatSpeed(x.speed));
  if (x.remaining !== null) parts.push(format("downloads.left", { time: formatDuration(x.remaining, { locale: ctx.locale }) }));
  if (x.group === "paused" || !parts.length || x.status !== "running") parts.push(String(x.statusText));
  if (x.details && x.group !== "active") parts.push(String(x.details));
  return parts;
}
function rowControls(x: TaskRow): ListRow["controls"] {
  const p = x.publication;
  const paused = x.status === "pause" || !!p?.manualPaused;
  const published = !!p?.published;
  const single = !(p?.files?.length > 1);
  return {
    preview: published && single && !!previewType(String(x.name)),
    reveal: published,
    retrySave: retryable(x) ? copy(p?.files?.length ? "bt.retry" : "save.retry") : undefined,
    pause: canControl(x) && x.status !== "error" && !paused,
    resume: canControl(x) && x.status !== "error" && paused,
    retry: canControl(x) && x.status === "error",
    clear: true,
    clearing: !!p?.deleting,
  };
}
function listRow(x: TaskRow): ListRow {
  return {
    id: x.id,
    name: String(x.name),
    group: x.group,
    status: String(x.statusText),
    tone: tone(x),
    kind: fileKind(String(x.name), x.protocol),
    tile: tileLabel(String(x.name), x.protocol),
    percent: x.status === "preparing" || (!(taskSize({ meta: x.meta }) > 0) && x.status !== "done") ? null : x.percent,
    bar: rowBar(x),
    meta: rowMeta(x),
    reason: x.group === "failed" ? String(x.details || x.statusText) : "",
    controls: rowControls(x),
  };
}
// The Downloads tabs, each with its count and its own toolbar actions.
const downloadTab = ref<DownloadTab>("all");
const tabLabels: Record<DownloadTab, string> = { all: "downloads.tab.all", active: "downloads.tab.active", done: "downloads.tab.done", failed: "downloads.tab.failed" };
const tabCounts = computed(() => {
  const counts = Object.fromEntries(downloadTabs.map((t) => [t, 0])) as Record<DownloadTab, number>;
  for (const x of tasks.value) for (const t of downloadTabs) if (tabGroups[t].includes(taskGroup(x))) counts[t]++;
  return counts;
});
const downloadTabItems = computed(() => downloadTabs.map((t) => ({ value: t, label: `${copy(tabLabels[t])} (${tabCounts.value[t]})` })));
const tabList = computed(() => tabRows(rows.value, downloadTab.value).map(listRow));
const tabActions = computed(() => {
  const list = tabList.value, out: { op: string; group: DownloadGroup; label: string }[] = [];
  // All lists the downloads too, so it keeps their Pause all and Resume all.
  if (downloadTab.value === "active" || downloadTab.value === "all") {
    if (list.some((r) => r.group === "active" && r.controls.pause)) out.push({ op: "pause", group: "active", label: copy("downloads.pauseAll") });
    if (list.some((r) => r.group === "paused" && r.controls.resume)) out.push({ op: "continue", group: "paused", label: copy("downloads.resumeAll") });
  }
  if (downloadTab.value === "done" || downloadTab.value === "failed") {
    const group = downloadTab.value, clear = clearableIn(group);
    if (clear.length) out.push({ op: "clear", group, label: `${copy(group === "failed" ? "ui.action.clearFailed" : "ui.action.clearCompleted")} (${clear.length})` });
  }
  return out;
});
// Phone (narrow and touch, the UI SDK's signal): a bottom tab bar, the
// one-line Add and the task's details in a sheet. Desktop stays as it is.
const phoneTabs = computed(() => [
  { key: "downloads", label: copy("ui.nav.downloads"), icon: Download, badge: tasks.value.filter((x) => taskGroup(x) === "active").length },
  { key: "search", label: copy("search.nav"), icon: Search },
  { key: "settings", label: copy("ui.nav.settings"), icon: Settings },
]);
// Downloads added on this device, newest first: they lead the phone's list.
const recent = ref<string[]>([]);
const phoneRows = computed(() => pinRecent(tabList.value, recent.value));
const taskIds = computed(() => tasks.value.map((x) => x.id));
const phoneMenu = computed(() => [
  ...tabActions.value.map((a) => ({ key: `${a.op}:${a.group}`, label: a.label })),
  { key: "refresh", label: copy("ui.action.refresh"), icon: RefreshCw },
]);
const phoneDownloads = ref<InstanceType<typeof PhoneDownloads>>();
const quickText = ref("");
function phoneMenuPick(key: string) {
  if (key === "refresh") return run(refreshPage);
  const [op, group] = key.split(":");
  return groupAct(group as DownloadGroup, op);
}
async function phoneAdded(ids: string[], text: string) {
  recent.value = addRecent(recent.value, ids);
  if (downloadTab.value !== "all" && downloadTab.value !== "active") downloadTab.value = "all";
  toast.value?.show({ title: text, tone: "success", actionLabel: copy("phone.view"), onAction: () => { detailId.value = ids[0]; } });
  await refresh();
}
// The full New download dialog, with what the one-line field holds.
const addInitial = ref("");
const sharedDraft = ref<IntakeDraft>();
const intakeNotice = ref("");
const addMediaOptions = ref<MediaOptions>();
function openAdd(text = "", mediaOptions?:MediaOptions) {
  sharedDraft.value = undefined;
  addMediaOptions.value = mediaOptions;
  addInitial.value = text;
  createOpen.value = true;
}
// A deep link opens a page; a link to add waits for the user's tap.
const confirmLink = ref("");
async function applyDeepLink(link: DeepLink) {
  if (link.page) navigate(link.page);
  if (page.value !== "downloads") return;
  if (link.filter) downloadTab.value = link.filter;
  if (!link.add) return;
  await nextTick();
  if (phoneDownloads.value) { quickText.value = link.add; confirmLink.value = link.add; }
  else openAdd(link.add);
}
function confirmDeepLink() {
  confirmLink.value = "";
  void phoneDownloads.value?.start();
}
const intakeAbort = new AbortController();
const intakeHandles = new Set<string>();
const launchQueue: LaunchParams[] = [];
let launchReady = false, launchWork = Promise.resolve();
function queueLaunch(params?: LaunchParams) {
  if (params) launchQueue.push(params);
  if (!launchReady) return;
  for (const launch of launchQueue.splice(0)) {
    launchWork = launchWork.then(async () => {
      if (!alive) return;
      if (!inMobileShell || !launch.intake) {
        await applyDeepLink(parseDeepLink(launch));
        return;
      }
      if (intakeHandles.has(launch.intake)) return;
      intakeHandles.add(launch.intake);
      try {
        const items = await takeIntake(launch.intake, {signal: intakeAbort.signal});
        if (!alive) return;
        const draft = intakeDraft(items);
        intakeNotice.value = "";
        navigate("downloads");
        if (!createOpen.value) {
          addInitial.value = "";
          addMediaOptions.value = undefined;
        }
        sharedDraft.value = draft;
        createOpen.value = true;
      } catch (e: any) {
        if (alive) intakeNotice.value = copy(e?.code === "intake_unsupported" ? "mobile.unsupported" : "mobile.intakeFailed");
      }
    });
  }
}
// The toolbar's live summary: what downloads now and how fast.
const summary = computed(() => liveSummary(rows.value));
const summaryText = computed(() =>
  summary.value.downloading
    ? `${format("downloads.downloadingCount", { count: summary.value.downloading })} · ↓ ${formatSpeed(summary.value.speed)}`
    : "",
);
// A group's records cleared in one go: saved downloads, or failed ones.
// A finished download still waiting to save is not complete yet.
function clearableIn(group: DownloadGroup) {
  return tasks.value.filter((x) => !x.publication?.deleting && (group === "failed" ? downloadFailed(x) : group === "done" && downloadComplete(x) && !!x.publication?.published));
}
function format(key: string, values: Record<string, string | number>) {
  return fill(copy(key), values, ctx.locale);
}
function filesSaved(x: any) {
  const files = x.publication?.files || [];
  return format('bt.filesSaved', { saved: files.filter((f: any) => f.state === 'committed').length, total: files.length });
}
// What a status needs said, its reason or next step, in one line beside it.
function details(x: any) {
  const p = x.publication;
  const parts: string[] = [];
  if (x.status === "done" && !p?.delegated && !p?.published && !p?.uncertain) parts.push(copy("multi.legacyHelp"));
  const reason = p?.delegated && p?.state === "canceled" && saveReasonMessage(p?.reason, ctx.locale);
  if (reason) parts.push(`${["scope-changed", "grant-revoked", "integration-disabled"].includes(p.reason) ? copy("save.settingsCanceled") + " " : ""}${reason}`);
  if (p?.files?.length) parts.push(filesSaved(x) + (retryable(x) ? ` · ${copy("bt.retryHelp")}` : ""));
  if (x.protocol==='media') parts.push(copy('download.mediaResumeHelp'));
  if ((p?.manualPaused || p?.pausePending) && p?.state) parts.push(copy('download.saveResumeHelp'));
  if (p?.deleting) parts.push(copy("delete.cancelingHelp"));
  else if (p?.state === "copying" && p?.error === "CANCEL_REQUESTED") parts.push(copy("save.cancelingHelp"));
  else if (p?.state === "interrupted" && p?.error === "EXECUTOR_PAUSED") parts.push(copy("save.pausedHelp"));
  else if (!p?.manualPaused && !p?.pausePending && p?.state !== "canceled" && (p?.uncertain || p?.error))
    parts.push(p.state === "pending" ? copy("save.pendingHelp") : p.state === "save_failed" ? saveFailedHelp(x) : p.uncertain ? message({ code: "commit_uncertain" }) : statusMessage({ code: p.error }));
  return parts.join(" · ");
}
function retryable(x: any) {
  return !!x.publication?.retryable && !x.publication?.deleting;
}
// File Station refusals of a move name their own cause; the next step
// follows the task's retryable flag and never promises a refused retry.
const saveCauses = ["destination-exists", "not-found", "name-too-long", "PERMISSION_DENIED", "SCOPE_DENIED", "APP_DENIED"];
function saveFailedHelp(x: any) {
  const error = x.publication?.error;
  if (saveCauses.includes(error)) return copy("error." + error) + " " + copy(retryable(x) ? "save.retryStep" : "save.noRetryStep");
  if (retryable(x)) return copy("save.retryHelp");
  return error === "WRITE_FAILED" || error === "save_interrupted" ? copy("save.interruptedHelp") : copy("save.failedHelp");
}
// Gopeed can still report a task running at no speed after every declared
// byte arrived (seen while the executor was stopped); it only waits to save.
function received(x: any) {
  const declared = taskSize({ meta: x.meta });
  return x.status === "running" && declared > 0 && (x.progress?.downloaded || 0) >= declared;
}
function status(x: any) {
 if (x.publication?.deleting) return copy('ui.state.canceling');
 if (x.status==='pause') return copy('ui.state.paused');
 if (x.status==='preparing') return copy(x.waiting ? 'ui.state.waitingPeers' : 'ui.state.preparing');
 if (x.status==='pausing') return copy('ui.state.pausing');
 if (x.preparation && x.status==='error') return copy('ui.state.failed');
 if (mediaRetryable(x)) return copy('ui.state.failed');
 if (x.status === 'done' && x.uploading) return copy('ui.state.seeding');
 if (x.publication?.state === 'pending') return copy('ui.state.savePending');
 if (x.publication?.error === 'download_incomplete') return copy('ui.state.downloadFailed');
 if (x.publication?.state === "interrupted") return copy("ui.state.saveInterrupted");
 if (x.publication?.state === "copying" && x.publication?.error === "CANCEL_REQUESTED") return copy("ui.state.canceling");
 if (x.publication?.state === "canceled") return copy('ui.state.saveCanceled');
  if (x.publication?.uncertain)
    return copy("ui.state.saveUncertain");
  if (x.publication?.error) return copy("ui.state.saveFailed");
  if (x.status === "done" || received(x))
    return x.publication?.published
      ? copy("ui.state.saved")
      : copy("ui.state.savePending");
  return (
    (
      {
        running: copy("ui.state.downloading"),
        pause: copy("ui.state.paused"),
        wait: copy("ui.state.queued"),
        ready: copy("ui.state.ready"),
        error: copy("ui.state.failed"),
      } as any
    )[x.status] || x.status
  );
}
// The status dot: red for a failure, green once saved, amber when the
// owner has something to do, blue while work goes on. Waiting to save is
// work going on in either phase, so it shares one tone as it shares one name.
function tone(x: any) {
  const p = x.publication;
  if (x.status === "error" || mediaRetryable(x) || p?.error === "download_incomplete" || p?.state === "save_failed") return "danger";
  if (p?.deleting || p?.state === "pending" || p?.state === "copying" || x.status === "running" || (x.status === "done" && x.uploading)) return "info";
  if (p?.uncertain || p?.error || p?.state === "interrupted" || p?.state === "canceled") return "warning";
  if (x.status === "done") return p?.published ? "success" : "info";
  return "neutral";
}
// Gopeed reports an unknown size as 0; it reads as a dash.
function bytes(n: number) {
  return formatFileSize(n > 0 ? n : undefined);
}
function downloading(x: any) {
  return x.status === "running" && !received(x);
}
function remaining(x: any, size: number) {
  const speed = x.progress?.speed || 0;
  // A size read back from the bytes so far is no total to count down to.
  const known = taskSize({ meta: x.meta }) > 0;
  return downloading(x) && known && size > 0 && speed > 0 ? Math.ceil((size - (x.progress?.downloaded || 0)) / speed) : null;
}
// Gopeed sends Go's zero time for a time it does not know.
function time(iso: unknown) {
  const ms = typeof iso === "string" ? Date.parse(iso) : NaN;
  return ms > Date.UTC(2000, 0, 1) ? ms : null;
}
function percent(x: any) {
  return Math.min(
    100,
    Math.max(
      0,
      x.status === "done"
        ? 100
        : ((x.progress?.downloaded || 0) / (x.size || 1)) * 100,
    ),
  );
}
// The executor answers these while it cannot take work yet; the same
// request succeeds later. The others end a save for good.
const retryLaterCodes = ["ADMISSION_PENDING", "EXECUTOR_UNAVAILABLE", "EXECUTOR_TIMEOUT", "EXECUTOR_PROTOCOL", "canceled", "EXECUTOR_PAUSED", "queue-full", "STORAGE_ERROR"];
const saveFailedCodes = ["ADMISSION_EXHAUSTED", "STAGING_CHANGED", "operation-not-found", "operation-conflict", "disabled", "SCOPE_DENIED"];
function message(e: any) {
  if(e?.code==='search_browser_network')return copy('browser.networkHelp');
  if(e?.code==='search_parse')return copy('search.reason.parse');
  if(e?.code==='search_browser_verification')return copy('browser.verificationHelp');
  if(typeof e?.code==='string'&&e.code.startsWith('search_rto_')){
   return copy('browser.'+(/user_cancelled|user_canceled|allowlist|domain|permission|refus/.test(e.code)?'permissionHelp':/tab_closed|tab_changed|no_controlled_tab/.test(e.code)?'tabClosed':/extract_limit/.test(e.code)?'sizeHelp':'retryHelp'));
  }
  if(e?.code==='search_browser_torrent_manual')return copy('browser.torrentManual');
  if(e?.code==='search_browser_unsupported')return copy('browser.unsupportedHelp');
  if(e?.code==='search_browser_expired'||e?.code==='search_browser_canceled')return copy('browser.expiredHelp');
  if(e?.code==='search_browser_size')return copy('browser.sizeHelp');
  const github = githubFailure(e);
  if (github) return githubText(github, ctx.locale);
  const mediaReason = typeof e?.code === "string" ? copy("media.err." + e.code) : "";
  if (mediaReason) return mediaReason;
  // An action a stopped or starting package refused is tried again by
  // hand; only the page's own reads update by themselves (refreshFailed).
  const refusal = lifecycleRefusal(e, ctx.locale);
  if (refusal) return refusal;
  const map: Record<string, string> = {
    admin_required: copy('multi.adminRequired'),
    app_privilege_denied: copy('entry.help'),
    background_destination_required: copy('multi.waitingGrant'),
    save_unavailable: destinationCopy(ctx.locale).error,
    download_incomplete: copy('download.incomplete'),
    APP_DENIED: copy('error.APP_DENIED'),
    DELEGATION_DENIED: copy('save.accessDenied'),
    ...Object.fromEntries(retryLaterCodes.map((code) => [code, copy('save.pendingHelp')])),
    ...Object.fromEntries(saveFailedCodes.map((code) => [code, copy('save.failedHelp')])),
  };
  if (!e?.code) return e?.message || copy("error.unknown");
  return map[e.code] || copy("error." + e.code) || bridgeErrorCopy(e, ctx.locale) || copy("error.unknown");
}
// A state the App leaves by itself, such as a save the starting executor
// will take, reads in the SDK's page words; message() is for actions.
function statusMessage(e: any) {
  const lifecycle = appLifecycle(e);
  return lifecycle ? lifecycleCopy(lifecycle, ctx.locale) : message(e);
}
// A refused add created nothing that could resume: the owner adds it again.
function addFailure(e: any) {
  return retryLaterCodes.includes(e?.code) ? copy("add.saveUnavailable") : message(e);
}
async function request(method: string, path: string, data: any = null) {
  const r: any = await ctx.call("gopeed.request", {
    method,
    path,
    query: {},
    data,
  });
  if (r.code !== 0)
    throw { code: "engine_request_failed" };
  return r.data;
}
async function run(fn: () => Promise<void>) {
  if (busy.value) return;
  busy.value = true;
  try {
    await attempt(fn);
  } finally {
    if (alive) busy.value = false;
  }
}
// An action's refusal replaces the page's last message.
async function attempt(fn: () => Promise<void>) {
  error.value = "";
  try {
    await fn();
  } catch (e) {
    denyAccess(e);
    if (alive) { error.value = message(e); errorCause.value = e; }
  }
}
// "Add a GitHub token" from any GitHub refusal opens Settings at the field.
const githubFocus = ref(0);
function openGitHubToken() {
  navigate("settings");
  if (page.value !== "settings") return;
  settingsTab.value = "github";
  githubFocus.value++;
}
function denyAccess(e: any) {
  if (e?.code === "app_privilege_denied") privilegeDenied.value = true;
  // A failed DSM verification is temporary, not a confirmed sign-out.
  if (!["session_required", "app_privilege_denied"].includes(e?.code)) return;
  entryDenied.value = true;
  authorized.value = false;
  tasks.value = [];
  plugins.value = [];
  runtime.value = undefined;
  release.value = undefined;
  createOpen.value = false;
  pluginSettings.value = undefined;
  messageBox.value?.onClose();
  clearInterval(timer);
  unsubscribe();
}
// Removals already listed at the first read happened before the App opened
// and are not news.
const seenRemovals = new Set<string>();
let removalsRead = false;
async function refreshDownloads() {
    const list = await request("GET", "/api/v1/tasks");
    if (alive) tasks.value = (list || []).filter((x: any) => !removing.has(x.id));
    const removals = await request("GET", "/api/v1/removals");
    if (alive && Array.isArray(removals)) {
      for (const removed of removals) {
        if (removalsRead && !seenRemovals.has(removed.id) && removed.fileKept) toast.value?.success(copy('delete.fileKept') + ' ' + removed.filename);
        seenRemovals.add(removed.id);
      }
      removalsRead = true;
    }
}
async function refresh(loadSettings = false, signal = pageLoad.signal) {
  if (!alive || !authorized.value) return;
  // After a refused read (a package stop, an engine start or restart) the
  // page is read again in full, as when it opened.
  const recovering = (!!pollError.value || !!pageNotice.value) && !loadSettings;
  // A stopped engine refuses the task list; the admin page still reads the
  // supervisor's own status, which names the real cause.
  const [downloads, admin] = await Promise.allSettled([refreshDownloads(), refreshAdmin(loadSettings, signal)]);
  if (signal.aborted) return;
  if (downloads.status === "fulfilled" && alive) {
    pollNotice.value="";
    if (!lifecycleFromExecutor) observedLifecycle.value=undefined;
  }
  const failures = [downloads, admin].flatMap((x) => x.status === "rejected" ? [engineCause(x.reason)] : []);
  if (failures.length) {
    refreshFailed(failures[0]);
    for (const e of failures.slice(1)) if (authorized.value) denyAccess(e);
    return;
  }
  if (alive) pollError.value = "";
  lifecycleAttempt = 0;
  if (recovering) await refresh(true, signal);
}
// Each section of an admin page loads on its own, so a slow external source
// (catalog, update checks) never holds up the rest of the page.
async function refreshAdmin(loadSettings: boolean, signal: AbortSignal) {
  if (!session.admin) return;
  const read = <T>(method: string) => withDeadline(ctx.call<T>(method, {}), signal);
  if (page.value === "plugins") {
    if (loadSettings) void loadCatalog(signal);
    await loadPlugins(signal, loadSettings);
    if (loadSettings && authorized.value) void checkPlugins(signal);
  }
  if (page.value === "binary") {
    await Promise.all([
      read<any>("binaries.status").then(status=>{runtime.value=status}),
      refreshTools(),
    ]);
  }
  if (loadSettings && page.value === "settings") readCore(await read("gopeed.settings.get"));
}
function readCore(settings: any) {
  Object.assign(core, settings);
  trackerText.value = (settings.trackers ?? []).join("\n");
  coreRead.value = coreDraft();
}
// The form as the engine would read it; Reset returns to the last read.
function coreDraft() {
  const { maxRunning, connections, seedRatio, seedMinutes, btPort, ed2kPort, ed2kUdpPort } = core;
  const numbers = Object.entries({ maxRunning, connections, seedRatio, seedMinutes, btPort, ed2kPort, ed2kUdpPort }).map(([key, value]) => [key, Number(value)]);
  return JSON.stringify({ ...Object.fromEntries(numbers), trackers: trackerText.value.split(/\s+/).filter(Boolean) });
}
const coreRead = ref("");
const coreChanged = computed(() => !!coreRead.value && coreDraft() !== coreRead.value);
function resetCore() {
  readCore(JSON.parse(coreRead.value));
}
function applyCore() {
  return run(async () => {
    readCore(await ctx.call("gopeed.settings.set", JSON.parse(coreDraft())));
    toast.value?.success(copy("ui.settings.saved"));
  });
}
async function loadPlugins(signal: AbortSignal, first: boolean) {
  if (first) pluginsLoading.value = true;
  try {
    const previous = new Map(plugins.value.map(x=>[x.identity,x.version]));
    plugins.value = await withDeadline(ctx.call<any[]>("gopeed.plugins.list", {}), signal);
    for (const x of plugins.value) if (previous.has(x.identity) && previous.get(x.identity) !== x.version) delete updates[x.identity];
  } finally {
    if (!signal.aborted) pluginsLoading.value = false;
  }
}
async function loadCatalog(signal = pageLoad.signal) {
  catalogLoading.value = true;
  catalogError.value = "";
  try { catalog.value = await withDeadline(ctx.call<any[]>("gopeed.plugins.catalog", {}), signal); }
  catch (e) { if (signal.aborted) return; denyAccess(e); catalogError.value = message(e); }
  finally { if (!signal.aborted) catalogLoading.value = false; }
}
// An unreachable engine is explained by the supervisor's runtime error.
function engineCause(e: any) {
  return e?.code === "engine_unavailable" && runtime.value?.error?.startsWith("engine_") ? { code: runtime.value.error } : e;
}
function refreshFailed(e: any) {
  denyAccess(e);
  const lifecycle = observedLifecycle.value ?? appLifecycle(e);
  if (alive && lifecycle) {
    // The package answers again by itself: information, not a failure.
    pollError.value = "";
    pollNotice.value = lifecycleCopy(lifecycle, ctx.locale);
    clearTimeout(lifecycleTimer);
    lifecycleTimer = window.setTimeout(() => { lifecycleTimer = 0; void refresh(); }, lifecycleRetryDelay(lifecycleAttempt++, lifecycle));
  } else if (alive && e?.code === "engine_busy") {
    pollNotice.value=copy("scope.statusPending");
  } else if (alive) {
    // A failure replaces the lifecycle copy of an earlier poll.
    pollNotice.value = "";
    pollError.value = message(e);
  }
}
// Each page's toolbar holds its one refresh; the panels expose theirs.
async function refreshPage() {
  await Promise.all([page.value === "settings" ? backgroundSettings.value?.refresh() : undefined, refresh()]);
}
function navigate(key: string) {
  if (busy.value) return;
  pageLoad.abort();
  pageLoad = new AbortController();
  page.value = key;
  error.value = "";
  void loadPage(pageLoad.signal);
}
async function loadPage(signal: AbortSignal) {
  try { await refresh(true, signal); }
  catch (e) { if (signal.aborted) return; denyAccess(e); if (alive) { error.value = message(e); errorCause.value = e; } }
}
async function readDestination() {
  try { saveDestination.value = await saves.destination("save"); }
  catch { saveDestination.value = null; }
}
// The details panel follows its row through each refresh and closes with it.
const detailId = ref(""),
  // Bumped to bring the panel's file list into view.
  focusFiles = ref(0);
let searchSelection: { id: string; batch: string } | undefined;
watch([tasks, detailId], () => {
  const selection = searchSelection;
  if (!selection) return;
  // A user choosing another row or closing details ends the follow.
  if (detailId.value !== selection.id) { searchSelection = undefined; return; }
  const current = tasks.value.find(t => t.id === selection.id);
  const resolved = current && !current.preparation && !current.id.startsWith('prepare-') ? current
    : tasks.value.find(t => t.publication?.batch === selection.batch && !t.preparation && !t.id.startsWith('prepare-')) ?? current;
  if (resolved) { selection.id = resolved.id; detailId.value = resolved.id; }
});
// Torrents added with file choice on: each opens its Files tab once its
// file list is known.
const chooseBatches = ref<string[]>([]);
function chooseLater(keys: string[]) {
  chooseBatches.value = [...chooseBatches.value, ...keys];
}
watch([tasks, chooseBatches], () => {
  for (const x of tasks.value) {
    const key = x.publication?.batch;
    if (!key || !chooseBatches.value.includes(key) || x.preparation) continue;
    const files = x.meta?.res?.files?.length || 0;
    if (x.protocol === "bt" && !files && x.status !== "done" && x.status !== "error") continue;
    chooseBatches.value = chooseBatches.value.filter((k) => k !== key);
    if (files > 1 && !detailId.value) {
      detailId.value = x.id;
      focusFiles.value++;
    }
  }
});
function openDetails(id: string) {
  detailId.value = detailId.value === id ? "" : id;
}
const detailTask = computed(() => {
  const x = detailId.value && tasks.value.find((t) => t.id === detailId.value);
  return x ? taskRow(x) : undefined;
});
const detailView = computed(() => detailTask.value && listRow(detailTask.value));
function rowAct(id: string, op: string, file?: string) {
  const x = tasks.value.find((t) => t.id === id);
  if (!x) return;
  if (op === "pause" || op === "continue") return action(x, op);
  if (op === "retrySave") return retryFiles(x);
  if (op === "reveal") return reveal(x);
  if (op === "clear") return clearRecord(x);
  if (op === "preview") return openPreview(x);
  if (op === "previewFile" && file) return openPreview(x, file);
  if (op === "savePhone") return saveToPhone(x, file);
  if (op === "copied") toast.value?.success(copy("details.linkCopied"));
  if (op === "copyFailed") toast.value?.info(copy("details.copyFailed"));
}
function groupAct(group: DownloadGroup, op: string) {
  if (op === "clear") return clearRecords(group);
  return batchAction(op, tasks.value.filter((x) => taskGroup(x) === group));
}
// The saved file the preview shows. Its path comes only from the
// owner-bound reveal; a torrent's other files sit in the same folder.
const preview = ref<{ id: string; name: string; size: number; file?: string }>();
const dsmFiles = createDsmFileStation();
const fileTransfers = createFileTransfers(ctx.files);
onUnmounted(() => fileTransfers.dispose());
async function saveToPhone(x: any, file?: string) {
  if (!inMobileShell || !x.publication?.published || busy.value) return;
  await run(async () => {
    if (file && !x.publication?.files?.some((f: any) => f.name === file && f.state === "committed")) throw {code:"invalid_file"};
    const destination: any = await ctx.call("gopeed.reveal", {id: x.id});
    if (file && (file.includes("/") || file.includes("\\") || file === "." || file === "..")) throw {code:"invalid_file"};
    const path = savedFile(file ? {folder: destination.folder, filename: file} : destination);
    const entry = await destinationAdapter.provider.info(path, pageLoad.signal);
    if (entry.isDir) throw {code:"invalid_file"};
    await fileTransfers.download(path);
  });
}
function openPreview(x: any, file?: string) {
  const row = taskRow(x);
  const saved = file ? x.publication?.files?.find((f: any) => f.name === file) : undefined;
  preview.value = { id: x.id, name: file || String(row.name), size: saved ? saved.size : row.size, file };
}
async function previewPath() {
  const p = preview.value!;
  const destination: any = await ctx.call("gopeed.reveal", { id: p.id });
  if (!p.file) return savedFile(destination);
  if (p.file.includes("/") || p.file === "." || p.file === "..") throw { code: "invalid_file" };
  return savedFile({ folder: destination.folder, filename: p.file });
}
function previewReveal() {
  const x = preview.value && tasks.value.find((t) => t.id === preview.value!.id);
  preview.value = undefined;
  if (x) void reveal(x);
}
const taskStats = (id: string) => request("GET", `/api/v1/tasks/${id}/stats`);
// A task added from search: the Downloads list shows it selected.
async function showTask(id: string, batch = '') {
  navigate("downloads");
  if (page.value !== "downloads") return;
  search.value = "";
  downloadTab.value = "all";
  searchSelection = batch ? { id, batch } : undefined;
  detailId.value = id;
  await nextTick();
  Array.from(document.querySelectorAll<HTMLElement>("[data-downloads-list] [data-row-key]")).find((e) => e.dataset.rowKey === id)?.scrollIntoView?.({ block: "nearest" });
}
function openSearchSettings() {
  navigate("settings");
  if (page.value === "settings") settingsTab.value = "search";
}
function addSearchSource() {
  searchReturn.value = true;
  openSearchSettings();
}
// A source added on the way from search goes straight back to the query.
function searchSourceSaved(source: SourceView) {
  for (const tab of searchTabs.tabs) receiveSource(tab,source);
  if (!searchReturn.value) return;
  searchReturn.value = false;
  const current = activeSearch(searchTabs);
  if (current.selected && !current.selected.includes(source.id)) current.selected.push(source.id);
  navigate("search");
}
// The dialog closes on its own; the list shows the new downloads.
async function downloadsAdded(text: string) {
  toast.value?.success(text);
  await refresh();
}
// A row check marks only its own row and keeps every other control usable.
async function checkRow<T>(row: string, work: () => Promise<T>, done: (value: T) => void) {
  if (rowBusy[row]) return;
  const signal = pageLoad.signal;
  rowBusy[row] = true;
  delete rowErrors[row];
  delete rowCauses[row];
  try { done(await withDeadline(work(), signal)); }
  catch (e) { if (signal.aborted) return; denyAccess(e); rowErrors[row] = message(e); rowCauses[row] = e; }
  finally { delete rowBusy[row]; }
}
function checkTool(tool: string) {
  delete toolChecks[tool];
  return checkRow(tool, () => ctx.call("media.tools.check", { tool }), check => { toolChecks[tool] = check; });
}
function checkCore() {
  return checkRow("core", () => ctx.call("binaries.check", {}), latest => { release.value = latest; });
}
async function mutateTool(tool:string,op:string,work:()=>Promise<any>){
  if(toolPending(tool))return;
  toolMutations[tool]={op};delete rowErrors[tool];delete rowCauses[tool];
  try {
    const result=await work();
    if(!alive)return;
    toolMutations[tool]={op,request:result.request};
    await refreshTools();
  } catch(e) {
    if(!alive)return;
    denyAccess(e);rowErrors[tool]=message(e);rowCauses[tool]=e;
    delete toolMutations[tool];
  }
}
function updateTool(tool: string) {
  const latest = toolChecks[tool]?.latest;
  if (!latest || toolPending(tool)) return;
  ask(format("tools.confirmUpdate", { tool: toolNames[tool] }), `${copy(tool === "yt-dlp" && toolState(tool)?.active ? "tools.selfUpdateHelp" : "tools.installHelp")}\n${toolState(tool)?.active?.version || copy("tools.notInstalled")} → ${latest.version}`, async () => {
    return ctx.call("media.tools.update", { tool, version: latest.version, sha256: latest.sha256, consent: true });
  }, fn=>mutateTool(tool,"install",fn));
}
function rollbackTool(tool: string) {
  if(toolPending(tool))return;
  ask(format("tools.confirmRollback", { tool: toolNames[tool] }), `${copy("tools.rollbackHelp")}\n${toolState(tool)?.active?.version} → ${toolState(tool)?.previous?.version}`, async () => {
    return ctx.call("media.tools.rollback", { tool, consent: true });
  }, fn=>mutateTool(tool,"rollback",fn));
}
async function retryFiles(x: any) {
  await run(async () => {
    await ctx.call("gopeed.publish", { id: x.id });
    await refresh();
  });
}
async function action(x: any, op: string) {
  await run(async () => {
    await request("PUT", `/api/v1/tasks/${x.id}/${op}`);
    await refresh();
  });
}
// Rows confirmed for clearing whose clearing has not answered yet: the engine
// may take a while to drop a stalled download. Lists read meanwhile skip them.
function canControl(x:any){return !x.publication?.deleting && (!x.publication?.published || x.uploading)}
// A tab toolbar's Pause all or Resume all: each task the action applies to.
async function batchAction(op:string,tasksIn:any[]){
 const list=tasksIn.filter(x=>canControl(x) && (op==="continue" ? x.status==="pause" || x.publication?.manualPaused || x.status==="error" : x.status!=="pause" && !x.publication?.manualPaused && x.status!=="error"));
 await run(async()=>{let failure:unknown;for(const x of list){try{await request("PUT",`/api/v1/tasks/${x.id}/${op}`)}catch(e){failure??=e}}await refresh();if(failure)throw failure});
}
const removing = new Set<string>();
// Clears the records one at a time; a refusal leaves the others going and is
// answered with the count cleared before it.
async function clearTasks(list: any[]) {
  for (const x of list) removing.add(x.id);
  tasks.value = tasks.value.filter((t) => !removing.has(t.id));
  let cleared = 0, fileKept = false, failure: unknown;
  try {
    for (const x of list) {
      try {
        const result = await request("DELETE", `/api/v1/tasks/${x.id}`);
        cleared++;
        fileKept ||= !!result?.fileKept;
      } catch (e) {
        failure ??= e;
      } finally {
        removing.delete(x.id);
      }
    }
  } finally {
    // A cancel in progress or a refusal shows the row again.
    await refresh();
  }
  return { cleared, fileKept, failure };
}
function clearRecord(x: any) {
  ask(copy("ui.confirm.clearRecord"), copy("ui.confirm.clearRecordKeeps"), async () => {
    const { fileKept, failure } = await clearTasks([x]);
    if (failure) throw failure;
    if (fileKept) toast.value?.success(copy("delete.fileKept"));
  });
}
function clearRecords(group: DownloadGroup) {
  const list = clearableIn(group), kind = group === "failed" ? "Failed" : "Completed";
  ask(format(`ui.confirm.clear${kind}`, { count: list.length }), copy(`ui.confirm.clear${kind}Keeps`), async () => {
    const { cleared, failure } = await clearTasks(list);
    if (cleared) toast.value?.success(cleared === 1 ? copy("ui.notice.cleared.one") : format("ui.notice.cleared", { count: cleared }));
    if (failure) throw failure;
  });
}
async function ask(title: string, description: string, fn: () => Promise<any>, start: (fn:()=>Promise<any>)=>Promise<void> = run) {
  if (await messageBox.value?.confirm(title, description) === "confirm") await start(fn);
}
// Gopeed 1.9.3 adds an installed extension to its list without a lock, so
// extensions change one at a time.
async function installEntry(id: string, fn: () => Promise<void>) {
  if (busy.value || installing.value) return;
  installing.value = id;
  try {
    await attempt(fn);
  } finally {
    if (alive) installing.value = "";
  }
}
async function reveal(x: any) {
  if (busy.value) return;
  const popup = inDsmWindow ? undefined : window.open("about:blank", "_blank");
  if (popup) popup.opener = null;
  await run(async () => {
    try {
      const destination: any = await ctx.call("gopeed.reveal", {
        id: x.id,
      });
      const file = savedFile(destination);
      const entry = await destinationAdapter.provider.info(file, pageLoad.signal);
      if (entry.isDir) throw {code:"invalid_file"};
      const result = revealSaved(ctx, x.id, file, popup);
      // File Station opening is the feedback; only a refusal says anything.
      if (result.status !== "requested")
        throw {
          message: copy("ui.notice.desktopBlocked"),
        };
    } catch (e) {
      popup?.close();
      throw e;
    }
  });
}
async function checkPlugins(signal = pageLoad.signal) {
  await Promise.all(plugins.value.map(async x => {
    const id = x.identity;
    if (pluginChecks[id]) return;
    pluginChecks[id] = true;
    try { const check:any = await withDeadline(ctx.call("gopeed.plugins.check", {id}), signal); updates[id] = check.newVersion || ""; delete checkErrors[id]; }
    catch(e) { if (!signal.aborted) { denyAccess(e); checkErrors[id] = message(e); } }
    finally { delete pluginChecks[id]; }
  }));
}
function installCatalog(x:any) {
  ask(copy("ui.plugins.confirmTrustInstall"), `${copy("ui.plugins.permissions")} ${copy("ui.plugins.disabledAfterInstall")}\n${x.title} ${x.version}\n${x.url}`, async()=>{
    await ctx.call("gopeed.plugins.install", {url:x.url,consent:true});
    await refresh(true);
    toast.value?.success(copy("ui.plugins.installedDisabled"));
  }, fn => installEntry(x.id, fn));
}
async function installPlugin() {
  await run(async () => {
    await ctx.call("gopeed.plugins.install", {
      url: pluginURL.value.trim(),
      consent: trust.value,
    });
    pluginURL.value = "";
    trust.value = false;
    await refresh();
    toast.value?.success(copy("ui.plugins.installedNowDisabled"));
  });
}
async function mutatePlugin(x: any, op: string) {
  await ctx.call(`gopeed.plugins.${op}`, {
    id: x.identity,
    ...(op === "update" ? {version:updates[x.identity]} : {}),
    consent: true,
    ...(op === "switch" ? { enabled: x.disabled } : {}),
  });
  if (op === "update" || op === "delete") delete updates[x.identity];
  await refresh();
}
function editPlugin(x: any) {
  pluginSettings.value = x;
  for (const k of Object.keys(pluginDraft)) delete pluginDraft[k];
  for (const field of x.settings || [])
    pluginDraft[field.name] =
      field.value ??
      (field.type === "boolean" ? false : field.type === "number" ? 0 : "");
}
async function savePlugin() {
  await run(async () => {
    const x = pluginSettings.value;
    const values: Record<string, any> = {};
    for (const f of x.settings || [])
      values[f.name] =
        f.type === "number" ? Number(pluginDraft[f.name]) : pluginDraft[f.name];
    await ctx.call("gopeed.plugins.settings", {
      id: x.identity,
      settings: values,
    });
    pluginSettings.value = undefined;
    await refresh();
    toast.value?.success(copy("ui.plugins.settingsSaved"));
  });
}
onMounted(async () => {
  stopLaunch = ctx.onLaunch?.(queueLaunch) ?? (() => {});
  await run(async () => {
    Object.assign(session, await ctx.call("gopeed.session", {}));
 session.admin = session.admin && ctx.user?.isAdmin === true;
    authorized.value = true;
    entryDenied.value = false;
    void readDestination();
    await refresh();
    const revealID = new URL(location.href).searchParams.get("gopeedReveal");
    if (revealID && ctx.host.platform().dsmStandalone !== true) {
      const dest: any = await ctx.call("gopeed.reveal", { id: revealID });
      const file = savedFile(dest);
      const entry = await destinationAdapter.provider.info(file, pageLoad.signal);
      if (entry.isDir) throw {code:"invalid_file"};
      ctx.host.revealFile(file);
    }
  });
  loading.value = false;
  if (!authorized.value || !alive) return;
  const deepLink = parseDeepLink(ctx.launchParams, new URL(location.href).searchParams);
  const clean = withoutDeepLink(location.href);
  if (clean) history.replaceState(history.state, "", clean);
  applyDeepLink(deepLink);
  launchReady = true;
  if (ctx.launchParams?.intake) launchQueue.unshift(ctx.launchParams);
  queueLaunch();
  if(session.admin)toolTimer=window.setTimeout(pollTools,2000);
  timer = window.setInterval(() => {
    if (!busy.value && !lifecycleTimer) void refresh();
  }, 4000);
  unsubscribe = ctx.reopen(() => void refresh());
});
onUnmounted(() => {
  folderResolution?.(undefined);folderResolution=undefined;
  alive = false;
  intakeAbort.abort();
  pageLoad.abort();
  clearInterval(timer);
  clearTimeout(toolTimer);
  toolLoad.abort();
  clearTimeout(lifecycleTimer);
  unsubscribe();
  stopLaunch();
});
</script>
<template>
  <AppShell app-root title="Download Hub" :model-value="page" :denied="entryDenied && {title: copy(privilegeDenied ? 'multi.privilegeTitle' : 'multi.entryTitle'), message: error || copy('multi.entryHelp')}" :ctx="ctx" :locale="ctx.locale" :pages="pages" page-width="fill" sidebar-storage-key="SynoPkgGopeed" @update:model-value="navigate">
      <template v-if="pageNotice || error || pollError || intakeNotice || saveDestination?.selection?.scope==='home'" #notices>
        <Callout v-if="intakeNotice" tone="warning" role="status">{{ intakeNotice }}</Callout>
        <HomeServiceNotice v-if="saveDestination?.selection?.scope==='home'" :ctx="ctx" @retry="readDestination" />

        <Callout v-if="pageNotice" tone="info" role="status">{{ pageNotice }}</Callout>
        <GitHubNotice v-if="errorGitHub" :failure="errorGitHub" :locale="ctx.locale" :admin="session.admin" @token="openGitHubToken" />
        <Callout
          v-else-if="error || pollError"
          tone="danger"
          :title="copy('ui.error.operationFailed')"
          ><span role="alert">{{ error || pollError }}</span></Callout
        >
      </template>
      <template #tabbar="{phone}">
        <AppTabBar v-if="phone" :items="phoneTabs" :model-value="page" :aria-label="copy('phone.tabs')" data-phone-tabs @update:model-value="navigate" />
      </template>
      <template #default="{phone}">
      <ToastProvider ref="toast">
          <DownloadFolderDialog :ctx="ctx" :destination="saveDestination" :open="folderDialogOpen" @close="finishFolder()" @changed="saveDestination=$event" @saved="finishFolder" />
          <div v-if="page === 'downloads' && phone" class="gopeed-dl-page" data-downloads data-gopeed-native>
            <PhoneDownloads ref="phoneDownloads" v-model:tab="downloadTab" v-model:text="quickText" :ctx="ctx" :rows="phoneRows" :counts="tabCounts" :task-ids="taskIds" :destination="saveDestination" :busy="busy" :loading="loading" :empty="!tasks.length" :selected="detailId" :summary="summaryText" :actions="phoneMenu" :reason="addFailure" :format="format" @open="openDetails" @act="rowAct" @menu="phoneMenuPick" @added="phoneAdded" @refused="denyAccess" @authorized="readDestination" @several="openAdd" />
          </div>
          <div v-else-if="page === 'downloads'" class="gopeed-dl-page" data-downloads data-gopeed-native>
          <DestinationRow :ctx="ctx" :destination="saveDestination" :busy="busy" @authorized="readDestination" />
          <SectionTabs v-model="downloadTab" phone="segment" :aria-label="copy('downloads.tabs')" :items="downloadTabItems" fill>
            <template #actions>
              <span v-if="summaryText" class="gopeed-summary" data-downloads-summary>{{ summaryText }}</span>
              <Button v-for="a in tabActions" :key="a.op" size="sm" :disabled="busy" :data-group-action="a.op" @click="groupAct(a.group, a.op)">{{ a.label }}</Button>
              <SearchField v-if="tasks.length" v-model="search" class="gopeed-filter" data-downloads-filter variant="filter" :placeholder="copy('ui.downloads.filter')" />
              <IconButton :disabled="busy" :label="copy('ui.action.refresh')" data-page-refresh @click="run(refreshPage)"><RefreshCw :size="16" /></IconButton>
              <Button :disabled="busy || loading" data-add-download @click="openAdd()"><Plus :size="14" />{{ copy("add.title") }}</Button>
            </template>
            <template v-for="t in downloadTabs" :key="t" #[`panel-${t}`]>
              <div class="gopeed-dl" :data-downloads-tab="t">
                <div class="gopeed-dl-scroll">
                  <LoadingArea v-if="loading && !tasks.length" :text="copy('ui.loading')" role="status" />
                  <EmptyState
                    v-else-if="!tasks.length"
                    :title="copy('ui.downloads.empty')"
                    :description="copy('ui.downloads.emptyHelp')"
                    :icon="Download"
                  />
                  <EmptyState v-else-if="!rows.length" :title="copy('ui.downloads.noMatch')" :icon="Search" />
                  <EmptyState v-else-if="!tabList.length" :title="copy('downloads.tabEmpty')" :icon="Download" />
                  <DownloadsList v-else :rows="tabList" :selected="detailId" :busy="busy" :copy="copy" :format="format" @open="openDetails" @act="rowAct" />
                </div>
                <TaskPanel v-if="detailTask && detailView" :key="detailTask.id" :ctx="ctx" :task="detailTask" :view="detailView" :method="detailTask.method" :host="detailTask.host" :tone="tone(detailTask)" :busy="busy" :stats="taskStats" :focus-files="focusFiles" @close="detailId = ''" @act="(op, file) => rowAct(detailTask!.id, op, file)" />
              </div>
            </template>
          </SectionTabs>
          </div>
          <SearchPage v-if="page === 'search'" :ctx="ctx" :tabs="searchTabs" :reason="message" :destination="saveDestination" @show-task="showTask" @solver-settings="openSearchSettings" @add-source="addSearchSource" @refused="denyAccess" @authorized="readDestination">
            <template #toolbar><IconButton :disabled="busy" :label="copy('ui.action.refresh')" data-page-refresh @click="run(refreshPage)"><RefreshCw :size="16" /></IconButton></template>
          </SearchPage>
          <SectionTabs v-if="page === 'settings'" v-model="settingsTab" data-settings :items="settingsTabs" fill keep-mounted>
            <template v-if="!phone" #actions><IconButton :disabled="busy" :label="copy('ui.action.refresh')" data-page-refresh @click="run(refreshPage)"><RefreshCw :size="16" /></IconButton></template>
            <template #panel-general>
              <SettingsPage data-settings-panel="general">
                <BackgroundSaveSettings ref="backgroundSettings" :sdk="saves" :admin="session.admin" :locale="ctx.locale" :picker="{provider:destinationAdapter.provider,keys:['save']}" @destinations="readDestination" />
                <Form v-if="session.admin" :id="coreFormId" data-section="core" @submit.prevent="applyCore">
                  <Fieldset :collapsible="false" :title="copy('ui.settings.core')" :description="copy('multi.globalEffect')">
                    <FormItem :label="copy('ui.settings.simultaneous')" :description="'1–16'"><Input v-model="core.maxRunning" :disabled="busy" type="number" min="1" max="16" required /></FormItem>
                    <FormItem :label="copy('ui.settings.connections')" :description="'1–32'"><Input v-model="core.connections" :disabled="busy" type="number" min="1" max="32" required /></FormItem>
                  </Fieldset>
                  <Fieldset :collapsible="false" :title="copy('bt.settings')" :description="copy('bt.seedHelp')">
                    <FormItem :label="copy('bt.seedRatio')" :description="copy('bt.zeroOff')"><Input v-model="core.seedRatio" :disabled="busy" type="number" min="0" max="100" step="0.1" required /></FormItem>
                    <FormItem :label="copy('bt.seedMinutes')" :description="copy('bt.zeroOff')"><Input v-model="core.seedMinutes" :disabled="busy" type="number" min="0" max="10080" :unit="copy('ui.unit.minutes')" required /></FormItem>
                    <FormItem :label="copy('bt.port')" :description="copy('bt.portHelp')"><Input v-model="core.btPort" :disabled="busy" type="number" min="1024" max="65535" required /></FormItem>
                    <FormItem :label="copy('bt.trackers')" :description="copy('bt.trackersHelp')"><Textarea v-model="trackerText" data-bt-trackers :rows="4" :disabled="busy" :placeholder="'udp://tracker.example.org:1337/announce'" spellcheck="false" /></FormItem>
                  </Fieldset>
                  <Fieldset :collapsible="false" :title="copy('ed2k.settings')" :description="copy('ed2k.portHelp')">
                    <FormItem :label="copy('ed2k.port')" :description="'1024–65535'"><Input v-model="core.ed2kPort" :disabled="busy" type="number" min="1024" max="65535" required /></FormItem>
                    <FormItem :label="copy('ed2k.udpPort')" :description="'1024–65535'"><Input v-model="core.ed2kUdpPort" :disabled="busy" type="number" min="1024" max="65535" required /></FormItem>
                  </Fieldset>
                </Form>
                <template v-if="session.admin" #footer>
                  <Button :disabled="busy || !coreChanged" @click="resetCore">{{ copy("ui.settings.reset") }}</Button>
                  <Button variant="primary" type="submit" :form="coreFormId" :disabled="busy">{{ copy("ui.settings.apply") }}</Button>
                </template>
              </SettingsPage>
            </template>
            <template #panel-media><SettingsPage data-settings-panel="media"><MediaSettings :ctx="ctx" :reason="message" :destination="saveDestination" :path="saveDestination?.destination?.path" @changed="readDestination" /></SettingsPage></template>
            <template #panel-search>
              <SettingsPage data-settings-panel="search">
                <SourceSettings :query="activeSearch(searchTabs).query" :ctx="ctx" :admin="session.admin" :reason="message" @saved="searchSourceSaved" @refused="denyAccess" @github-token="openGitHubToken" />
              </SettingsPage>
            </template>
            <template #panel-github>
              <SettingsPage data-settings-panel="github">
                <GitHubTokenSettings :ctx="ctx" :focus="githubFocus" @refused="denyAccess" />
              </SettingsPage>
            </template>
            <template #panel-access>
              <SettingsPage data-settings-panel="access">
                <PortalSettings v-if="ctx.portal" :portal="ctx.portal" />
                <SettingsSection v-if="session.admin" data-section="access" :title="copy('entry.title')">
                  <SettingsRow :label="copy('entry.privileges')" :description="copy('entry.help')">
                    <Button variant="secondary" @click="openApplicationPrivileges">{{ copy("entry.open") }}</Button>
                  </SettingsRow>
                </SettingsSection>
              </SettingsPage>
            </template>
          </SectionTabs>
          <Space v-if="page === 'plugins' && session.admin" data-plugins direction="column" spacing="relaxed">
            <div class="gopeed-toolbar"><IconButton :disabled="busy" :label="copy('ui.action.refresh')" data-page-refresh @click="run(refreshPage)"><RefreshCw :size="16" /></IconButton></div>
            <Callout
              tone="warning"
              :title="
                copy('ui.plugins.trustOnly')
              "
              >{{
                copy('multi.extensionWarning')
              }}</Callout
            >
            <SettingsSection v-if="recommended.length" data-section="recommended" :title="copy('ui.plugins.recommended')">
              <div class="gopeed-recommended" role="list">
                <div v-for="r in recommended" :key="r.key" role="listitem" class="gopeed-recommended-card" :data-recommended="r.key">
                  <img v-if="cardIcon(r.entry.icon) && !brokenIcons[r.key]" :src="cardIcon(r.entry.icon)" alt="" width="32" height="32" referrerpolicy="no-referrer" loading="lazy" @error="brokenIcons[r.key]=true" />
                  <span v-else class="gopeed-recommended-glyph" aria-hidden="true"><Plug :size="18" /></span>
                  <div class="gopeed-recommended-text">
                    <div class="gopeed-recommended-title" :title="r.entry.title">{{ r.entry.title }}</div>
                    <div class="gopeed-recommended-line">{{ copy('recommended.' + r.key) }}</div>
                  </div>
                  <Badge v-if="r.installed" tone="success">{{ copy("tools.installed") }}</Badge>
                  <Button v-else size="sm" :disabled="extensionBusy && installing !== r.entry.id" :loading="installing === r.entry.id" :aria-label="`${copy('tools.install')} ${r.entry.title}`" @click="installCatalog(r.entry)">{{ copy("tools.install") }}</Button>
                </div>
              </div>
            </SettingsSection>
            <SettingsSection data-section="installed" :title="copy('tools.installed')" :loading="pluginsLoading">
              <template #actions><Button v-if="plugins.length" :disabled="pluginsLoading || Object.keys(pluginChecks).length > 0" @click="checkPlugins()">{{ copy("ui.plugins.checkUpdates") }}</Button></template>
              <EmptyState
                v-if="!plugins.length"
                :title="copy('ui.plugins.none')"
                :description="
                  copy('ui.plugins.disabledUntilEnabled')
                "
                :icon="Plug"
              />
              <div v-if="plugins.length" role="list">
              <ListItem v-for="x in plugins" :key="x.identity" :title="x.title || x.name">
                <template #description>{{ x.description }}<template v-if="x.description && (x.author || x.repository?.url)"> · </template><template v-if="x.author">{{ x.author }}</template><template v-if="x.author && x.repository?.url"> · </template><Link v-if="x.repository?.url" :href="x.repository.url" external>{{ x.repository.url }}</Link></template>
                <template #meta>
                    <Badge tone="neutral">{{ copy("ui.plugins.currentVersion") }} {{ x.version }}</Badge>
                    <Badge :tone="x.disabled ? 'neutral' : 'success'">{{ x.disabled ? copy("ui.plugins.stateDisabled") : copy("ui.plugins.stateEnabled") }}</Badge>
                    <Badge v-if="updates[x.identity]" tone="info">{{ copy("ui.plugins.availableVersion") }} {{ updates[x.identity] }}</Badge>
                    <Badge v-if="pluginChecks[x.identity]" tone="neutral" aria-busy="true" role="status">{{ copy("ui.plugins.checking") }}</Badge>
                </template>
                  <Callout v-if="checkErrors[x.identity]" tone="warning" variant="inline">{{ checkErrors[x.identity] }}</Callout>
                  <Callout v-if="x.updateError" tone="warning">{{ message({code:x.updateError}) }}</Callout>
                <template #actions>
                  <Button
                    v-if="updates[x.identity]"
                    :disabled="extensionBusy"
                    @click="
                      ask(
                        copy('ui.plugins.confirmUpdate'),
                        copy('ui.plugins.updateHelp') + ` ${x.version} → ${updates[x.identity]} ${x.repository?.url || ''}` ,
                        () => mutatePlugin(x, 'update'),
                      )
                    "
                    >{{ copy("ui.action.update") }}</Button
                  ><Button
                    :disabled="extensionBusy"
                    @click="
                      ask(
                        x.disabled
                          ? copy('ui.plugins.confirmEnable')
                          : copy('ui.plugins.confirmDisable'),
                        copy('multi.changeEffect'),
                        () => mutatePlugin(x, 'switch'),
                      )
                    "
                    >{{
                      x.disabled ? copy("ui.action.enable") : copy("ui.action.disable")
                    }}</Button
                  ><Button
                    v-if="x.settings?.length"
                    :disabled="extensionBusy"
                    @click="editPlugin(x)"
                    >{{ copy("ui.nav.settings") }}</Button
                  ><Button
                    :disabled="extensionBusy"
                    @click="
                      ask(
                        copy('ui.plugins.confirmRemove'),
                        x.title || x.name,
                        () => mutatePlugin(x, 'delete'),
                      )
                    "
                    >{{ copy("ui.action.remove") }}</Button
                  >
                </template>
              </ListItem>
              </div>
            </SettingsSection>
            <SettingsSection data-section="catalog" :title="copy('ui.plugins.catalog')" :description="copy('source.catalogNote')" :loading="catalogLoading">
              <template v-if="catalogError">
                <Callout tone="warning" :dismissible="false">{{ catalogError }}
                  <template #actions><Button size="sm" @click="loadCatalog()">{{ copy("ui.plugins.retryCatalog") }}</Button></template>
                </Callout>
              </template>
              <Callout v-if="catalogAvailable.length" tone="warning" variant="inline">{{ copy("ui.plugins.permissions") }}</Callout>
              <Callout v-if="installing" tone="info" variant="inline" role="status">{{ copy("ui.plugins.oneAtATime") }}</Callout>
              <Callout v-if="catalogInstalled" tone="info" variant="inline">{{ catalogInstalled === 1 ? copy('ui.plugins.catalogInstalled.one') : format('ui.plugins.catalogInstalled', {count: catalogInstalled}) }}</Callout>
              <div v-if="catalogAvailable.length" role="list">
              <ListItem v-for="x in catalogAvailable" :key="x.id" :data-catalog-entry="x.id" :title="x.title">
                <template #description>{{ x.description }}<template v-if="x.description"> · </template><Link :href="x.url" external>{{ x.url }}</Link></template>
                <template #meta>
                    <Badge tone="neutral">{{ x.version }}</Badge>
                    <Badge tone="neutral">{{ copy("ui.plugins.notInstalled") }}</Badge>
                </template>
                <template #actions>
                  <Button :disabled="extensionBusy && installing !== x.id" :loading="installing === x.id" :aria-label="`${copy('tools.install')} ${x.title}`" @click="installCatalog(x)">{{ copy("tools.install") }}</Button>
                </template>
              </ListItem>
              </div>
            </SettingsSection>
            <SettingsSection data-section="install-url" :title="copy('ui.plugins.fromAddress')">
              <Form direction="vertical" @submit.prevent="installPlugin">
                <FormItem :label="copy('ui.plugins.githubUrl')" field-width="full">
                  <InputGroup><Input
                    v-model="pluginURL"
                    placeholder="https://github.com/author/extension#subfolder"
                    :aria-label="copy('ui.plugins.url')"
                    required /><Button
                    type="submit"
                    variant="primary"
                    :disabled="extensionBusy || !trust || !pluginURL"
                    >{{ copy("ui.plugins.install") }}</Button
                  ></InputGroup>
                </FormItem>
                <Checkbox
                  v-model="trust"
                  :label="
                    copy('ui.plugins.trustSource')
                  "
                />
              </Form>
            </SettingsSection>
          </Space>
          <Space v-if="page === 'binary' && session.admin" data-binary direction="column" spacing="relaxed">
            <div class="gopeed-toolbar"><IconButton :disabled="busy" :label="copy('ui.action.refresh')" data-page-refresh @click="run(refreshPage)"><RefreshCw :size="16" /></IconButton></div>
            <Card data-core :title="copy('ui.core.engine')">
              <template #header-actions><Space spacing="compact" data-badges>
                  <Badge :tone="runtime?.ready ? 'success' : 'warning'">{{ runtime?.ready ? copy("ui.core.running") : copy("ui.core.notReady") }}</Badge>
                  <Badge v-if="release && release.version !== runtime?.active?.version" tone="info">{{ copy("tools.updateAvailable") }}</Badge>
                  <Badge v-if="rowBusy.core" tone="neutral" aria-busy="true" role="status">{{ copy("ui.plugins.checking") }}</Badge>
              </Space></template>
              <div>
                <KeyValueRow :label="copy('tools.current')" :value="runtime?.active?.version || '—'" />
                <KeyValueRow :label="copy('tools.latest')" :value="release?.version || copy('tools.notChecked')" />
                <KeyValueRow v-if="rollbackTarget(runtime)" :label="copy('tools.previous')" :value="rollbackTarget(runtime).version" />
              </div>
              <Callout v-if="runtime?.error" tone="warning">{{
                message({ code: runtime.error })
              }}</Callout>
              <p v-if="release && !release.compatible">
                {{
                  copy("ui.core.integrationRequired")
                }}
              </p>
              <p v-else-if="release">
                {{
                  copy("ui.core.updateHelp")
                }}
              </p>
              <GitHubNotice v-if="rowErrors.core && githubFailure(rowCauses.core)" :failure="githubFailure(rowCauses.core)!" :locale="ctx.locale" admin @token="openGitHubToken" />
              <Callout v-else-if="rowErrors.core" tone="warning">{{ rowErrors.core }}</Callout>
              <template #footer>
                <Button
                  :disabled="rowBusy.core"
                  @click="checkCore"
                  >{{ copy("tools.check") }}</Button
                ><Button
                  v-if="release"
                  :variant="release.version === runtime?.active?.version ? 'secondary' : 'primary'"
                  :disabled="busy || !release.compatible"
                  @click="
                    ask(
                      copy('ui.core.confirmInstall'),
                      copy('ui.core.restartHelp'),
                      async () => {
                        runtime = await ctx.call('binaries.update', {
                          version: release.version,
                          sha256: release.sha256,
                          consent: true,
                        });
                        await refresh();
                        toast?.success(copy('ui.core.installed'));
                      },
                    )
                  "
                  >{{
                    release.version === runtime?.active?.version
                      ? copy("ui.core.reinstall")
                      : copy("ui.core.update")
                  }}</Button
                ><Button
                  v-if="rollbackTarget(runtime)"
                  :disabled="busy"
                  @click="
                    ask(
                      copy('ui.core.confirmRollback'),
                      copy('ui.core.restartHelp'),
                      async () => {
                        runtime = await ctx.call('binaries.rollback', {
                          consent: true,
                        });
                        await refresh();
                      },
                    )
                  "
                  >{{ copy("tools.rollback") }}</Button
                >
              </template>
            </Card>
            <SettingsSection :title="copy('tools.title')" :description="copy('tools.help')">
            <!-- Until one read succeeds nothing is known: no card says "Not installed". -->
            <Callout v-if="toolReadError" tone="warning">{{ toolReadError }}</Callout>
            <Space direction="column" :spacing="12">
            <Card v-for="tool in mediaTools" :key="tool" :title="toolNames[tool]" :data-media-tool="tool">
              <template #header-actions><Space spacing="compact" data-badges>
                  <Badge v-if="!toolSupported(tool)" tone="neutral">{{ copy("tools.unavailableModel") }}</Badge>
                  <template v-else-if="toolState(tool)?.active">
                    <Badge tone="success">{{ copy("tools.installed") }}</Badge>
                    <Badge v-if="toolChecks[tool]?.update" tone="info">{{ copy("tools.updateAvailable") }}</Badge>
                  </template>
                  <Badge v-else-if="toolChecks[tool]?.update" tone="info">{{ copy("tools.available") }}</Badge>
                  <Badge v-else-if="toolStatus" tone="neutral">{{ copy("tools.notInstalled") }}</Badge>
                  <Badge v-if="rowBusy[tool]" tone="neutral" aria-busy="true" role="status">{{ copy("ui.plugins.checking") }}</Badge>
              </Space></template>
              <p>{{ copy("tools.about." + tool) }}</p>
              <div v-if="toolSupported(tool)">
                <KeyValueRow :label="copy('tools.current')" :value="toolVersion(toolState(tool)?.active?.version) || (toolStatus ? copy('tools.notInstalled') : '—')" />
                <KeyValueRow :label="copy('tools.latest')"><Space spacing="compact" align-items="center">{{ toolChecks[tool] ? toolVersion(toolChecks[tool].latest?.version) : copy('tools.notChecked') }}<Badge v-if="toolChecks[tool] && !toolChecks[tool].update" tone="success">{{ copy('tools.upToDate') }}</Badge></Space></KeyValueRow>
                <KeyValueRow v-if="rollbackTarget(toolState(tool))" :label="copy('tools.previous')" :value="toolVersion(rollbackTarget(toolState(tool)).version)" />
              </div>
              <template v-if="toolFailure(tool)">
                <GitHubNotice v-if="githubFailure({ code: toolFailure(tool), resetAt: toolJob(tool)?.resetAt })" :failure="githubFailure({ code: toolFailure(tool), resetAt: toolJob(tool)?.resetAt })!" :locale="ctx.locale" admin @token="openGitHubToken" />
                <Callout v-else tone="warning" role="status">{{ toolFailureText(tool) }}</Callout>
              </template>
              <GitHubNotice v-if="rowErrors[tool] && githubFailure(rowCauses[tool])" :failure="githubFailure(rowCauses[tool])!" :locale="ctx.locale" admin @token="openGitHubToken" />
              <Callout v-else-if="rowErrors[tool]" tone="warning">{{ rowErrors[tool] }}</Callout>
              <template v-if="toolSupported(tool)" #footer>
                <Button :disabled="rowBusy[tool]" @click="checkTool(tool)">{{ copy("tools.check") }}</Button
                ><Button v-if="toolChecks[tool]?.update || toolPending(tool) && toolOperation(tool)!=='rollback'" :loading="toolPending(tool) && toolOperation(tool)!=='rollback'" :disabled="toolPending(tool)" @click="updateTool(tool)">{{ toolState(tool)?.active ? copy("tools.update") : copy("tools.install") }}</Button
                ><Button v-if="rollbackTarget(toolState(tool))" :loading="toolPending(tool) && toolOperation(tool)==='rollback'" :disabled="toolPending(tool)" @click="rollbackTool(tool)">{{ copy("tools.rollback") }}</Button>
              </template>
            </Card>
            </Space>
            </SettingsSection>
          </Space>
    <PreviewDialog v-if="preview" :key="preview.id + (preview.file || '')" :ctx="ctx" :name="preview.name" :size="preview.size" :path="previewPath" :token="dsmFiles.token" @close="preview = undefined" @reveal="previewReveal" />
 <BottomSheet v-if="phone" :open="!!(detailTask && detailView)" :title="detailTask?.name" data-task-sheet @update:open="(v) => { if (!v) detailId = ''; }">
      <TaskPanel v-if="detailTask && detailView" :key="detailTask.id" sheet :ctx="ctx" :task="detailTask" :view="detailView" :method="detailTask.method" :host="detailTask.host" :tone="tone(detailTask)" :busy="busy" :stats="taskStats" :focus-files="focusFiles" @close="detailId = ''" @act="(op, file) => rowAct(detailTask!.id, op, file)" />
    </BottomSheet>
    <Dialog :open="!!confirmLink" :title="copy('phone.confirmTitle')" data-confirm-link @update:open="(v) => { if (!v) confirmLink = ''; }">
      <p class="gopeed-confirm-link">{{ confirmLink }}</p>
      <template #footer>
        <Button @click="confirmLink = ''">{{ copy('add.cancel') }}</Button>
        <Button variant="primary" data-confirm-link-start @click="confirmDeepLink"><Download :size="14" />{{ copy('search.download') }}</Button>
      </template>
    </Dialog>
    <AddDownloadDialog v-if="createOpen" :shared="sharedDraft" :initial="addInitial" :initial-media-options="addMediaOptions" :ctx="ctx" :reason="addFailure" @close="createOpen = false" @added="downloadsAdded" @refused="denyAccess" :destination="saveDestination" @authorized="readDestination" @choose="chooseLater" />

    <MessageBoxProvider ref="messageBox" />
    <Dialog
      :open="!!pluginSettings"
      :title="format('ui.plugins.settingsTitle', {title: pluginSettings?.title || ''})"
      :description="copy('ui.plugins.settingsShared')"
      @update:open="
        (v) => {
          if (!v) pluginSettings = undefined;
        }
      "
      ><Form
        :id="pluginFormId"
        direction="vertical"
        @submit.prevent="savePlugin"
      >
        <Callout v-if="error" tone="danger"
          ><span role="alert">{{ error }}</span></Callout
        >
        <template
          v-for="field in pluginSettings?.settings || []"
          :key="field.name"
          ><Checkbox
            v-if="field.type === 'boolean'"
            v-model="pluginDraft[field.name]"
            :label="field.title || field.name"
          /><FormItem v-else :label="field.title || field.name" :description="field.description"
            ><Select
              v-if="field.options?.length"
              v-model="pluginDraft[field.name]"
              :options="
                field.options.map((x: any) => ({
                  label: x.label,
                  value: String(x.value),
                }))
              "
            /><Input
              v-else
              v-model="pluginDraft[field.name]"
              :aria-label="field.title || field.name"
              :type="
                field.type === 'number'
                  ? 'number'
                  : /cookie|password|token|secret/i.test(field.name)
                    ? 'password'
                    : 'text'
              "
            /></FormItem
          ></template
        >
      </Form>
      <template #footer
        ><Button @click="pluginSettings = undefined">{{
          copy("add.cancel")
        }}</Button
        ><Button
          :form="pluginFormId"
          type="submit"
          variant="primary"
          :disabled="busy"
          >{{ copy("ui.action.save") }}</Button
        ></template
      ></Dialog
    >
      </ToastProvider>
      </template>
  </AppShell>
</template>
