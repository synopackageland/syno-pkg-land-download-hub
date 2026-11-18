import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {readFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const clean:(()=>void)[]=[]
afterEach(()=>clean.splice(0).forEach(f=>f()))
async function settle(){for(let i=0;i<40;i++){await Promise.resolve();await nextTick()}}
async function open(label:string,prepare?:(host:ReturnType<typeof mockHost>)=>void){
 const host=mockHost();prepare?.(host);const {root}=mountApp(host.ctx,clean);await settle()
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)!.click();await settle()
 return root
}
// Extensions is a column of SettingsSections; Settings is tabs of SettingsPages
// whose core settings are DSM Fieldsets, as Control Panel draws them.
test('every block on the extensions page is a SettingsSection',async()=>{
 const root=await open('Extensions')
 const sections=[...root.querySelectorAll('[data-plugins] [data-section]')]
 expect(sections.length).toBeGreaterThan(1)
 for(const section of sections){
  const body=section.querySelector('.ui-settings-section-body')
  expect(body,section.textContent!).not.toBeNull()
  expect(body!.closest('[data-section]')).toBe(section)
 }
 expect(root.querySelector('[data-plugins] h2')).toBeNull()
})
test('every settings tab is a SettingsPage and the core settings are Fieldsets',async()=>{
 const root=await open('Settings')
 expect([...root.querySelectorAll('[data-settings] [data-ui-tab-value]')].map(t=>t.getAttribute('data-ui-tab-value'))).toEqual(['general','search','media','github','access'])
 const page=root.querySelector('[data-settings-panel="general"]')!
 expect(page.querySelectorAll('[data-section="core"] [data-ui-fieldset]')).toHaveLength(3)
 expect(page.querySelector('h2')).toBeNull()
})
test('core settings fields are FormItems with 128px number fields',async()=>{
 const root=await open('Settings')
 const form=root.querySelector('[data-settings-panel="general"] form:has(input[type="number"])')!
 expect(form.querySelectorAll('[data-form-item]')).toHaveLength(8)
 for(const input of form.querySelectorAll('input[type="number"]'))expect(input.className).toContain('w-32')
})
test('the core and each media tool are Cards',async()=>{
 const root=await open('Core updates')
 expect(root.querySelector('[data-core]')!.textContent).toContain('Gopeed engine')
 const cards=[...root.querySelectorAll('[data-core],[data-media-tool]')]
 expect(cards.length).toBeGreaterThan(1)
 for(const card of cards){expect(card.className).not.toContain('tool-card');expect(card.getAttribute('data-layout')).toBe('regular')}
 expect(root.querySelector('[data-binary] h2')).toBeNull()
})
test('extension sections load with SectionSkeleton',async()=>{
 const root=await open('Extensions',host=>{const call=host.ctx.call;host.ctx.call=(m:string,p:any)=>m.startsWith('gopeed.plugins.')?new Promise(()=>{}):call(m,p)})
 expect(root.querySelector('.section-loading')).toBeNull()
 for(const key of ['installed','catalog'])expect(root.querySelector(`[data-section="${key}"] [data-ui-skeleton-line]`),key).not.toBeNull()
})
test('App CSS does not restyle labels, cap form widths or draw its own cards',()=>{
 const css=readFileSync(resolve(__dirname,'../src/style.css'),'utf8')
 expect(css).not.toMatch(/label[^{]*\{[^}]*display:\s*grid/)
 expect(css).not.toMatch(/max-width:\s*(400|600|640|720)px/)
 for(const legacy of ['.tool-card','.section-loading','fieldset','legend','[data-settings] h2','.section-head'])expect(css).not.toContain(legacy)
})
// Pages stack their sections in a UI Space; the App's CSS lays out no page.
test.each([['Extensions','[data-plugins]'],['Core updates','[data-binary]']])('%s stacks %s in a UI Space',async(label,page)=>{
 const root=await open(label);const el=root.querySelector<HTMLElement>(page)!
 expect(el.style.flexDirection).toBe('column')
 const css=readFileSync(resolve(__dirname,'../src/style.css'),'utf8')
 for(const rule of ['.page-stack','.downloads','.footnote','.settings-page','.plugins-page','.binary-page','.card-list','.file-link','.install-line'])expect(css).not.toContain(rule)
})
test('the tool cards stack with a UI Space',async()=>{
 const root=await open('Core updates');const el=root.querySelector<HTMLElement>('[data-media-tool]')!.parentElement!
 expect(el.style.flexDirection).toBe('column');expect(el.style.gap).toBe('12px')
})
