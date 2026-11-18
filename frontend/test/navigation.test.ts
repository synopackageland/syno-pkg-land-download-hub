import {expect,test,vi} from 'vitest'
import {revealSaved} from '../src/navigation'
import {mockHost} from './mock-host'
test('standalone navigation requests a separate DSM desktop with task ID, without path/credentials',()=>{
 const {ctx}=mockHost();ctx.host.platform=()=>({host:'standalone-web',dsmStandalone:false})
 const popup:any={opener:{},location:{href:''}}
 expect(revealSaved(ctx,'owned-task','/home/Downloads/a.pdf',popup)).toEqual({status:'requested'})
 const url=new URL(popup.location.href);expect(url.searchParams.get('gopeedReveal')).toBe('owned-task');expect(url.searchParams.get('launchApp')).toBe('SYNO.SDS.SynoPkgGopeed.Instance');expect(url.href).not.toContain('/home/Downloads');expect(popup.opener).toBeNull()
})
test('a desktop window asks File Station to select the saved file',()=>{
 const {ctx}=mockHost();const reveal=vi.spyOn(ctx.host,'revealFile')
 expect(revealSaved(ctx,'owned-task','/home/Downloads/a.pdf')).toEqual({status:'requested'});expect(reveal).toHaveBeenCalledWith('/home/Downloads/a.pdf')
})
test('blocked portal popup and missing desktop file return refusal',()=>{
 const {ctx}=mockHost();expect(revealSaved(ctx,'owned-task')).toEqual({status:'unsupported'})
 ctx.host.platform=()=>({host:'standalone-web'});expect(revealSaved(ctx,'owned-task',undefined,null)).toEqual({status:'blocked'})
})
