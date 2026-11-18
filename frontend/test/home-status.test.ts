import {expect,test,vi} from 'vitest'
import {homeDecision,checkHome,openUserHome} from '../src/home-status'
import {validSubfolder} from '../src/subfolder'

test.each([true,false])('home decisions follow this caller’s own visibility, admin=%s',admin=>{
 expect(homeDecision({admin,visible:true})).toBe('enabled')
 expect(homeDecision({admin,visible:false})).toBe('disabled')
 expect(homeDecision({admin})).toBe('unknown')
})
test('directory accounts use the appropriate home-service switch when identity type is known',()=>{
 const settings={enable:false,enable_domain:true,enable_ldap:false}
 expect(homeDecision({admin:true,kind:'local',settings})).toBe('disabled')
 expect(homeDecision({admin:true,kind:'domain',settings})).toBe('enabled')
 expect(homeDecision({admin:true,kind:'ldap',settings})).toBe('disabled')
 expect(homeDecision({admin:false,kind:'local',settings})).toBe('unknown')
 expect(homeDecision({admin:true,kind:'ldap',settings,visible:true})).toBe('enabled')
})
test('admin-only API refusal falls through to the caller’s own File Station shares and caches briefly',async()=>{
 const webApi=vi.fn(async(api:string)=>{if(api==='SYNO.Core.User.Home')throw {code:'denied'};return {shares:[{name:'home',path:'/home',isdir:true}],offset:0,total:1}})
 const ctx:any={user:{isAdmin:true},webApi,call:vi.fn(async()=>({mode:'browser-session'}))}
 expect(await checkHome(ctx)).toBe('enabled');const count=webApi.mock.calls.length
 expect(await checkHome(ctx)).toBe('enabled');expect(webApi.mock.calls.length).toBe(count)
 await checkHome(ctx,true);expect(webApi.mock.calls.length).toBeGreaterThan(count)
 expect(webApi.mock.calls.some(([api])=>api==='SYNO.Core.User.Home')).toBe(true)
})
test('ordinary user with no home sees disabled without calling the control-panel API',async()=>{
 const webApi=vi.fn(async()=>({shares:[],offset:0,total:0}))
 const ctx:any={user:{isAdmin:false},webApi,call:vi.fn(async()=>({mode:'browser-session'}))}
 expect(await checkHome(ctx)).toBe('disabled')
 expect(webApi.mock.calls.every(([api]:any)=>api!=='SYNO.Core.User.Home')).toBe(true)
 const refused:any={user:{isAdmin:false},webApi:vi.fn(async()=>{throw {code:'denied'}}),call:ctx.call}
 expect(await checkHome(refused)).toBe('unknown')
})
test('home settings button requests a DSM Control Panel jump without changing settings',()=>{
 const openURL=vi.fn(()=>({status:'requested'}));openUserHome({host:{openURL}} as any)
 const url=new URL(openURL.mock.calls[0][0] as string)
 expect(url.searchParams.get('launchApp')).toBe('SYNO.SDS.AdminCenter.Application')
 expect(url.searchParams.get('launchParam')).toContain('User.Main');expect(url.searchParams.get('launchParam')).toContain('advanced')
})
test('relative subfolders reject escapes, empty segments, invalid names and oversized UTF-8 segments',()=>{
 for(const path of ['', 'Downloads','Videos/YouTube/Channel','相簿/照片'])expect(validSubfolder(path)).toBe(true)
 for(const path of ['/home','..','a/../b','a//b','a/','a:b','a\\b','a\nb','@eaDir','x/#recycle','界'.repeat(86)])expect(validSubfolder(path),path).toBe(false)
})
