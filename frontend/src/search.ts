export type SearchResult = { resultId: string; sourceId: string; title: string; size?: number; seeders?: number; leechers?: number; category?: string; date?: string; hash?: string };
export type SourcePage = { status?:string; siteURL?: string; sourceId: string; page?: number; results: SearchResult[]; morePossible?: boolean; error?: { code: string; message?:string } };
export type Call = (method: string, params: any) => Promise<any>;

import {followSearchJob,startSearchOperation,searchRequestKey,type SearchJob} from './search-jobs'

export async function searchSources(call:Call,ids:string[],query:string,onPage:(p:SourcePage)=>void,signal?:AbortSignal,onJob?:(id:string)=>void,existingId?:string,onStatus?:(job:SearchJob)=>void,category=''){
 const id=existingId??(await startSearchOperation(call,'search.jobs.start',{key:searchRequestKey(),sourceIds:ids,query,page:1,...(category?{category}:{})})).job.id
 onJob?.(id)
 return followSearchJob(call,id,{onPage,signal,onStatus})
}

const problems: Record<string, string> = {
  search_cloudflare: "cloudflare",
  search_login: "login",
  search_auth_unsupported: "login",
  search_network: "network",
  search_timeout: "timeout",
  search_dns: "dns",
  search_tls: "tls",
  search_connection: "connection",
  search_connection_reset: "reset",
  search_destination_denied: "denied",
  search_parse: "parse",
  search_empty: "empty",
  search_no_data: "noData",
  search_busy: "busy",
  search_storage_busy: "busy",
  search_tool_missing: "tool",
  search_unsupported: "unsupported",
  search_source_disabled: "disabled",
  search_job_canceled: "stopped",
  search_job_interrupted: "error",
  search_job_failed: "error",
  search_job_paused: "busy",
};

export function sourceProblem(code: string) {
  if(/^search_http_[1-5]\d{2}$/.test(code))return 'http';
  return problems[code] || "error";
}

// optional: the site works without it (it has a default, or the site needs no sign-in).
export type SourceSetting = { name: string; label: string; type: string; optional?: boolean; options?: { name?: string; label?: string; value?: string }[] };
export type SourceView = {
  browserReason?:string;
  cloudflare?: boolean;
  explicit?: boolean;
  url?: string;
  id: string;
  kind: string;
  name: string;
  enabled: boolean;
  definitionId?: string;
  configured: boolean;
  supported: boolean;
  reasons: string[];
  settings: SourceSetting[];
};
export type SourceStatus = { state: string; count: number; code?: string; message?:string };
export type SearchRow = {
  id: string;
  title: string;
  type: string | null;
  size: number | null;
  seeders: number | null;
  leechers: number | null;
  date: number | null;
  source: string;
  sourceId: string;
  // The torrent's info-hash; the same torrent from other sources joins this row.
  hash: string | null;
  sourceIds: string[];
};
export type SearchSort = { key: string; direction: "asc" | "desc" } | null;



// The search page's state lives with the App, so a query, its results and
// what was added survive switching pages.
let tabSeq=0
export function createSearchState() {
  return {
    id: "tab-"+(++tabSeq),
    // The words and category this tab last searched; its tab label.
    searched: "",
    category: "",
    // Set on a tab opened for a search the page starts when it shows it.
    pending: false,
    sources: [] as SourceView[],
    solverConfigured: false,
    siteURLs: {} as Record<string,string>,
    query: "",
    selected: null as string[] | null,
    filter: "",
    // A source chip the results are narrowed to.
    sourceFilter: "",
    sort: null as SearchSort,
    rows: [] as SearchRow[],
    held: [] as SearchRow[],
    statuses: {} as Record<string, SourceStatus>,
    added: {} as Record<string, string>,
    // Admission IDs can disappear when preparation becomes an engine task.
    // The server's stable batch key follows that transition for View.
    addedBatches: {} as Record<string, string>,
    running: false,
    jobId: "",
    jobStartedAt: "",
    generation: 0,
  };
}
export type SearchState = ReturnType<typeof createSearchState>;

