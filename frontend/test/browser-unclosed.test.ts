import {expect,test} from 'vitest'
import {createApp,h,nextTick,reactive} from 'vue'
import BrowserSearchGuide from '../src/BrowserSearchGuide.vue'
import {BrowserHelperKey} from '../src/browser-helper'
import {appCopy} from '../src/app-copy'
async function guide(unclosed:number,locale='en'){
 const state=reactive({status:'ready',version:'8.6.0',activity:'',error:'',timings:{},unclosed})
 const root=document.createElement('div');document.body.append(root)
 const app=createApp({render:()=>h(BrowserSearchGuide,{locale})});app.provide(BrowserHelperKey,{state} as any);app.mount(root);await nextTick()
 return {root,state,done:()=>{app.unmount();root.remove()}}
}
test('source tabs the browser could not close show one line outside the collapsed guide',async()=>{
 const g=await guide(2)
 try{
  const hint=g.root.querySelector('[data-browser-unclosed]')!
  expect(hint.textContent).toBe(appCopy('en','browser.unclosed').replace('{count}','2'))
  expect(hint.closest('[data-browser-steps]')).toBeNull()
  g.state.unclosed=0;await nextTick()
  expect(g.root.querySelector('[data-browser-unclosed]')).toBeNull()
 }finally{g.done()}
})
test('the hint is translated in every App catalog without trailing punctuation',()=>{
 for(const locale of ['en','zh-TW','zh-CN','ja','ko','de','fr','es','it']){
  const text=appCopy(locale,'browser.unclosed')
  expect(text,locale).toContain('{count}');expect(text,locale).not.toMatch(/[.。]$/)
  if(locale!=='en')expect(text,locale).not.toBe(appCopy('en','browser.unclosed'))
 }
 expect(appCopy('zh-TW','browser.unclosed')).toBe('有 {count} 個來源分頁無法自動關閉，請手動關閉')
})

// Ready RTO needs nothing from the user: one Ready badge, no title, steps,
// toggle or links. Until it is ready the title and the three steps show.
test('ready RTO shows only its Ready badge; the guide shows until then',async()=>{
 const g=await guide(0)
 try{
  const section=g.root.querySelector<HTMLElement>('[data-browser-guide]')!
  expect(section.dataset.open).toBe('false')
  expect(section.querySelector('[data-browser-state]')!.textContent?.trim()).toBe('Ready')
  expect(section.querySelector('[data-browser-steps]')).toBeNull()
  expect(section.querySelector('[data-browser-steps-toggle]')).toBeNull()
  expect(section.querySelector('.guide-title')).toBeNull()
  expect(section.querySelectorAll('a')).toHaveLength(0)
  expect(section.textContent?.trim()).toBe('Ready')
  for(const status of ['not-installed','disconnected']){
   g.state.status=status;await nextTick()
   expect(section.dataset.open,status).toBe('true')
   expect(section.querySelector('.guide-title')?.textContent,status).toBe(appCopy('en','browser.title'))
   expect(section.querySelector('[data-browser-state]')!.textContent?.trim(),status).toBe(appCopy('en','browser.'+status))
   expect(g.root.querySelectorAll('[data-browser-steps] li'),status).toHaveLength(3)
  }
 }finally{g.done()}
})
test('Ready is translated in every App catalog',()=>{
 const seen=new Set<string>()
 for(const locale of ['en','zh-TW','zh-CN','ja','ko','de','fr','es','it']){
  const text=appCopy(locale,'browser.readyShort')
  expect(text,locale).toBeTruthy();expect(text,locale).not.toBe('browser.readyShort');seen.add(text)
 }
 expect(appCopy('zh-TW','browser.readyShort')).toBe('就緒')
 expect(seen.size).toBeGreaterThan(5)
})
