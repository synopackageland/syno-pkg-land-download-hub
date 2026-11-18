import {afterEach,expect,test,vi} from 'vitest'
import {nextTick} from 'vue'
import {mountApp,settingsTab} from './mount-app'
import {mockHost} from './mock-host'
import {portalMessages} from '@synopackageland/sdk'
import type {PortalStatus} from '@synopackageland/sdk'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<12;i++){await Promise.resolve();await nextTick()}}

// The CLI's DSM window gives ctx.portal when dsm-package.yaml declares portal;
// the settings page shows the SDK's Application Portal panel for it.
test('settings show the SDK Application Portal panel with the direct link',async()=>{
 const host=mockHost(true)
 const status={state:'active',alias:'download-hub',url:'https://nas.example/download-hub/',canManage:false,controlPanel:false,defaultAlias:'download-hub'} as const
 host.ctx.portal={messages:{title:'Application Portal',link:'Direct link:'},status:vi.fn(async()=>status),apply:vi.fn(),disable:vi.fn(),openControlPanel:vi.fn()}
 const {root}=mountApp(host.ctx,cleanup);await settle()
 ;[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()==='Settings')!.click();await settle()
 await settingsTab(root,'access',settle)
 expect(root.querySelector('[data-settings-panel] a[href="https://nas.example/download-hub/"]')).not.toBeNull()
})

const button=(root:HTMLElement,label:string)=>[...root.querySelectorAll<HTMLButtonElement>('button')].find(b=>b.textContent?.trim()===label)
async function executorPortal(admin=true,failFirst=false,ownPage=false){
 const host=mockHost(admin)
 if(ownPage){
  const previous=window.location.href
  window.history.replaceState(null,'','/download-hub/')
  cleanup.push(()=>window.history.replaceState(null,'',previous))
  host.ctx.fullPage=true
 }
 let status:PortalStatus=ownPage
  ? {state:'active',entry:'registered',alias:'download-hub',url:`${window.location.origin}/download-hub/`,canManage:admin,controlPanel:false,defaultAlias:'download-hub'}
  : {state:'inactive',entry:'unregistered',canManage:admin,controlPanel:false,defaultAlias:'download-hub'}
 const apply=vi.fn(async(alias:string)=>status={...status,state:'active',alias,url:`https://nas.example/${alias}/`})
 const register=vi.fn(async()=>{
  if(failFirst){failFirst=false;throw {code:'PORTAL_FAILED'}}
  return status={...status,entry:'registered'}
 })
 const unregister=vi.fn(async()=>status={...status,state:'inactive',entry:'unregistered',alias:undefined,url:undefined})
 host.ctx.portal={messages:portalMessages('en'),status:vi.fn(async()=>status),apply,disable:vi.fn(),register,unregister,openControlPanel:vi.fn()}
 const {root}=mountApp(host.ctx,cleanup);await settle()
 button(root,'Settings')!.click();await settle()
 await settingsTab(root,'access',settle)
 return {root,host,apply,register,unregister}
}

test('an administrator enables the executor entry and applies the Download Hub alias in one action',async()=>{
 const {root,register,apply,unregister}=await executorPortal()
 expect(root.querySelector('#portal-alias')).toBeNull()
 button(root,'Enable entry')!.click();await settle()
 expect(register).toHaveBeenCalledOnce()
 expect(apply).toHaveBeenCalledExactlyOnceWith('download-hub')
 expect(root.querySelector<HTMLAnchorElement>('[data-settings-panel] a[href="https://nas.example/download-hub/"]')).not.toBeNull()
 expect(root.querySelector<HTMLInputElement>('#portal-alias')?.value).toBe('download-hub')
 button(root,'Remove entry')!.click();await settle()
 expect(unregister).toHaveBeenCalledOnce()
 expect(button(root,'Enable entry')).toBeDefined()
 expect(root.querySelector('#portal-alias')).toBeNull()
})

