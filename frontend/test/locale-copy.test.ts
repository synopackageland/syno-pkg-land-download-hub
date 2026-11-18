import {afterEach,expect,test} from 'vitest'
import {nextTick} from 'vue'
import {readFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}
async function open(root:ParentNode,text:string){
 const b=[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===text)
 expect(b,`button ${text}`).toBeTruthy();b!.click();await settle()
}
async function fixture(locale:string){
 const host=mockHost(true);host.ctx.locale=locale
 const {root}=mountApp(host.ctx,cleanup,'width:1350px;height:700px');await settle();return root
}
// Every UI text comes from the nine-language catalogs, not a zh-TW/English pair.
test('the App has no zh-TW/English-only text helper',()=>{
 const app=readFileSync(resolve(__dirname,'../src/App.vue'),'utf8')
 expect(app).not.toMatch(/\bt\(\s*["'`]/)
})
test('German shows German pages, core settings and extension risk',async()=>{
 const root=await fixture('de')
 for(const page of ['Downloads','Einstellungen','Erweiterungen','Kernupdates'])expect(root.querySelector('nav, aside')?.textContent).toContain(page)
 await open(root,'Einstellungen')
 expect(root.textContent).toContain('Einstellungen des Download-Kerns')
 expect(root.textContent).toContain('Gleichzeitige Downloads')
 expect(root.textContent).not.toContain('Download core settings')
 await open(root,'Erweiterungen')
 expect(root.textContent).toContain('Offizieller Erweiterungskatalog')
 expect(root.textContent).toContain('Führt JavaScript in der gemeinsamen Gopeed-Engine aus')
 expect(root.textContent).not.toContain('Runs JavaScript')
})
test('zh-CN reads Simplified Chinese, not the zh-TW text',async()=>{
 const root=await fixture('zh-CN')
 expect(root.querySelector('nav, aside')?.textContent).toContain('设置')
 await open(root,'设置')
 const access=await settingsTab(root,'access',settle)
 expect(access.textContent).toContain('应用程序权限')
 expect(access.textContent).not.toContain('應用程式權限')
})
test('zh-TW says 擴充套件 and zh-CN 扩展 for extensions',()=>{
 const tw=JSON.parse(readFileSync(resolve(__dirname,'../../i18n/messages/zh-TW.json'),'utf8')),cn=JSON.parse(readFileSync(resolve(__dirname,'../../i18n/messages/zh-CN.json'),'utf8'))
 expect(Object.values(tw).filter(v=>String(v).includes('外掛'))).toEqual([])
 expect(Object.values(cn).filter(v=>String(v).includes('插件'))).toEqual([])
})
// zh-CN uses mainland wording, not Taiwan wording written in simplified
// characters. DSM's own messages in the glossary stay as DSM writes them.
test('zh-CN keeps mainland wording',()=>{
 const cn=JSON.parse(readFileSync(resolve(__dirname,'../../i18n/messages/zh-CN.json'),'utf8')),glossary=JSON.parse(readFileSync(resolve(__dirname,'../../i18n/glossary.json'),'utf8'))
 const dsmText=new Set(glossary.terms.map((term:any)=>term.text['zh-CN']))
 const taiwan=/连线|联机|帐号|帐户|透过|存取|装置|支援|进阶|设定|程式|网路|登入|登出|资讯|讯息|档案|伺服器|预设|软体|视窗|萤幕|滑鼠|影音|停用|等候/
 expect(Object.entries(cn).filter(([,v])=>!dsmText.has(v)&&taiwan.test(String(v))).map(([k])=>k)).toEqual([])
})
// A status label fits its 140px column in a few characters or words (in
// Chromium, 16 Latin letters are cut); the whole sentence is its tooltip
// and the Details column.
test('every status label is short in all nine languages',()=>{
 const long:string[]=[]
 for(const locale of ['en','zh-TW','zh-CN','ja','ko','de','fr','it','es']){
  const catalog=JSON.parse(readFileSync(resolve(__dirname,`../../i18n/messages/${locale}.json`),'utf8'))
  for(const [key,text] of Object.entries<string>(catalog).filter(([key])=>key.startsWith('gopeed.ui.state.'))){
   const words=text.split(/\s+/).length,chars=[...text.replace(/\s/g,'')].length
   const short=locale.startsWith('zh')?chars<=4:locale==='ja'?chars<=8:locale==='ko'?words<=3&&chars<=8:words<=3&&text.length<=15
   if(!short)long.push(`${locale} ${key}: ${text}`)
  }
 }
 expect(long).toEqual([])
})
