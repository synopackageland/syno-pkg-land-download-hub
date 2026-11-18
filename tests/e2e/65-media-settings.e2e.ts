// Local specs only in this change; device execution belongs to the release pass.
import {test} from '@e2e-dev/web'
import {expect} from 'e2e'
import {call,data,goTo,openDownloadHub,text} from './support/download-hub.ts'

async function mediaPage(app:Parameters<typeof openDownloadHub>[0]){
 const page=await openDownloadHub(app);await goTo(page,'Settings','設定');await page.locator('[data-ui-tab-value=media]').click();await page.locator('[data-media-settings]').waitFor({state:'visible'});return page
}

test('media defaults are private to the signed-in user and accept choices only',{session:'normal1',timeout:120_000},async({app})=>{
 const page=await mediaPage(app),before=await data(page,'media.settings.get')
 try{
  const changed={...before.defaults,format:before.defaults.format==='720'?'1080':'720',nfo:true}
  expect((await call(page,'media.settings.save',changed)).success).toBe(true)
  expect((await data(page,'media.settings.get')).defaults).toEqual(changed)
  expect((await call(page,'media.settings.save',{...changed,exec:'echo denied'})).success).toBe(false)
  expect((await call(page,'media.cookies.status',{owner:'another-user'})).success).toBe(false)
 }finally{await call(page,'media.settings.save',before.defaults)}
})

test('the media cookie UI imports metadata and deletes a fixture domain',{session:'normal1',timeout:120_000},async({app})=>{
 const page=await mediaPage(app),before=await data(page,'media.cookies.status')
 // Import replaces a user's jar; never overwrite an existing private import.
 if(before.domains?.length)return
 const content='# Netscape HTTP Cookie File\n.media-fixture.example\tTRUE\t/\tTRUE\t0\tid\tfixture-value\n'
 try{
  await page.locator('[data-media-import]').click();await page.locator('[data-media-cookie-text]').fill(content)
  await page.getByRole('dialog').filter({has:page.locator('[data-media-cookie-text]')}).getByRole('button',{name:text('Import cookies','匯入 cookies'),exact:true}).click()
  const status=await data(page,'media.cookies.status');expect(status.domains).toEqual([{domain:'media-fixture.example',count:1}]);expect(JSON.stringify(status)).not.toContain('fixture-value')
  expect(await page.locator('[data-media-settings]').innerText()).not.toContain('fixture-value')
 }finally{await call(page,'media.cookies.delete',{domain:'media-fixture.example'})}
})

test('follow requires consent and a long interval; rules and follows remain owner scoped',{session:'normal1',timeout:120_000},async({app})=>{
 const page=await mediaPage(app)
 expect((await call(page,'media.follows.add',{url:'https://video.example/channel',path:'/home',consent:false})).success).toBe(false)
 expect((await call(page,'media.follows.add',{url:'https://video.example/channel',path:'/home',consent:true,intervalHours:1})).success).toBe(false)
 expect((await call(page,'media.rules.save',{host:'video.example',path:'/home',options:{format:'--exec'}})).success).toBe(false)
 const state=await data(page,'media.settings.get');expect(state).toHaveProperty('rules');expect(state).toHaveProperty('follows')
 await page.locator('[data-media-add-follow]').click();const dialog=page.getByRole('dialog').filter({has:page.locator('input[type=url]')})
 expect(await dialog.locator('input[type=number]').inputValue()).toBe('6');expect(await dialog.getByRole('button',{name:text('Apply','套用'),exact:true}).isDisabled()).toBe(true)
})

test('a saved host rule round-trips through the checked destination',{session:'normal1',timeout:120_000},async({app})=>{
 const page=await mediaPage(app),destination=await data(page,'delegate.destination',{key:'save'})
 let id=''
 try{
  const rule=await data(page,'media.rules.save',{host:'media-rule-fixture.example',path:destination.destination.path,options:{format:'720',subtitles:'vtt',subtitleLanguages:['en'],nfo:true}});id=rule.id
  const state=await data(page,'media.settings.get'),saved=state.rules.find((r:any)=>r.id===id)
  expect(saved.path).toBe(destination.destination.path);expect(saved.options.format).toBe('720');expect(saved.options.nfo).toBe(true)
  expect((await call(page,'media.rules.delete',{id,owner:'another-user'})).success).toBe(false)
 }finally{if(id)await call(page,'media.rules.delete',{id})}
})

// The tester supplies a short authorized non-live video fixture. This
// spec uses the real downloader and protected receipts, not mocked tool output.
test('video options publish a paired NFO through normal save receipts',{
 session:'normal1',timeout:600_000,
 skip:process.env.E2E_MEDIA_URL?false:'Set E2E_MEDIA_URL to an authorized short non-live video fixture',
},async({app})=>{
 const {createFolder,engine,fileInfo,removeFromNas,saveFolder,tag,tasks,task}=await import('./support/download-hub.ts')
 const page=await openDownloadHub(app),restore=await saveFolder(page)
 const current=await data(page,'delegate.destination',{key:'save'})
 const folder=await createFolder(page,current.selection.root,`${tag}-media-${Date.now()}`),key=`media-${Date.now()}-${Math.random().toString(36).slice(2)}`
 let id=''
 try{
  await data(page,'gopeed.destination',{key:'save',path:folder})
  await data(page,'downloads.folder.set',{subfolder:''})
  const accepted=await data(page,'downloads.create_many',{path:folder,items:[{key,url:process.env.E2E_MEDIA_URL}],mediaOptions:{format:'720',subtitles:'off',nfo:true,embedMetadata:true}})
  expect(accepted.results[0].status).toBe('created')
  await expect.poll(async()=>{id=(await tasks(page)).find((t:any)=>t.publication?.batch===key&&t.protocol==='media')?.id??'';return id},{timeout:120_000}).not.toBe('')
  await expect.poll(async()=>(await task(page,id)).publication?.published,{timeout:420_000,interval:5000}).toBe(true)
  const downloaded=await task(page,id),names=downloaded.meta.res.files.map((f:any)=>f.name)
  const nfo=names.find((name:string)=>name.endsWith('.nfo'));expect(nfo).toBeTruthy()
  const stem=nfo.slice(0,-4);expect(names.some((name:string)=>name.startsWith(stem+'.')&&/\.(mp4|mkv|webm|mov|m4v)$/.test(name))).toBe(true)
  expect((await fileInfo(page,folder+'/'+nfo)).additional.size).toBeGreaterThan(0)
 }finally{
  for(const t of await tasks(page))if(t.id===id||t.publication?.batch===key)await engine(page,'DELETE',`/api/v1/tasks/${t.id}`).catch(()=>undefined)
  await restore(page);await removeFromNas(page,[folder])
 }
})