test('a refused portal registration keeps the error and can be retried without applying the alias',async()=>{
 const {root,register,apply}=await executorPortal(true,true)
 button(root,'Enable entry')!.click();await settle()
 expect(root.textContent).toContain('DSM did not accept the Application Portal change. Try again')
 expect(apply).not.toHaveBeenCalled()
 button(root,'Enable entry')!.click();await settle()
 expect(register).toHaveBeenCalledTimes(2)
 expect(apply).toHaveBeenCalledExactlyOnceWith('download-hub')
})

test('ordinary users ask an administrator and cannot change the executor portal entry',async()=>{
 const {root,register,apply,unregister}=await executorPortal(false)
 expect(root.textContent).toContain('Ask an administrator to set this up')
 expect(button(root,'Enable entry')).toBeUndefined()
 expect(button(root,'Remove entry')).toBeUndefined()
 expect(root.querySelector('#portal-alias')).toBeNull()
 expect(register).not.toHaveBeenCalled()
 expect(apply).not.toHaveBeenCalled()
 expect(unregister).not.toHaveBeenCalled()
})

test('removing the entry from its own page confirms, cancels safely and keeps desktop recovery after navigation',async()=>{
 const {root,unregister}=await executorPortal(true,false,true)
 button(root,'Remove entry')!.click();await settle()
 const dialog=root.querySelector<HTMLElement>('[role="dialog"]')!
 expect(dialog.textContent).toContain('This page will stop working. To enable the entry again, open this app from the DSM desktop and go to Settings > Application Portal. Remove the entry?')
 expect(unregister).not.toHaveBeenCalled()
 button(dialog,'Cancel')!.click();await settle()
 expect(root.querySelector('[role="dialog"]')).toBeNull()
 expect(unregister).not.toHaveBeenCalled()
 button(root,'Remove entry')!.click();await settle()
 button(root.querySelector<HTMLElement>('[role="dialog"]')!,'Remove entry')!.click();await settle()
 expect(unregister).toHaveBeenCalledOnce()
 expect(root.textContent).toContain('Entry removed. This page no longer works.')
 expect(root.textContent).toContain('Settings > Application Portal > Enable entry')
 const desktop=root.querySelector<HTMLAnchorElement>('[data-settings-panel] a[href="/"]')!
 expect(desktop.textContent).toContain('Open DSM desktop')
 expect(desktop.target).toBe('_blank')
 expect(button(root,'Enable entry')).toBeUndefined()
 button(root,'Downloads')!.click();await settle()
 button(root,'Settings')!.click();await settle()
 expect(root.textContent).toContain('Entry removed. This page no longer works.')
 expect(button(root,'Enable entry')).toBeUndefined()
})

test('a lost removal reply keeps cautious desktop recovery while pending and after rejection',async()=>{
 const {root,unregister}=await executorPortal(true,false,true)
 let reject!:(reason:unknown)=>void
 unregister.mockImplementationOnce(()=>new Promise<PortalStatus>((_,fail)=>{reject=fail}))
 button(root,'Remove entry')!.click();await settle()
 button(root.querySelector<HTMLElement>('[role="dialog"]')!,'Remove entry')!.click();await settle()
 expect(root.textContent).toContain('This page may no longer work. Open this app from the DSM desktop and check Settings > Application Portal')
 expect(button(root,'Enable entry')).toBeUndefined()
 reject({code:'PORTAL_FAILED'});await settle()
 expect(root.textContent).toContain('This page may no longer work. Open this app from the DSM desktop and check Settings > Application Portal')
 expect(root.textContent).not.toContain('Entry removed. This page no longer works.')
 expect(root.querySelector('[data-settings-panel] a[href="/"]')).not.toBeNull()
 button(root,'Downloads')!.click();await settle()
 button(root,'Settings')!.click();await settle()
 expect(root.textContent).toContain('This page may no longer work. Open this app from the DSM desktop and check Settings > Application Portal')
 expect(unregister).toHaveBeenCalledOnce()
})
