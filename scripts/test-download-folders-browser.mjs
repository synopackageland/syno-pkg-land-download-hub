// Actual built Vue app against local fixture SDK/DSM channels; never a NAS test.
import assert from 'node:assert/strict'
import {mkdir,readFile,writeFile} from 'node:fs/promises'
import {resolve,join,extname} from 'node:path'
import {createServer} from '../frontend/node_modules/vite/dist/node/index.js'
import {chromium} from 'playwright'
const out=resolve(process.env.BROWSER_ARTIFACTS??'.cache/download-folder-browser');await mkdir(out,{recursive:true})
const bundleRoot=resolve('ui'),port=18787
const html=(await readFile('frontend/test/preview.html','utf8')).replace('<head>','<head><link rel="stylesheet" href="/bundle/app.css">').replace("from '../src/main.ts'","from '/bundle/app.module.js'")
const server=await createServer({root:resolve('frontend'),configFile:resolve('frontend/vite.config.ts'),server:{host:'127.0.0.1',port,strictPort:true},plugins:[{name:'download-folder-built-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 const path=new URL(req.url,'http://fixture.example').pathname
 if(path==='/test/download-folders.html'){res.setHeader('Content-Type','text/html');res.end(html);return}
 if(path.startsWith('/bundle/')){const file=resolve(bundleRoot,path.slice(8));if(!file.startsWith(bundleRoot+'/')){res.statusCode=404;res.end();return}try{res.setHeader('Content-Type',extname(file)==='.css'?'text/css':'text/javascript');res.end(await readFile(file))}catch{res.statusCode=404;res.end()};return}next()
 })}}]});await server.listen()
const browser=await chromium.launch({executablePath:process.env.CHROME??chromium.executablePath(),args:['--no-sandbox','--disable-dev-shm-usage']})
const evidence=[]
async function visit(width,query=''){
 const context=await browser.newContext({viewport:{width,height:width===390?844:900},isMobile:width===390,hasTouch:width===390})
 const page=await context.newPage(),errors=[],warnings=[];page.on('pageerror',e=>errors.push(String(e)));page.on('console',m=>{if(m.type()==='warning'&&/button|capsule|primary/i.test(m.text()))warnings.push(m.text())})
 await page.goto(`http://127.0.0.1:${port}/test/download-folders.html?full-page&download-folders&locale=zh-TW&user=example-user&${query}`)
 await page.waitForFunction(()=>window.fixture?.calls.some(c=>c.method==='downloads.location'),null,{timeout:120000})
 return {context,page,errors,warnings,width}
}
async function shot(f,name){
 await f.page.waitForTimeout(300)
 const overflow=await f.page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1)
 assert.equal(overflow,false,name);assert.deepEqual(f.errors,[],name);assert.deepEqual(f.warnings,[],name)
 await f.page.screenshot({path:join(out,name+'.png')})
 evidence.push({name,width:f.width,overflow,errors:f.errors,warnings:f.warnings})
}
async function settings(f){if(f.width===390)await f.page.locator('[data-phone-tabs] [data-ui-tab-bar-item=settings]').click();else await f.page.locator('[data-nav-item]').filter({hasText:'設定'}).click();await f.page.locator('[data-ui-tab-value=media]').click();await f.page.locator('[data-media-settings]').waitFor({state:'visible'})}
async function paste(f){
 if(f.width===390){await f.page.locator('[data-phone-link] input,input[data-phone-link]').first().fill('https://example.com/pending.zip');await f.page.locator('[data-phone-start]').click()}
 else{await f.page.locator('[data-add-download]').click();const input=f.page.locator('form.add-download textarea');await input.fill('https://example.com/pending.zip');await f.page.locator('form.add-download').evaluate(form=>form.requestSubmit())}
 await f.page.locator('[data-download-folder-dialog]').waitFor({state:'visible'})
}
try{
 for(const width of [390,1280]){
  const f=await visit(width,'admin=false');await settings(f)
  for(const kind of ['rule','follow']){
   await f.page.locator(`[data-media-add-${kind}]`).click();await f.page.locator('[data-rule-subfolder]').waitFor();await f.page.locator('[data-rule-subfolder] input,input[data-rule-subfolder]').first().fill('Videos/YouTube/Channel')
   await shot(f,`${kind}-folder-${width}`)
   await f.page.getByRole('dialog').last().getByRole('button',{name:'取消',exact:true}).click()
  }
  await f.context.close()
  for(const arch of ['amd64','arm64']){
   const g=await visit(width,`arch=${arch}`)
   if(width===390){await g.page.setViewportSize({width:1280,height:900});await g.page.waitForTimeout(200)}
   await g.page.locator('[data-nav-item]').filter({hasText:'核心更新'}).click()
   if(width===390){await g.page.setViewportSize({width:390,height:844});await g.page.waitForTimeout(200)}
   const row=g.page.locator('[data-media-tool="gallery-dl"]');await row.waitFor({state:'visible'});await row.scrollIntoViewIfNeeded()
   if(arch==='arm64'){assert.ok((await row.textContent()).includes('此機型不支援'));assert.equal(await row.getByRole('button').count(),0)}
   else assert.equal(await row.getByRole('button',{name:'檢查更新',exact:true}).count(),1)
   await shot(g,`gallery-${arch}-${width}`);await g.context.close()
  }
  const first=await visit(width,'first-folder&admin=false');await paste(first)
  assert.equal(await first.page.locator('[data-download-subfolder] input,input[data-download-subfolder]').inputValue(),'Downloads')
  assert.equal(await first.page.evaluate(()=>window.fixture.calls.some(c=>c.method==='downloads.create_many')),false)
  await shot(first,`first-folder-${width}`)
  await first.page.locator('[data-confirm-download-folder]').click()
  await first.page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='downloads.create_many'))
  const submitted=await first.page.evaluate(()=>window.fixture.calls.find(c=>c.method==='downloads.create_many').params)
  assert.equal(submitted.path,'/home/Downloads');assert.equal(submitted.items[0].url,'https://example.com/pending.zip')
  await first.context.close()
  for(const admin of [true,false]){
   const h=await visit(width,`first-folder&home-off&admin=${admin}`)
   await h.page.locator('[data-home-service-notice]').waitFor({state:'visible'})
   await shot(h,`home-off-notice-${admin?'admin':'user'}-${width}`)
   await paste(h);await h.page.locator('[data-home-off]').waitFor({state:'visible'})
   assert.equal(await h.page.locator('[data-confirm-download-folder]').count(),0)
   const txt=await h.page.locator('[data-home-off]').textContent();assert.ok(txt.includes(admin?'使用者家目錄服務未開啟':'請管理員開啟使用者家目錄服務'))
   await shot(h,`home-off-sheet-${admin?'admin':'user'}-${width}`);await h.context.close()
  }
 }
 await writeFile(join(out,'browser-evidence.json'),JSON.stringify({localFixture:true,evidence},null,2)+'\n');console.log(JSON.stringify(evidence))
}finally{await browser.close();await server.close()}
