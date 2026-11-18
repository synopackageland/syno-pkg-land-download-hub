import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.unstubAllGlobals()})
async function settle(){await vi.dynamicImportSettled();for(let i=0;i<30;i++){await Promise.resolve();await nextTick()}}
const buttons=(root:ParentNode,text:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].filter(b=>b.textContent?.trim()===text)
async function click(root:ParentNode,text:string){const b=buttons(root,text)[0];expect(b,`button ${text}`).toBeTruthy();b.click();await settle()}
async function openSearch(host=mockHost(true)){
 const {root}=mountApp(host.ctx,cleanup,'width:1280px;height:800px');await settle()
 await click(root,'Search');return {host,root}
}
async function type(root:ParentNode,query:string){
 const field=root.querySelector<HTMLInputElement>('[data-search-query] input')!
 field.value=query;field.dispatchEvent(new Event('input',{bubbles:true}));await settle()
}
async function submit(root:ParentNode){root.querySelector<HTMLButtonElement>('button[data-search-submit]')!.click();await settle()}
const titles=(root:ParentNode)=>[...root.querySelectorAll('[data-search-row] [data-search-title]')].map(r=>r.textContent?.trim())
const chip=(root:ParentNode,name:string)=>[...root.querySelectorAll<HTMLElement>('[data-search-chip]')].find(c=>c.textContent?.includes(name))!
// Chooses an option through a UI Select's own trigger and listbox.
async function choose(select:Element,label:string){
 select.querySelector<HTMLElement>('[role="combobox"]')!.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true,button:0,pointerType:'mouse'}));await settle()
 const option=[...document.querySelectorAll<HTMLElement>('[role="option"]')].find(o=>o.textContent?.trim()===label)
 expect(option,`${label} option`).toBeTruthy()
 for(const t of ['pointerdown','pointerup'])option!.dispatchEvent(new PointerEvent(t,{bubbles:true,button:0,pointerType:'mouse'}));option!.click();await settle()
}

test('HTTP DSM can start a search without crypto.randomUUID',async()=>{
 const getRandomValues=crypto.getRandomValues.bind(crypto)
 vi.stubGlobal('crypto',{getRandomValues,randomUUID:undefined})
 const {host,root}=await openSearch()
 await type(root,'the cycle');await submit(root)
 expect(host.calls.find(c=>c.method==='search.jobs.start')?.params.key).toMatch(/^[a-f0-9]{32}$/)
 expect(titles(root).length).toBe(6)
})

test('a local exception is shown as its real cause instead of a network failure',async()=>{
 const host=mockHost(true),call=host.ctx.call
 host.ctx.call=async(method:string,params?:any)=>{if(method==='search.jobs.start')throw new TypeError('crypto.randomUUID is not a function');return call(method,params)}
 const {root}=await openSearch(host)
 await type(root,'the cycle');await submit(root)
 expect(root.querySelector('[data-search-statuses]')?.textContent).not.toContain('Network error')
 root.querySelector<HTMLButtonElement>('[data-search-statuses] button')!.click();await settle()
 expect(root.querySelector('[data-search-reason]')?.textContent).toContain('crypto.randomUUID is not a function')
})

test('Search is the second page',async()=>{
 const host=mockHost(true),{root}=mountApp(host.ctx,cleanup);await settle()
 const nav=[...root.querySelectorAll('nav button, aside button')].map(b=>b.textContent?.trim()).filter(Boolean)
 expect(nav.slice(0,2)).toEqual(['Downloads','Search'])
})