export function receiveSource(s: SearchState, source: SourceView) {
  const i=s.sources.findIndex(x=>x.id===source.id);
  if(i<0)s.sources.push(source);else s.sources.splice(i,1,source);
}

// Returns the search's generation; answers carry it back.
export function beginSearch(s: SearchState, ids: string[]) {
  s.rows = [];
  s.held = [];
  s.siteURLs = {};
  s.statuses = Object.fromEntries(ids.map((id) => [id, { state: "running", count: 0 }]));
  s.running = ids.length > 0;
  return ++s.generation;
}

function resultRow(r: SearchResult, source?: SourceView): SearchRow {
  // Year 1 is an unparsed date from older helpers, not an upload two thousand years ago.
  const parsed = r.date ? Date.parse(r.date) : NaN;
  const date = parsed > Date.UTC(1971, 0) ? parsed : NaN;
  return {
    id: r.resultId,
    title: r.title,
    type: r.category ?? null,
    size: r.size ?? null,
    seeders: r.seeders ?? null,
    leechers: r.leechers ?? null,
    date: Number.isNaN(date) ? null : date,
    source: source?.name ?? "",
    sourceId: r.sourceId,
    hash: r.hash && /^[0-9a-f]{40}$/.test(r.hash) ? r.hash : null,
    sourceIds: [r.sourceId],
  };
}

