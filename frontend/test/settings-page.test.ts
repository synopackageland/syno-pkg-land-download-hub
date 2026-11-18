import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(f=>f()))
async function settle(){for(let i=0;i<40;i++){await Promise.resolve();await nextTick()}}
const buttons=(root:ParentNode,label:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].filter(b=>b.textContent?.trim()===label)
async function settings(admin=true){
 const host=mockHost(admin),{root}=mountApp(host.ctx,clean);await settle()
 buttons(root,'Settings')[0].click();await settle()
 return {host,root,page:root.querySelector<HTMLElement>('[data-settings-panel="general"]')!}
}

// Background saves and settings are one page, the sidebar's last entry.
test('background saving is a section of the settings page, which the sidebar lists last',async()=>{
 const {root,page}=await settings()
 expect(buttons(root,'Background saving')).toHaveLength(0)
 const nav=['Downloads','Extensions','Core updates','Settings'].map(label=>buttons(root,label)[0])
 for(let i=1;i<nav.length;i++)expect(nav[i-1].compareDocumentPosition(nav[i])&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
 const first=page.querySelector('.background-save-settings')!
 expect(first.compareDocumentPosition(page.querySelector('[data-section="core"]')!)&Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
 expect(first.querySelector('[data-save-row="folder"]')!.textContent).toContain('/home/Downloads')
})

// The SDK chooses and approves the folder in one form; no App workaround.
test('an administrator approves the save folder once with the DSM password',async()=>{
 const {host,page}=await settings()
 expect(page.querySelector('input[type="password"]:not([data-github-token])')).toBeNull()
 expect(page.querySelectorAll('[data-save-row]')).toHaveLength(1)
 expect(page.querySelector('[data-save-row="scope"]')).toBeNull()
 buttons(page,'Choose folder')[0].click();await settle()
 const password=page.querySelector<HTMLInputElement>('input[type="password"]:not([data-github-token])')!
 expect(password).not.toBeNull()
 expect(host.calls.filter(c=>c.method==='gopeed.destination')).toHaveLength(0)
 password.value='fixture-password';password.dispatchEvent(new Event('input'));await settle()
 page.querySelector('form')!.dispatchEvent(new Event('submit'));await settle()
 expect(host.calls.filter(c=>c.method==='gopeed.destination')).toEqual([{method:'gopeed.destination',params:{key:'save',path:'/home'}}])
 expect(page.querySelector('[data-save-row="folder"]')!.textContent).toContain('/home')
 expect(page.querySelector('input[type="password"]:not([data-github-token])')).toBeNull()
})

test('an ordinary user sees the save folder, not the scope or the switch',async()=>{
 const {page}=await settings(false)
 expect(page.querySelector('[data-save-row="folder"]')!.textContent).toContain('/home/Downloads')
 expect(page.querySelector('[data-save-row="scope"]')).toBeNull()
 expect(buttons(page,'Disable background saves')).toHaveLength(0)
 expect(page.textContent).not.toContain('Download core settings')
})

test('the page header refresh rereads background saves',async()=>{
 const {host,root}=await settings()
 const count=()=>host.calls.filter(c=>c.method==='downloads.location').length,before=count()
 root.querySelector<HTMLButtonElement>('button[aria-label="Refresh"]')!.click();await settle()
 expect(count()).toBeGreaterThan(before)
 expect([...root.querySelectorAll('button')].filter(b=>/Refresh/.test((b.textContent||'')+(b.getAttribute('aria-label')||'')))).toHaveLength(1)
})

// One label column for the whole page: the SDK rows and the App's rows.
// Reset and Apply sit in the page's footer, which stays at the bottom.
test('every settings row shares the label column and Apply stays in view',async()=>{
 const {root,page}=await settings()
 expect(page.querySelectorAll('.field-stack,.field-pair,.form-actions,.destination-settings')).toHaveLength(0)
 const core=page.querySelector('[data-section="core"]')!
 expect(core.querySelector('form [data-direction="vertical"]')).toBeNull()
 // Each field is named by its row label, not a hidden copy of it.
 for(const input of core.querySelectorAll('input'))expect(document.getElementById(input.getAttribute('aria-labelledby')!.split(' ')[0])?.textContent?.trim()).toBeTruthy()
 const footer=buttons(page,'Apply')[0].closest('.ui-settings-page-footer')!
 expect(footer).not.toBeNull()
 expect(buttons(footer,'Reset')).toHaveLength(1)
 expect(buttons(footer,'Apply')[0].getAttribute('form')).toBe(core.getAttribute('id'))
 const access=await settingsTab(root,'access',settle)
 expect(buttons(access.querySelector('[data-section="access"] [data-settings-label]')!.parentElement!,'Open Control Panel')).toHaveLength(1)
})

test('settings have no save history or app-wide disable action',async()=>{
 const {host,page}=await settings()
 expect(page.textContent).not.toContain('Save history')
 expect(page.querySelector('[data-save-operations],[data-save-danger]')).toBeNull()
 expect(buttons(page,'Disable background saves')).toHaveLength(0)
 expect(host.calls.filter(c=>c.method==='delegate.operations.list')).toHaveLength(0)
})

// every core field has a description under its control, as the
// first two do, so a wrapped label never spaces one section's fields apart
// from another's; notes such as "0 = off" are descriptions, not labels.
test('every core settings field has its own description and a label without notes',async()=>{
 for(const locale of ['en','zh-TW','de']){
  const host=mockHost(true);host.ctx.locale=locale
  const {root}=mountApp(host.ctx,clean);await settle()
  ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.querySelector('svg')&&/Settings|設定|Einstellungen/.test(b.textContent||''))!.click();await settle()
  const items=[...root.querySelectorAll('[data-section="core"] [data-form-item]')]
  expect(items).toHaveLength(8)
  for(const item of items){
   expect(item.querySelector('input,textarea')!.getAttribute('aria-describedby'),locale+': '+item.textContent).toBeTruthy()
   expect(item.querySelector('[data-form-label]')!.textContent,locale).not.toMatch(/[(（]/)
  }
  clean.splice(0).forEach(f=>f())
 }
})

// The tabs stay mounted, so a draft typed in one tab survives a visit to another.
test('a settings draft survives switching tabs',async()=>{
 const {root}=await settings()
 const github=await settingsTab(root,'github',settle)
 const token=github.querySelector<HTMLInputElement>('input[data-github-token]')!
 token.value='github_pat_draft';token.dispatchEvent(new Event('input'));await settle()
 await settingsTab(root,'access',settle)
 expect(github.isConnected).toBe(true)
 const back=await settingsTab(root,'github',settle)
 expect(back.querySelector<HTMLInputElement>('input[data-github-token]')!.value).toBe('github_pat_draft')
})

test('the core settings sections are fixed, without a chevron',async()=>{
 const {page}=await settings()
 const core=page.querySelector('[data-section="core"]')!
 expect(core.querySelectorAll('[data-ui-fieldset]')).toHaveLength(3)
 expect(core.querySelector('[data-fieldset-header],[aria-expanded]')).toBeNull()
})
