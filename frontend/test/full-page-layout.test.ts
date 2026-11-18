import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {existsSync,readFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {mountApp} from './mount-app'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}
// jsdom measures no width: report one page width to every observed element.
function pageWidth(width:number){
 const original=globalThis.ResizeObserver
 vi.stubGlobal('ResizeObserver',class{constructor(private callback:ResizeObserverCallback){}observe(){this.callback([{contentRect:{width}} as ResizeObserverEntry],this as unknown as ResizeObserver)}unobserve(){}disconnect(){}})
// The UI applies a measured width on the next frame; run frames at once.
 const frame=globalThis.requestAnimationFrame
 vi.stubGlobal('requestAnimationFrame',(callback:FrameRequestCallback)=>{callback(0);return 0})
 cleanup.push(()=>{vi.stubGlobal('ResizeObserver',original);vi.stubGlobal('requestAnimationFrame',frame)})
}
async function open(width:number,prepare:(ctx:any)=>void){
 pageWidth(width)
 const host=mockHost(true);prepare(host.ctx)
 const {root,app}=mountApp(host.ctx,cleanup);await settle()
 return {root,app,mode:root.querySelector('[data-ui-layout-mode]')?.getAttribute('data-ui-layout-mode')}
}
const viewport=()=>document.head.querySelector<HTMLMetaElement>('meta[name="viewport"]')
const icon='webman/3rdparty/SynoPkgGopeed/images/icon_64.png'

test('a DSM desktop window keeps the window layout without a viewport or brand',async()=>{
 const {root,mode}=await open(1440,ctx=>{ctx.fullPage=false;ctx.appIcon=icon})
 expect(mode).toBe('window')
 expect(viewport()).toBeNull()
 expect(root.querySelector('[data-ui-sidebar-brand]')).toBeNull()
 expect(root.querySelector('[data-ui-sidebar-user]')).toBeNull()
})

test('a full-page launch takes the web layout, a device-width viewport and the Download Hub brand',async()=>{
 const {root,app,mode}=await open(1440,ctx=>{ctx.fullPage=true;ctx.appIcon=icon})
 expect(mode).toBe('web')
 expect(viewport()?.content).toBe('width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content')
 const brand=root.querySelector('[data-ui-sidebar-brand]')!
 expect(brand.textContent).toContain('Download Hub')
 expect(brand.querySelector('img')?.getAttribute('src')).toBe(icon)
 app.unmount()
 expect(viewport()).toBeNull()
})

// A phone must reach mobile mode: nothing in the app holds a desktop minimum width.
test('a full page on a phone lays out in mobile mode',async()=>{
 const {root,mode}=await open(390,ctx=>{ctx.fullPage=true})
 expect(mode).toBe('mobile')
 expect(root.querySelector('[data-downloads] [data-ui-segment-control]')).not.toBeNull()
 expect(root.querySelector('[data-row-key="active"]')).not.toBeNull()
})

// An older CLI passes no fullPage: the host platform still tells a DSM window apart.
test('without ctx.fullPage, a standalone DSM launch or the Portal page is full-page',async()=>{
 for(const platform of [{host:'dsm',dsmStandalone:true},{web:true,host:'standalone-web',dsmStandalone:false}]){
  const {mode}=await open(1440,ctx=>{ctx.host.platform=()=>platform})
  expect(mode,platform.host).toBe('web')
  cleanup.splice(0).forEach(fn=>fn())
 }
 const {mode}=await open(1440,()=>{})
 expect(mode).toBe('window')
})

// A full page has no DSM title bar or desktop: the sidebar shows who is signed in.
test('a full page shows the signed-in DSM account and role at the foot of the sidebar',async()=>{
 const {root}=await open(1440,ctx=>{ctx.fullPage=true})
 const user=root.querySelector('[data-ui-sidebar-user]')!
 expect(user.querySelector('[data-ui-user-name]')?.textContent).toBe('fixture-admin')
 expect(user.querySelector('[data-ui-user-detail]')?.textContent).toBe('Administrator')
})

test('a visible DSM portal banner supplies branding and account; hiding it restores the App fallback',async()=>{
 pageWidth(1440)
 const banner=document.createElement('div')
 banner.className='sds-standalone-main-window'
 banner.innerHTML='<header class="sds-standalone-main-window-header"><span class="sds-standalone-main-window-header-text">Download Hub</span><button class="sds-standalone-main-window-header__toolbar__btn">fixture-admin</button></header>'
 document.body.append(banner);cleanup.push(()=>banner.remove())
 const rect=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockReturnValue({width:1440,height:40,top:0,left:0,bottom:40,right:1440,x:0,y:0,toJSON(){}})
 cleanup.push(()=>rect.mockRestore())
 const host=mockHost(true);host.ctx.fullPage=true
 const {root}=mountApp(host.ctx,cleanup);banner.append(root);await settle()
 // Notify the SDK after moving the fixture into the DSM standalone host.
 window.dispatchEvent(new Event('resize'));await settle()
 expect(root.querySelector('.app-shell-portal-brand')).not.toBeNull()
 expect(root.querySelector('[data-ui-sidebar-user]')).toBeNull()
 banner.classList.add('sds-standalone-main-window--no-banner');await settle()
 expect(root.querySelector('.app-shell-portal-brand')).toBeNull()
 expect(root.querySelector('[data-ui-sidebar-brand]')?.textContent).toContain('Download Hub')
 expect(root.querySelector('[data-ui-user-name]')?.textContent).toBe('fixture-admin')
})

// Switching pages starts the new page at the top, as the SDK shell does.
test('a new page opens scrolled to the top',async()=>{
 const {root}=await open(1440,ctx=>{ctx.fullPage=true})
 const scroller=root.querySelector<HTMLElement>('.app-page')!
 scroller.scrollTop=400
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Settings')!.click();await settle()
 expect(scroller.scrollTop).toBe(0)
})

// The shell, layout, viewport, brand and account come from the SDK's AppShell.
test('the App renders the SDK AppShell and keeps no shell or host types of its own',()=>{
 const app=readFileSync(resolve(__dirname,'../src/App.vue'),'utf8')
 expect(app).toMatch(/<AppShell\b/)
 for(const own of ['AppLayout','AppSidebar','ensureDeviceViewport','page-scroll','gopeed-layout','<AppRoot','toFront','data-gopeed-access'])expect(app,own).not.toContain(own)
 expect(app).toMatch(/<AppShell[^>]*\bapp-root\b/)
 expect(app).toMatch(/:denied=/)
 expect(existsSync(resolve(__dirname,'../src/compat'))).toBe(false)
 expect(readFileSync(resolve(__dirname,'../src/style.css'),'utf8')).not.toMatch(/gopeed-layout|page-scroll/)
})