export function receivePage(s: SearchState, p: SourcePage, generation = s.generation) {
  if (generation !== s.generation || !(p.sourceId in s.statuses)) return;
  if(p.siteURL && /^https?:\/\//.test(p.siteURL))s.siteURLs[p.sourceId]=p.siteURL;
  const source = s.sources.find((x) => x.id === p.sourceId);
  if(source && p.error?.code==='search_cloudflare'){
   source.cloudflare=true;
  }
  const existing=new Set([...s.rows,...s.held].map(r=>r.id));
  // A torrent already listed gains this source; only an info-hash joins rows.
  const byHash=new Map([...s.rows,...s.held].filter(r=>r.hash).map(r=>[r.hash!,r]));
  const rows:SearchRow[]=[];
  for(const row of p.results.map((r) => resultRow(r, source))){
   if(existing.has(row.id))continue;
   const same=row.hash?byHash.get(row.hash):undefined;
   if(same){
    if(!same.sourceIds.includes(row.sourceId))same.sourceIds.push(row.sourceId);
    if(row.seeders!==null&&(same.seeders===null||row.seeders>same.seeders)){same.seeders=row.seeders;same.leechers=row.leechers}
    continue;
   }
   if(row.hash)byHash.set(row.hash,row);
   rows.push(row);
  }
  // Past a screenful, late answers wait behind "N new results" instead of
  // moving the list someone is already reading.
  if ((s.sort || s.rows.length >= searchEnough) && s.rows.length) s.held.push(...rows);
  else s.rows.push(...rows);
  s.statuses[p.sourceId] = p.error
    ? { state: sourceProblem(p.error.code), count: 0, code: p.error.code, message:p.error.message }
    : { state: p.results.length ? "done" : "empty", count: p.results.length };
  s.running = Object.values(s.statuses).some((x) => x.state === "running");
}

// Enough to choose from: the remaining sources can be stopped instead of waited for.
export const searchEnough = 100;
export function enoughFound(s: SearchState) {
  // Sources waiting on the browser are still to come, as running ones are.
  const list = Object.values(s.statuses);
  if (!list.some((x) => x.state === "running" || x.state === "browser")) return false;
  return s.rows.length + s.held.length >= searchEnough || list.filter((x) => x.state === "done").length >= 3;
}

export function mergeHeld(s: SearchState) {
  s.rows.push(...s.held.splice(0));
}

export type CatalogSite = { cloudflare?: boolean; id: string; name: string; type: string; supported: boolean; reasons: string[]; settings: SourceSetting[] };
export type Catalog = {
  commit: string;
  total: number;
  supported: number;
  rollbackAvailable: boolean;
  updatedAt?: string;
  sites: CatalogSite[];
};

// Values that sign in are written, never shown back.
export function secretSetting(s: SourceSetting) {
  return s.type === "password" || /cookie|api_?key|pass|token|secret/i.test(s.name);
}

// The fields a source asks for: a site definition's own settings, or the
// key a Torznab or RSS source signs in with.
export function sourceFields(kind: string, settings: SourceSetting[], copy: (key: string) => string): SourceSetting[] {
  if (kind === "torznab") return [{ name: "apikey", label: copy("sources.apikey"), type: "password" }];
  if (kind === "rss") return [{ name: "cookie", label: copy("sources.cookie"), type: "password" }];
  if (kind === "builtin" || kind === "cardigann") return settings.filter((s) => s.type === "text" || s.type === "password").map(s=>s);
  return [];
}

// The user's explicit selection, including an empty selection, wins. Seeded
// Cloudflare sources stay available but are not implicitly searched without a solver.
export function defaultSourceIDs(s:SearchState,solverConfigured:boolean):string[]{
 return (s.selected??s.sources.filter(x=>x.enabled&&x.supported&&(!x.cloudflare||x.explicit||solverConfigured)).map(x=>x.id)).filter(id=>s.sources.some(x=>x.id===id&&x.enabled&&x.supported))
}

// The result categories; values match the backend and the results' type.
export const searchCategories=['movie','series','music','software','book','anime','other'] as const

// Each search keeps its own tab; the sources and their choice carry over.
export const maxSearchTabs=5
export type SearchTabs={tabs:SearchState[];active:string}
export function createSearchTabs():SearchTabs{const s=createSearchState();return {tabs:[s],active:s.id}}
export function activeSearch(t:SearchTabs){return t.tabs.find(x=>x.id===t.active)??t.tabs[0]}
export function searchUsed(s:SearchState){return Object.keys(s.statuses).length>0||s.rows.length>0||!!s.jobId}
function carried(from:SearchState){const s=createSearchState();s.sources=from.sources;s.solverConfigured=from.solverConfigured;s.selected=from.selected?[...from.selected]:null;return s}

// Opens a tab for the words typed in the active one, which gets back the
// words it searched. Beyond the limit the oldest idle tabs close.
export function openSearchTab(t:SearchTabs,closed?:(tab:SearchState)=>void){
 const from=activeSearch(t),tab=carried(from)
 tab.query=from.query;tab.category=from.category;tab.pending=true
 from.query=from.searched
 t.tabs.push(tab);t.active=tab.id
 while(t.tabs.length>maxSearchTabs){
  const i=t.tabs.findIndex(x=>x!==tab&&!x.running);if(i<0)break
  closed?.(t.tabs.splice(i,1)[0])
 }
 return tab
}
export function closeSearchTab(t:SearchTabs,id:string){
 const i=t.tabs.findIndex(x=>x.id===id);if(i<0)return
 const [tab]=t.tabs.splice(i,1)
 if(!t.tabs.length)t.tabs.push(carried(tab))
 if(t.active===id)t.active=t.tabs[Math.min(i,t.tabs.length-1)].id
 return tab
}

// A category hides results of another known type; unknown types stay, as a
// source that cannot name types may still have searched that category.
export function shownRows<T extends {type:string|null}>(rows:T[],category:string){return category?rows.filter(r=>!r.type||r.type===category):rows}

// A feed lists its latest items, so only RSS searches may leave out words.
export function queryOptional(sources:SourceView[],ids:string[]){return ids.length>0&&ids.every(id=>sources.find(x=>x.id===id)?.kind==='rss')}

// Result order for the sort menu; null keeps the order sources answered in.
export const searchSorts=['seeders','size','date','title'] as const
export function sortRows<T extends SearchRow>(rows:T[],sort:SearchSort):T[]{
 if(!sort)return rows
 const k=sort.key as keyof SearchRow,dir=sort.direction==='asc'?1:-1
 return [...rows].sort((a,b)=>{
  const x=a[k],y=b[k]
  if(x===null||x===undefined)return y===null||y===undefined?0:1
  if(y===null||y===undefined)return -1
  return (typeof x==='string'?x.localeCompare(String(y)):Number(x)-Number(y))*dir
 })
}
// Seeders at a glance: none red, a few amber, enough green.
export function seederTone(n:number|null){return n===null?'':n<=0?'danger':n<5?'warning':'success'}
// What a source chip offers: sign in, the browser, or a new try.
export function chipAction(x:SourceStatus|undefined):''|'signin'|'browser'|'retry'{
 if(!x)return ''
 if(x.state==='login')return 'signin'
 if(x.state==='cloudflare')return 'browser'
 if(['network','timeout','dns','connection','busy','error','http','stopped'].includes(x.state))return 'retry'
 return ''
}
// What the results area shows when it has no rows.
export function emptyKind(s:SearchState):'start'|'searching'|'none'|'login'|'failed'|''{
 const list=Object.values(s.statuses)
 if(s.rows.length||s.held.length)return ''
 if(!list.length)return 'start'
 if(list.some(x=>x.state==='running'||x.state==='browser'))return 'searching'
 const problems=list.filter(x=>!['done','empty','noData'].includes(x.state))
 if(!problems.length)return 'none'
 if(problems.length===list.length)return problems.every(x=>x.state==='login')?'login':'failed'
 return 'none'
}
// Cloudflare sources left out of a search because no browser or solver can
// pass the check; they show "Needs browser" instead of failing slowly.
export function skippedSources(s:SearchState,ids:string[],assist:boolean){
 if(assist||s.selected!==null)return []
 return s.sources.filter(x=>x.enabled&&x.supported&&x.cloudflare&&!ids.includes(x.id)).map(x=>x.id)
}

// A site that asks for a value it cannot do without: its sign-in.
export function needsSignIn(settings:SourceSetting[]){return settings.some(s=>!s.optional&&(s.type==='text'||s.type==='password'))}
// One word for a site or source: whether it can search as it is.
export type SiteStatus='ready'|'browser'|'login'|'unavailable'
export function siteStatus(x:{supported:boolean;cloudflare?:boolean;settings:SourceSetting[];configured?:boolean}):SiteStatus{
 if(!x.supported)return 'unavailable'
 if(needsSignIn(x.settings)&&!x.configured)return 'login'
 if(x.cloudflare)return 'browser'
 return 'ready'
}
export const siteTone:Record<SiteStatus,'success'|'warning'|'neutral'|'info'>={ready:'success',browser:'info',login:'warning',unavailable:'neutral'}

// A phone browser cannot run Remote Tab Opener, so a search there leaves out
// the sources that only answer through it (docs/cloudflare-search.md): the
// ones marked as needing the browser, unless an external solver passes them.
export function browserOnly(x:SourceView,solverConfigured:boolean){return !!x.cloudflare&&!solverConfigured}
export function phoneSourceIDs(s:SearchState,ids:string[]){
 return ids.filter(id=>{const x=s.sources.find(v=>v.id===id);return !x||!browserOnly(x,s.solverConfigured)})
}
// How many enabled sources a phone leaves out: they still work on a computer.
export function desktopOnlyCount(s:SearchState){
 return s.sources.filter(x=>x.enabled&&x.supported&&browserOnly(x,s.solverConfigured)).length
}

// The phone's one status line: how many sources this search asked, the
// results so far, whether any still answer and how many need the user.
export function searchSummary(s:SearchState){
 const list=Object.values(s.statuses)
 return {
  sources:list.length,
  results:s.rows.length+s.held.length,
  running:s.running||list.some(x=>x.state==='running'||x.state==='browser'),
  problems:list.filter(x=>!['done','empty','noData','running','browser','stopped'].includes(x.state)).length,
 }
}
// Phone filters (category, sources, sort, a source's results) the user set
// away from the defaults; the options button then shows a dot.
export function searchTuned(s:SearchState){return !!s.category||s.selected!==null||!!s.sort||!!s.sourceFilter}
// The phone shows the results filter field only for a long list.
export const phoneFilterFrom=20
