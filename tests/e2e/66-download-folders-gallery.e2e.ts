// Added for the release acceptance pass; intentionally not run during development.
import {test} from '@e2e-dev/web'
import {expect} from 'e2e'
import {call,data,goTo,openDownloadHub,text} from './support/download-hub.ts'

test('rules use relative paths without changing the main destination',{session:'normal1',timeout:120_000},async({app})=>{
 const page=await openDownloadHub(app),before=await data(page,'downloads.location')
 test.skip(before.status!=='active','Select an allowed destination for this acceptance account first')
 let id=''
 try{
  const rule=await data(page,'media.rules.save',{host:'gallery-fixture.example',path:'Pictures/Collections',options:{format:'best'}});id=rule.id
  const after=await data(page,'downloads.location')
  expect(after.destination).toEqual(before.destination);expect(after.subfolder).toBe(before.subfolder)
  for(const path of ['/home','../outside','Pictures//Images','Pictures/../Images','bad:name'])expect((await call(page,'media.rules.save',{host:'gallery-fixture.example',path,options:{format:'best'}})).success).toBe(false)
 }finally{if(id)await call(page,'media.rules.delete',{id})}
})

test('first-download folder confirmation continues the pending link once',{session:'normal1',timeout:120_000},async({app})=>{
 const page=await openDownloadHub(app)
 // UI contract fixture: no destination mutation and no real download admission.
 let selected=false,admissions=0;let submitted:any
 await page.route('**/api.cgi',async route=>{
  let body:any;try{body=route.request().postDataJSON()}catch{return route.continue()}
  const method=body?.method
  if(!['downloads.location','downloads.folder.set','downloads.create_many'].includes(method))return route.continue()
  let result:any
  if(method==='downloads.folder.set')selected=true
  if(method==='downloads.create_many'){admissions++;submitted=body.params;result={results:body.params.items.map((it:any)=>({key:it.key,status:'created',ids:['fixture-preparation']}))}}
  else result={key:'save',status:selected?'active':'missing',subfolder:'Downloads',selection:{scope:'home',root:'/home',generation:1},destination:selected?{id:'fixture-home',key:'save',path:'/home',username:'fixture-user',lifetime:'until-revoked'}:null}
  await route.fulfill({json:{success:true,data:result}})
 })
 await page.reload();await page.locator('[data-add-download]').click()
 await page.locator('form.add-download textarea').fill('https://example.com/pending.zip')
 await page.locator('form.add-download').evaluate(form=>(form as HTMLFormElement).requestSubmit())
 await page.locator('[data-download-folder-dialog]').waitFor({state:'visible'})
 expect(admissions).toBe(0)
 await page.locator('[data-confirm-download-folder]').click()
 await expect.poll(()=>admissions).toBe(1);expect(submitted.path).toBe('/home/Downloads');expect(submitted.items[0].url).toBe('https://example.com/pending.zip')
})

test('gallery-dl availability follows the package architecture',{session:'admin',timeout:120_000},async({app})=>{
 const page=await openDownloadHub(app);await goTo(page,'Core updates','核心更新')
 const status=await data(page,'media.tools.status'),row=page.locator('[data-media-tool="gallery-dl"]')
 await row.waitFor({state:'visible'})
 if(status.arch==='arm64'){
  expect(await row.innerText()).toMatch(/Not available on this model|此機型不支援/)
  expect(await row.getByRole('button').count()).toBe(0)
  expect((await call(page,'media.tools.check',{tool:'gallery-dl'})).success).toBe(false)
 }else expect(await row.getByRole('button',{name:text('Check for updates','檢查更新'),exact:true}).count()).toBe(1)
})
