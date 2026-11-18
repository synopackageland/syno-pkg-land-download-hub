import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>{cleanup.splice(0).forEach(fn=>fn());vi.restoreAllMocks()})
async function settle(){await vi.dynamicImportSettled();for(let i=0;i<30;i++){await Promise.resolve();await nextTick()}}
const button=(root:ParentNode,text:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===text)
async function click(root:ParentNode,text:string){const b=button(root,text);expect(b,`button ${text}`).toBeTruthy();b!.click();await settle()}
async function input(el:HTMLInputElement|HTMLTextAreaElement,value:string){el.value=value;el.dispatchEvent(new Event('input',{bubbles:true}));await settle()}
async function openSettings(host=mockHost(true)){
 const {root}=mountApp(host.ctx,cleanup,'width:1280px;height:800px');await settle()
 await click(root,'Settings');await settingsTab(root,'search',settle);return {host,root,section:root.querySelector<HTMLElement>('[data-section="search-sources"]')!}
}
const dialog=()=>document.querySelector<HTMLElement>('[role="dialog"]')!
const field=(label:string)=>dialog().querySelector<HTMLInputElement>(`[aria-label="${label}"]`)!
const mine=(section:HTMLElement,name:string)=>[...section.querySelectorAll<HTMLElement>('[data-source]')].find(x=>x.textContent?.includes(name))!
// A source row's ⋯ menu item, opened through the menu's own trigger.
async function menuItem(row:HTMLElement,label:string){
 const trigger=row.querySelector<HTMLElement>('[data-source-menu]')!
 trigger.dispatchEvent(new MouseEvent('pointerdown',{bubbles:true,button:0}));await settle()
 if(!document.querySelector('[role="menuitem"]')){trigger.click();await settle()}
 const item=[...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(i=>i.textContent?.trim()===label)
 expect(item,`menu item ${label}`).toBeTruthy();return item!
}
async function pick(row:HTMLElement,label:string){(await menuItem(row,label)).click();await settle()}
const picker=()=>document.querySelector<HTMLElement>('[data-site-picker]')!
const siteRow=(name:string)=>[...picker().querySelectorAll<HTMLElement>('[data-catalog] tbody tr')].find(r=>r.textContent?.includes(name))!

test('my sources list each source with its kind and state',async()=>{
 const {section}=await openSettings()
 const text=mine(section,'YouTube').textContent
 expect(text).toContain('YouTube search');expect(text).toContain('Off')
 // A badge says whether the source can search; its tooltip says why.
 const status=mine(section,'Private tracker').querySelector('[data-source-status]')!
 expect(status.textContent?.trim()).toBe('Ready');expect(status.getAttribute('title')).toBe('Sign-in set')
 expect(mine(section,'LinuxTracker').querySelector('[data-source-status]')?.getAttribute('title')).toBe('Searches without an account')
 // The page lists sources; the site list opens from Add site.
 expect(section.querySelector('[data-catalog]')).toBeNull()
 expect(button(section,'Add site')?.getAttribute('data-variant')??button(section,'Add site')?.className).toMatch(/primary/)
 expect(button(section,'Add feed')).toBeTruthy()
 // The switch shows on or off by itself; its name says which source it turns on.
 const off=mine(section,'YouTube').querySelector<HTMLElement>('[role="switch"]')!
 expect(off.getAttribute('aria-label')).toBe('On YouTube')
 expect(off.getAttribute('aria-checked')).toBe('false')
 expect(mine(section,'YouTube').textContent).not.toMatch(/\bOn\b/)
 expect(section.querySelector('[data-solver-advanced] summary')?.textContent?.trim()).toBe('Cloudflare solver settings')
})

test('the site picker filters by name, badges each site and keeps unavailable ones behind a toggle',async()=>{
 const {section}=await openSettings()
 await click(section,'Add site')
 const catalog=picker().querySelector<HTMLElement>('[data-catalog]')!
 expect(catalog.textContent).not.toContain('Old Tracker')
 expect(siteRow('LinuxTracker').querySelector('[data-site-status]')?.textContent?.trim()).toBe('Ready')
 expect(siteRow('PrivateHD').querySelector('[data-site-status]')?.textContent?.trim()).toBe('Needs login')
 expect(siteRow('LinuxTracker').textContent).toContain('Added')
 await click(picker(),'Show unavailable (1)')
 const old=siteRow('Old Tracker')
 expect(old.textContent).toContain('(andmatch)');expect(old.querySelector('[data-site-status]')?.textContent?.trim()).toBe('Unavailable')
 expect(old.querySelector<HTMLButtonElement>('button')?.disabled).toBe(true)
 await input(picker().querySelector<HTMLInputElement>('input[data-catalog-filter],[data-catalog-filter] input')!,'semi')
 expect(catalog.querySelectorAll('tbody tr').length).toBe(1)
})

test('a public site turns on in one click',async()=>{
 const host=mockHost(true);host.search.sources.length=0
 const {section}=await openSettings(host)
 await click(section,'Add site')
 ;(siteRow('LinuxTracker').querySelector('button') as HTMLButtonElement).click();await settle()
 expect(host.calls.find(c=>c.method==='search.sources.save')?.params).toEqual({kind:'builtin',name:'LinuxTracker',enabled:true,definitionId:'linuxtracker'})
})

// Secrets are written, never shown: a saved one reads "Set" and an empty
// field keeps it.
test('a site that needs sign-in asks for it and keeps saved secrets',async()=>{
 const {host,section}=await openSettings()
 await pick(mine(section,'Private tracker'),'Edit')
 expect(field('Password').type).toBe('password')
 expect(field('Password').placeholder).toBe('Set')
 await input(field('Username'),'me')
 await click(dialog(),'Save')
 expect(host.calls.find(c=>c.method==='search.sources.save')?.params).toEqual({id:'private',kind:'builtin',name:'Private tracker',enabled:true,definitionId:'privatehd',values:{username:'me'}})
})

// A site that signs in asks for it before it turns on.
test('a site picked in Add site that needs sign-in asks for it first',async()=>{
 const {host,section}=await openSettings()
 await click(section,'Add site')
 ;(siteRow('Semi Open').querySelector('button') as HTMLButtonElement).click();await settle()
 expect(document.querySelector('[data-site-picker]')).toBeNull()
 expect(host.calls.some(c=>c.method==='search.sources.save')).toBe(false)
 await input(field('Cookie'),'c=1')
 await click(dialog(),'Save')
 expect(host.calls.find(c=>c.method==='search.sources.save')?.params).toEqual({kind:'builtin',name:'Semi Open',enabled:true,definitionId:'semi',values:{cookie:'c=1'}})
})

test('an invalid YAML import says why and keeps the dialog',async()=>{
 const {section}=await openSettings()
 await click(section,'Add feed')
 // Add feed offers every kind but a site from the list.
 expect([...dialog().querySelectorAll('[data-ui-tab-value]')].map(t=>t.textContent)).not.toContain('Site')
 await click(dialog(),'Cardigann YAML')
 await input(field('Name'),'Mine')
 await input(dialog().querySelector('textarea')!,'name: no id here')
 await click(dialog(),'Save')
 expect(dialog().querySelector('[role="alert"]')?.textContent).toContain('not a valid site definition')
})

test('a Torznab source needs its address and keeps its API key secret',async()=>{
 const {host,section}=await openSettings()
 await click(section,'Add feed')
 await click(dialog(),'Torznab')
 await input(field('Name'),'Jackett')
 expect(button(dialog(),'Save')!.disabled).toBe(true)
 await input(field('Address'),'http://indexer.example:9117/api')
 expect(field('API key').type).toBe('password')
 await input(field('API key'),'k')
 await click(dialog(),'Save')
 expect(host.calls.find(c=>c.method==='search.sources.save')?.params).toEqual({kind:'torznab',name:'Jackett',enabled:true,url:'http://indexer.example:9117/api',values:{apikey:'k'}})
})

test('a source turns off, tests, exports and deletes on its own row',async()=>{
 const {host,section}=await openSettings()
 const row=mine(section,'LinuxTracker')
 ;(row.querySelector('[role="switch"]') as HTMLElement).click();await settle()
 expect(host.calls.find(c=>c.method==='search.sources.enabled')?.params).toEqual({id:'linux',enabled:false})
 await pick(mine(section,'Private tracker'),'Test')
 expect(host.calls.find(c=>c.method==='search.sources.test.start')?.params).toMatchObject({id:'private',key:expect.any(String)})
 expect(mine(section,'Private tracker').textContent).toContain('Login required')
 host.search.sources.push({id:'yaml',kind:'cardigann',name:'Imported',enabled:true,configured:false,supported:true,reasons:[],settings:[]})
 await click(section,'Reload')
 const url=vi.spyOn(URL,'createObjectURL').mockReturnValue('blob:x');vi.spyOn(URL,'revokeObjectURL').mockImplementation(()=>{})
 await pick(mine(section,'Imported'),'Export')
 expect(url).toHaveBeenCalled()
 await pick(mine(section,'Imported'),'Delete')
 await click(dialog(),'Delete')
 expect(host.calls.find(c=>c.method==='search.sources.delete')?.params).toEqual({id:'yaml'})
},15_000)

test('administrators update and roll back the site list without blocking the page',async()=>{
 const host=mockHost(true);let finish!:()=>void
 const call=host.ctx.call;host.ctx.call=(m:string,p:any)=>m==='search.definitions.update'?new Promise(r=>{finish=()=>r(call(m,p))}):call(m,p)
 const {section}=await openSettings(host)
 const version=section.querySelector('[data-definitions]')!
 expect(version.textContent).toContain('43359e0');expect(version.textContent).toContain('3 of 4 sites')
 await click(section,'Update site list')
 expect(version.textContent).toContain('Updating')
 expect(button(section,'Add site')!.disabled).toBe(false)
 finish();await settle()
 expect(version.textContent).not.toContain('Updating')
 await click(section,'Restore previous')
 await click(dialog(),'Restore previous')
 expect(host.calls.some(c=>c.method==='search.definitions.rollback')).toBe(true)
})

test('other users see the site list version without update buttons',async()=>{
 const {section}=await openSettings(mockHost(false))
 expect(section.querySelector('[data-definitions]')!.textContent).toContain('43359e0')
 expect(button(section,'Update site list')).toBeUndefined()
 expect(button(section,'Restore previous')).toBeUndefined()
})

// Adding the first source from the search page returns to the same query.
test('a source added from an empty search page goes back to search',async()=>{
 const host=mockHost(true);host.search.sources.length=0
 const {root}=mountApp(host.ctx,cleanup);await settle()
 await click(root,'Search');await click(root,'Choose sources')
 await click(root,'Add site')
 ;(siteRow('LinuxTracker').querySelector('button') as HTMLButtonElement).click();await settle()
 expect(root.querySelector('[data-search-query]')).toBeTruthy()
})


test('editing an unchanged redacted address omits it, preserving private credentials',async()=>{
 const host=mockHost(true)
 host.search.sources.push({id:'torznab',kind:'torznab',url:'https://indexer.test/api?apikey=***',name:'Torznab',enabled:true,configured:true,supported:true,reasons:[],settings:[]})
 const {section}=await openSettings(host)
 await pick(mine(section,'Torznab'),'Edit')
 expect(field('Address').value).toBe('https://indexer.test/api?apikey=***')
 await input(field('Name'),'Renamed')
 await click(dialog(),'Save')
 expect(host.calls.find(c=>c.method==='search.sources.save')?.params).toEqual({id:'torznab',kind:'torznab',name:'Renamed',enabled:true})
})

test('solver URL saves per user with SDK settings controls',async()=>{
 const {host,section}=await openSettings(mockHost(false))
 const input=section.querySelector<HTMLInputElement>('[data-cloudflare-solver] input')!
 expect(input).toBeTruthy();input.value='http://solver.example:8191';input.dispatchEvent(new Event('input',{bubbles:true}));await settle()
 await click(section.querySelector('[data-cloudflare-solver]')!,'Save solver URL')
 expect(host.calls.find(c=>c.method==='search.solver.set')?.params).toEqual({url:'http://solver.example:8191'})
})

test('HTTP DSM can test sources without randomUUID and opening Settings never updates definitions',async()=>{
 const getRandomValues=crypto.getRandomValues.bind(crypto)
 vi.stubGlobal('crypto',{getRandomValues,randomUUID:undefined})
 try{
  const {host,section}=await openSettings()
  expect(host.calls.some(c=>c.method==='search.definitions.update')).toBe(false)
  await pick(mine(section,'LinuxTracker'),'Test')
  expect(host.calls.find(c=>c.method==='search.sources.test.start')?.params.key).toMatch(/^[a-f0-9]{32}$/)
  expect(mine(section,'LinuxTracker').textContent).toContain('Available')
 }finally{vi.unstubAllGlobals()}
})

test('a catalog failure leaves existing sources usable',async()=>{
 const host=mockHost(true),call=host.ctx.call
 host.ctx.call=(m:string,p:any)=>m==='search.catalog'?Promise.reject({code:'search_update_rate_limited'}):call(m,p)
 const {section}=await openSettings(host)
 expect(mine(section,'LinuxTracker')).toBeTruthy()
 expect((await menuItem(mine(section,'LinuxTracker'),'Test')).hasAttribute('data-disabled')).toBe(false)
 expect(button(section,'Add site')?.disabled).toBe(true)
})