// Every enabled source is chosen at first and all are asked at once.
test('a search asks every enabled source at once and shows each answer',async()=>{
 const {host,root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const asked=host.calls.filter(c=>c.method==='search.jobs.start').map(c=>c.params.sourceIds)
 expect(asked).toEqual([['linux','private','feed']])
 expect(titles(root).length).toBe(6)
 const statuses=root.querySelector('[data-search-statuses]')!.textContent
 expect(statuses).toContain('LinuxTracker');expect(chip(root,'LinuxTracker').querySelector('.search-chip-state')?.textContent).toBe('3');expect(statuses).toContain('Login required')
})

test('the first source answer replaces the loading mask',async()=>{
 const host=mockHost(true);host.search.hold.add('feed')
 const {root}=await openSearch(host)
 await type(root,'ubuntu');await submit(root)
 expect(titles(root).length).toBe(3)
 expect(root.querySelector('[data-search-statuses]')!.textContent).toContain('Searching')
 host.search.slow.feed();await new Promise(r=>setTimeout(r,550));await settle()
 expect(titles(root).length).toBe(6)
})

test('a source status opens its reason',async()=>{
 const {root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const status=[...root.querySelectorAll<HTMLButtonElement>('[data-search-statuses] button')].find(b=>b.textContent?.includes('Private tracker'))!
 ;[...status.querySelectorAll<HTMLElement>('span')].find(x=>x.textContent?.trim()==='Login required')!.click();await settle()
 expect(status.getAttribute('aria-expanded')).toBe('true')
 expect(root.querySelector('[data-search-reason]')?.textContent).toContain('sign-in')
})

// A row is two lines: the title (cut after two lines, whole on hover) and one
// meta line; unknown values are left out rather than shown as dashes.
test('rows show the title on two lines and only the values a source gave',async()=>{
 const {root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const rows=[...root.querySelectorAll('[data-search-row]')]
 const first=rows.find(r=>r.textContent?.includes('linux 24.04'))!,third=rows.find(r=>r.textContent?.includes('linux 22.04'))!
 expect(first.querySelector('[data-search-title]')?.getAttribute('title')).toContain('a very long release name')
 expect(first.querySelector('.search-meta')?.textContent).toMatch(/^Video·1\.5 GB·[^·]+·↑40 ↓—·LinuxTracker$/)
 expect(first.querySelector('[data-search-peers] b')?.getAttribute('data-tone')).toBe('success')
 expect(third.querySelector('.search-meta')?.textContent?.trim()).toBe('LinuxTracker')
 // One secondary Download button per row.
 expect(first.querySelectorAll('button')).toHaveLength(1)
 expect(first.querySelector('[data-search-download]')?.textContent?.trim()).toBe('Download')
})

test('downloading keeps the search and shows the task link',async()=>{
 const {host,root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const row=root.querySelector('[data-search-row]')!
 ;(row.querySelector('button[data-search-download]') as HTMLButtonElement).click();await settle()
 expect(host.calls.filter(c=>c.method==='search.download').map(c=>c.params)).toEqual([{resultId:'linux-0'}])
 expect(root.querySelector('[data-search-results]')).toBeTruthy()
 // The button becomes the way to the new task.
 expect(row.querySelector('[data-search-download]')).toBeNull()
 await click(row,'Added · View')
 await vi.waitFor(()=>expect(root.querySelector('[data-downloads-list]')).toBeTruthy())
 const selected=root.querySelector('[data-downloads-list] [aria-current="true"]')
 expect(selected?.textContent).toContain('linux-0')
 await click(root,'Search')
 expect((root.querySelector('[data-search-query] input') as HTMLInputElement).value).toBe('ubuntu')
 expect(titles(root).length).toBe(6)
})

test('View selects the engine task after its real preparation ID has disappeared',async()=>{
 const host=mockHost(true),call=host.ctx.call
 const key='search-fixture-owned',prepare='prepare-fixture-owned',engine='engine-fixture-owned'
 host.ctx.call=async(method:string,params?:any)=>{
  if(method==='search.download'){
   host.tasks.push({id:prepare,status:'preparing',preparation:true,publication:{batch:key},meta:{opts:{name:'fixture.txt',path:'/home/Downloads'}},progress:{downloaded:0,speed:0}})
   return {results:[{key,kind:'engine',status:'created',ids:[prepare]}]}
  }
  return call(method,params)
 }
 const {root}=await openSearch(host)
 await type(root,'fixture');await submit(root)
 const row=root.querySelector('[data-search-row]')!
 ;(row.querySelector('[data-search-download]') as HTMLButtonElement).click();await settle()
 expect(host.tasks.some(t=>t.id===prepare)).toBe(true)
 host.tasks.splice(host.tasks.findIndex(t=>t.id===prepare),1,{id:engine,status:'running',publication:{batch:key},meta:{opts:{name:'fixture.txt',path:'/home/Downloads'}},progress:{downloaded:8,speed:1}})
 await click(row,'Added · View')
 await vi.waitFor(()=>expect(root.querySelector('[data-downloads-list] [aria-current="true"]')?.getAttribute('data-row-key')).toBe(engine))
 expect(root.querySelector('[data-task-panel]')?.getAttribute('aria-label')).toBe('fixture.txt')
},60_000)

test('without a save location the download asks where first',async()=>{
 const host=mockHost(true);await host.ctx.call('delegate.revoke')
 const {root}=await openSearch(host)
 await type(root,'ubuntu');await submit(root)
 ;(root.querySelector('[data-search-row] button[data-search-download]') as HTMLButtonElement).click();await settle()
 expect(host.calls.some(c=>c.method==='search.download')).toBe(false)
 expect(document.body.textContent).toContain('Save downloads to')
})

// A sorted list keeps its rows still; new answers wait behind a prompt.
test('new answers to a sorted list wait to be merged',async()=>{
 const host=mockHost(true);host.search.hold.add('feed')
 const {root}=await openSearch(host)
 await type(root,'ubuntu');await submit(root)
 await choose(root.querySelector('[data-search-sort]')!,'Largest')
 host.search.slow.feed();await new Promise(r=>setTimeout(r,550));await settle()
 expect(titles(root).length).toBe(3)
 await click(root,'3 new results')
 expect(titles(root).length).toBe(6)
})

test('the result filter narrows the rows',async()=>{
 const {root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const filter=root.querySelector<HTMLInputElement>('input[data-search-filter],[data-search-filter] input')!
 filter.value='feed';filter.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 expect(titles(root).length).toBe(3)
})

test('with no sources the page offers to choose some',async()=>{
 const host=mockHost(true);host.search.sources.length=0
 const {root}=await openSearch(host)
 expect(root.textContent).toContain('No source is turned on')
 await click(root,'Choose sources')
 expect(root.querySelector('[data-section="search-sources"]')).toBeTruthy()
})

// Sources that exist but are all off are no search bar with a disabled button.
test('with every source off the page says so instead of a dead search bar',async()=>{
 const host=mockHost(true);for(const x of host.search.sources)x.enabled=false
 const {root}=await openSearch(host)
 expect(root.querySelector('[data-search-query]')).toBeNull()
 expect(root.textContent).toContain('No source is turned on')
 await click(root,'Choose sources')
 expect(root.querySelector('[data-section="search-sources"]')).toBeTruthy()
})

test('a search that finds nothing points to the sources',async()=>{
 const host=mockHost(true);for(const id of ['linux','private','feed'])host.search.errors[id]='search_empty'
 const {root}=await openSearch(host)
 expect(root.querySelector('[data-search-manage]')).toBeNull()
 await type(root,'nothing');await submit(root)
 expect(root.textContent).toContain('Try other keywords, or turn on more sources')
 await click(root,'Choose sources')
 expect(root.querySelector('[data-section="search-sources"]')).toBeTruthy()
})

// The real component polls the same durable job and renders fast partial pages.
test('fast job pages reach the grid in under one second while a source is pending',async()=>{
 const host=mockHost(true);host.search.hold.add('feed')
 const {root}=await openSearch(host);await type(root,'x')
 const start=performance.now();await submit(root)
 expect(titles(root).length).toBe(3);expect(performance.now()-start).toBeLessThan(1000)
 expect(root.querySelector('[data-search-statuses]')?.textContent).toContain('Searching')
 expect(host.calls.filter(c=>c.method==='search.jobs.start')).toHaveLength(1)
 expect(host.calls.some(c=>['search.stream','search.batch','search.query'].includes(c.method))).toBe(false)
})

test('a browser refusal offers one retry and details without an unrelated site or solver action',async()=>{
 const host=mockHost(true);host.search.errors.private='search_cloudflare'
 const call=host.ctx.call;host.ctx.call=async(m:string,p:any)=>{
  const out=await call(m,p)
  if(m==='search.jobs.pages')for(const page of out.items)page.siteURL='https://source.example/search/ubuntu'
  return out
 }
 const {root}=await openSearch(host);await type(root,'ubuntu');await submit(root)
 const status=[...root.querySelectorAll<HTMLButtonElement>('[data-search-statuses] button')].find(x=>x.textContent?.includes('Private tracker'))!;status.click();await settle()
 const reason=root.querySelector('[data-search-reason]')!
 expect(reason.querySelector('[data-open-site]')).toBeNull()
 expect(reason.textContent).not.toContain('Cloudflare solver settings')
 expect(buttons(reason,'Retry')).toHaveLength(1)

})

test.each(['search_empty','search_no_data'])('browser %s renders a neutral badge with codes only in advanced details',async code=>{
 const host=mockHost(true);host.search.errors.private=code
 const {root}=await openSearch(host);await type(root,'none');await submit(root)
 const status=[...root.querySelectorAll<HTMLButtonElement>('[data-search-statuses] button')].find(x=>x.textContent?.includes('Private tracker'))!
 expect(status.innerHTML).not.toContain('danger');expect(status.textContent).not.toContain(code)
 status.click();await settle()
 expect(root.querySelector('[data-search-reason] details code')?.textContent).toBe(code)
 expect(root.querySelector('[data-search-reason] details')?.hasAttribute('open')).toBe(false)
})

// The category goes with the search and hides results of another known type.
test('a category is searched and filters the results',async()=>{
 const {host,root}=await openSearch()
 const select=root.querySelector('[data-search-category]')!
 expect(select.textContent).toContain('All categories')
 await choose(select,'Series')
 await type(root,'ubuntu');await submit(root)
 expect(host.calls.find(c=>c.method==='search.jobs.start')?.params.category).toBe('series')
 // Per source: one series and one unknown; the movie is hidden.
 expect(titles(root).every(t=>!/24\.04/.test(t??''))).toBe(true)
 expect(titles(root).length).toBe(4)
})

test('without a category the search sends none',async()=>{
 const {host,root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 expect('category' in host.calls.find(c=>c.method==='search.jobs.start')!.params).toBe(false)
})

// Each search keeps its tab; closing one shows a neighbour.
test('a second search opens a tab and the first keeps its results',async()=>{
 const {host,root}=await openSearch()
 await type(root,'first');await submit(root)
 await type(root,'second');await submit(root)
 const tabs=()=>[...root.querySelectorAll('[data-search-tabs] [data-tag]')]
 expect(tabs().map(t=>t.textContent?.trim())).toEqual(['first','second'])
 expect(titles(root)[0]).toContain('second')
 ;(tabs()[0] as HTMLElement).click();await settle()
 expect(titles(root)[0]).toContain('first')
 expect(root.querySelector<HTMLInputElement>('[data-search-query] input')!.value).toBe('first')
 expect(host.calls.filter(c=>c.method==='search.jobs.start').length).toBe(2)
 root.querySelector<HTMLElement>('[data-search-tabs] [data-tag-remove]')!.click();await settle()
 expect(tabs().map(t=>t.textContent?.trim())).toEqual(['second'])
 expect(titles(root)[0]).toContain('second')
})

// A feed lists its latest items without words; other sources still need them.
test('RSS alone searches without words',async()=>{
 const submitButton=(root:ParentNode)=>root.querySelector<HTMLButtonElement>('button[data-search-submit]')!
 const {root}=await openSearch()
 expect(submitButton(root).disabled).toBe(true)
 cleanup.splice(0).forEach(fn=>fn())
 const host=mockHost(true)
 for(const x of host.search.sources)if(x.kind!=='rss')x.enabled=false
 const opened=await openSearch(host)
 expect(opened.root.querySelector<HTMLInputElement>('[data-search-query] input')!.placeholder).toBe('Keywords (optional)')
 expect(submitButton(opened.root).disabled).toBe(false)
 await submit(opened.root)
 expect(host.calls.find(c=>c.method==='search.jobs.start')?.params).toMatchObject({query:'',sourceIds:['feed']})
 expect(opened.root.querySelector('[data-search-tabs] [data-tag]')?.textContent?.trim()).toBe('Latest items')
})

// The page leads with the search bar: a search box with its magnifier, the
// browser guide with its setup steps while RTO is not ready, the result tabs
// and filter on one row. The page's refresh sits on the guide's line.
test('the search bar leads the page and the result tabs share a row with the filter',async()=>{
 const {root}=await openSearch()
 const guide=root.querySelector<HTMLElement>('[data-browser-guide]')!
 expect(guide.dataset.open).toBe('true')
 expect(guide.querySelector('[data-page-refresh]')).not.toBeNull()
 expect(root.querySelector('[data-ui-page-header]')).toBeNull()
 expect(root.querySelector('[data-search-query] input[type="search"]')).not.toBeNull()
 expect(root.querySelector('[data-search-query] svg')).not.toBeNull()
 await type(root,'ubuntu');await submit(root)
 const bar=root.querySelector('[data-search-results-bar]')!
 expect(bar.querySelector('[data-search-tabs]')).not.toBeNull()
 expect(bar.querySelector('input[data-search-filter],[data-search-filter] input')).not.toBeNull()
})

test('a connection reset is explicit and never offers browser Retry',async()=>{
 const host=mockHost(true);host.search.errors.private='search_connection_reset'
 const {root}=await openSearch(host);await type(root,'ubuntu');await submit(root)
 const status=[...root.querySelectorAll<HTMLButtonElement>('[data-search-statuses] button')].find(x=>x.textContent?.includes('Private tracker'))!
 expect(status.textContent).toContain('Connection reset');status.click();await settle()
 const reason=root.querySelector('[data-search-reason]')!
 expect(reason.textContent).toContain('possibly by a network block')
 expect(buttons(reason,'Retry')).toHaveLength(0)
})

test('a chip with results narrows the list to its source and back',async()=>{
 const {root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const main=chip(root,'LinuxTracker').querySelector<HTMLButtonElement>('button')!
 main.click();await settle()
 expect(main.getAttribute('aria-pressed')).toBe('true')
 expect(titles(root).length).toBe(3);expect(titles(root).every(t=>t?.includes('linux'))).toBe(true)
 main.click();await settle()
 expect(main.getAttribute('aria-pressed')).toBe('false');expect(titles(root).length).toBe(6)
})
test('a chip that needs sign-in offers it and opens the source settings',async()=>{
 const {root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 const action=chip(root,'Private tracker').querySelector<HTMLButtonElement>('[data-chip-action]')!
 expect(action.textContent?.trim()).toBe('Sign in')
 action.click();await settle()
 expect(root.querySelector('[data-section="search-sources"]')).toBeTruthy()
})
test('a source that failed can be searched again on its own',async()=>{
 const host=mockHost(true);host.search.errors.linux='search_timeout'
 const {root}=await openSearch(host)
 await type(root,'ubuntu');await submit(root)
 expect(titles(root).length).toBe(3)
 const action=chip(root,'LinuxTracker').querySelector<HTMLButtonElement>('[data-chip-action]')!
 expect(action.textContent?.trim()).toBe('Retry')
 delete host.search.errors.linux
 action.click();await settle()
 await vi.waitFor(()=>expect(titles(root).length).toBe(6))
 expect(host.calls.filter(c=>c.method==='search.jobs.start').map(c=>c.params.sourceIds)).toEqual([['linux','private','feed'],['linux']])
})
test('every source failing, or all needing sign-in, says so in the results area',async()=>{
 const host=mockHost(true);host.search.errors.linux='search_login';host.search.errors.feed='search_login'
 const {root}=await openSearch(host)
 await type(root,'ubuntu');await submit(root)
 expect(root.querySelector('[data-search-results]')?.textContent).toContain('Sign in to search')
 cleanup.splice(0).forEach(fn=>fn())
 const other=mockHost(true);other.search.errors.linux='search_timeout';other.search.errors.feed='search_dns'
 const opened=await openSearch(other)
 await type(opened.root,'ubuntu');await submit(opened.root)
 expect(opened.root.querySelector('[data-search-results]')?.textContent).toContain('No source could search')
})
test('before a search the results area invites one',async()=>{
 const {root}=await openSearch()
 expect(root.querySelector('[data-search-results]')?.textContent).toContain('Enter keywords and search')
})
test('without browser assist a Cloudflare site is skipped and shows Needs browser',async()=>{
 const host=mockHost(true);host.search.sources.push({id:'cf',kind:'builtin',name:'Guarded site',enabled:true,definitionId:'guarded',configured:false,supported:true,reasons:[],settings:[],cloudflare:true} as any)
 const {root}=await openSearch(host)
 expect(root.querySelector('[data-assist-off]')?.textContent).toContain('Browser assist off')
 expect(root.querySelector('[data-assist-notice]')?.textContent).toContain('skipped until browser assist is on')
 await type(root,'ubuntu');await submit(root)
 expect(host.calls.find(c=>c.method==='search.jobs.start')?.params.sourceIds).toEqual(['linux','private','feed'])
 const guarded=chip(root,'Guarded site')
 expect(guarded.textContent).toContain('Needs browser')
 expect(guarded.querySelector('[data-chip-action]')?.textContent?.trim()).toBe('Open with browser')
})
test('the sort menu orders the results',async()=>{
 const {root}=await openSearch()
 await type(root,'ubuntu');await submit(root)
 await choose(root.querySelector('[data-search-sort]')!,'Most seeders')
 const peers=[...root.querySelectorAll('[data-search-row]')].map(r=>r.querySelector('[data-search-peers] b')?.textContent??'')
 expect(peers.slice(0,4)).toEqual(['↑40','↑40','↑33','↑33'])
 expect(peers.slice(4)).toEqual(['',''])
})

// A source list that failed to load says so, instead of "no sources".
test('a failed source list offers another try',async()=>{
 const host=mockHost(true),call=host.ctx.call;let fail=true
 host.ctx.call=async(method:string,params?:any)=>{if(method==='search.sources.list'&&fail)throw {code:'network',message:'NAS busy'};return call(method,params)}
 const {root}=await openSearch(host)
 expect(root.querySelector('[data-search-load-error]')).toBeTruthy()
 expect(root.textContent).not.toContain('No source is turned on')
 fail=false
 await click(root,'Retry')
 expect(root.querySelector('[data-search-load-error]')).toBeNull()
 expect(root.querySelector('[data-search-query]')).toBeTruthy()
})
