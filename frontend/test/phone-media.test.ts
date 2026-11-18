import {afterEach,expect,test} from 'vitest'
import {createApp,h,nextTick} from 'vue'
import {AppRoot} from '@synopackageland/ui'
import PhoneDownloads from '../src/PhoneDownloads.vue'
import AddDownloadDialog from '../src/AddDownloadDialog.vue'
import {defaultMediaOptions} from '../src/media-options'
import {mockHost} from './mock-host'
const cleanup:(()=>void)[]=[]
afterEach(()=>cleanup.splice(0).forEach(fn=>fn()))
async function settle(){for(let i=0;i<50;i++){await Promise.resolve();await nextTick()}}
async function fixture(){
 const host=mockHost(false),root=document.createElement('div');document.body.append(root)
 const transferred:any[]=[]
 const app=createApp({render:()=>h(AppRoot,{style:'height:844px;width:390px'},()=>h(PhoneDownloads,{ctx:host.ctx,rows:[],tab:'all',counts:{all:0,active:0,done:0,failed:0},taskIds:[],destination:{key:'save',status:'active',selection:{scope:'home',root:'/home',generation:1},destination:{id:'fixture-grant',key:'save',path:'/home/Downloads',username:'normal1',lifetime:'until-revoked'}},busy:false,loading:false,empty:true,selected:'',summary:'',actions:[],reason:(e:any)=>e.code,format:()=>'',onSeveral:(...args:any[])=>transferred.push(args)}))})
 app.config.idPrefix='phone-media-test';app.mount(root);cleanup.push(()=>{app.unmount();root.remove()});await settle()
 return {host,root,transferred}
}
async function options(root:HTMLElement){root.querySelector<HTMLButtonElement>('[data-phone-options]')!.click();await settle();return root.querySelector<HTMLElement>('[data-phone-media-options]')!}
function input(root:HTMLElement,value:string){const field=root.querySelector<HTMLInputElement>('[data-phone-link] input,input[data-phone-link]')!;field.value=value;field.dispatchEvent(new Event('input',{bubbles:true}))}

test('phone options submit choices for the next batch and then restore defaults',async()=>{
 const {host,root}=await fixture();expect(host.calls.some(c=>c.method==='media.settings.get')).toBe(false)
 const form=await options(root);expect(form.textContent).toContain('Advanced media options')
 form.querySelector<HTMLElement>('[role=checkbox]')!.click();await settle();form.querySelector<HTMLElement>('[data-media-options] [role=checkbox]')!.click();await settle()
 root.querySelector<HTMLButtonElement>('[data-phone-options-apply]')!.click();await settle();input(root,'https://video.example/one');await settle();root.querySelector('[data-phone-add]')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 const batches=()=>host.calls.filter(c=>c.method==='downloads.create_many');expect(batches()[0].params.mediaOptions.embedThumbnail).toBe(true)
 expect(host.calls.some(c=>c.method==='media.settings.save')).toBe(false)
 input(root,'https://video.example/two');await settle();root.querySelector('[data-phone-add]')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 expect(batches()[1].params).not.toHaveProperty('mediaOptions')
})
test('the several-links action carries the selected choices into the full add flow',async()=>{
 const {root,transferred}=await fixture();input(root,'https://video.example/one');await settle();const form=await options(root)
 form.querySelector<HTMLElement>('[role=checkbox]')!.click();await settle();form.querySelector<HTMLElement>('[data-media-options] [role=checkbox]')!.click();await settle();root.querySelector<HTMLButtonElement>('[data-phone-several]')!.click();await settle()
 expect(transferred).toHaveLength(1);expect(transferred[0][0]).toBe('https://video.example/one');expect(transferred[0][1].embedThumbnail).toBe(true)
})
test('the full add flow preserves incoming phone choices when defaults finish reading',async()=>{
 const host=mockHost(false),root=document.createElement('div');document.body.append(root)
 const choice={...defaultMediaOptions(),embedThumbnail:true,format:'480'}
 const app=createApp({render:()=>h(AppRoot,{style:'height:844px;width:390px'},()=>h(AddDownloadDialog,{ctx:host.ctx,initial:'https://video.example/one',initialMediaOptions:choice,reason:(e:any)=>e.code,destination:{key:'save',status:'active',selection:{scope:'home',root:'/home',generation:1},destination:{id:'fixture-grant',key:'save',path:'/home/Downloads',username:'normal1',lifetime:'until-revoked'}}}))})
 app.config.idPrefix='phone-batch-test';app.mount(root);cleanup.push(()=>{app.unmount();root.remove()});await settle()
 root.querySelector('form.add-download')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));await settle()
 expect(host.calls.find(c=>c.method==='downloads.create_many')?.params.mediaOptions).toEqual(choice)
})
