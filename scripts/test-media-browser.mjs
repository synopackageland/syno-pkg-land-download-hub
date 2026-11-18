// Local Vue fixture only: no NAS connections or real authentication material.
import assert from 'node:assert/strict'
import {mkdir,writeFile,readFile} from 'node:fs/promises'
import {resolve,join,extname} from 'node:path'
import {createServer} from '../frontend/node_modules/vite/dist/node/index.js'
import {chromium} from 'playwright'
const out=resolve(process.env.BROWSER_ARTIFACTS??'.cache/media-browser');await mkdir(out,{recursive:true})
const bundleRoot=resolve('ui')
const html=(await readFile('frontend/test/preview.html','utf8')).replace('<head>','<head><link rel="stylesheet" href="/bundle/app.css">').replace("from '../src/main.ts'","from '/bundle/app.module.js'")
const server=await createServer({root:resolve('frontend'),configFile:resolve('frontend/vite.config.ts'),server:{host:'127.0.0.1',port:18781,strictPort:true},plugins:[{name:'media-built-fixture',configureServer(server){server.middlewares.use(async(req,res,next)=>{
 const path=new URL(req.url,'http://fixture.example').pathname
 if(path==='/test/media-built.html'){res.setHeader('Content-Type','text/html');res.end(html);return}
 if(path.startsWith('/bundle/')){const file=resolve(bundleRoot,path.slice(8));if(!file.startsWith(bundleRoot+'/')){res.statusCode=404;res.end();return};try{res.setHeader('Content-Type',extname(file)==='.css'?'text/css':'text/javascript');res.end(await readFile(file))}catch{res.statusCode=404;res.end()};return};next()
 })}}]});await server.listen()
const browser=await chromium.launch({executablePath:process.env.CHROME??'/usr/bin/chromium',args:['--no-sandbox','--disable-dev-shm-usage']})
const evidence=[]
const shot=async(page,path)=>{await page.waitForTimeout(450);await page.screenshot({path})}
try{
 for(const width of [390,700,768,1280]){
  const context=await browser.newContext({viewport:{width,height:width===390?844:900},isMobile:width===390,hasTouch:width===390||width===768})
  const page=await context.newPage(),errors=[],warnings=[];page.on('pageerror',e=>{errors.push(String(e));console.error(String(e))});page.on('console',m=>{if(m.type()==='warning'&&/button|capsule|primary/i.test(m.text()))warnings.push(m.text())})
  await page.goto('http://127.0.0.1:18781/test/media-built.html?full-page&admin=false&locale=zh-TW')
  await page.waitForFunction(()=>window.fixture,null,{timeout:120000})
  if(width===390)await page.locator('[data-phone-tabs] [data-ui-tab-bar-item=settings]').click()
  else await page.locator('[data-nav-item]').filter({hasText:'設定'}).click()
  await page.locator('[data-ui-tab-value=media]').click();await page.locator('[data-media-settings]').waitFor({state:'visible'})
  await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='media.cookies.status'))
  await shot(page,join(out,`media-settings-${width}.png`))
  await page.locator('[data-media-add-rule]').click();await page.getByRole('dialog').last().waitFor();await shot(page,join(out,`media-rule-${width}.png`));await page.getByRole('dialog').last().getByRole('button',{name:'取消',exact:true}).click()
  await page.locator('[data-media-add-follow]').click();await page.getByRole('dialog').last().waitFor();await shot(page,join(out,`media-follow-${width}.png`));await page.getByRole('dialog').last().getByRole('button',{name:'取消',exact:true}).click()
  await page.locator('[data-media-import]').click();await page.locator('[data-media-cookie-text]').waitFor();await shot(page,join(out,`media-cookies-${width}.png`))
  await page.locator('[data-media-cookie-text]').fill('# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tid\tfixture-cookie-value\n')
  await page.getByRole('dialog').last().getByRole('button',{name:'匯入 cookies',exact:true}).click()
  await page.locator('[data-media-cookie]').waitFor();await page.locator('[data-media-cookie]').scrollIntoViewIfNeeded()
  await shot(page,join(out,`media-cookie-status-${width}.png`));assert.equal((await page.locator('[data-media-settings]').textContent()).includes('fixture-cookie-value'),false)

  let touchTargets=[]
  if(width===390){
   await page.locator('[data-phone-tabs] [data-ui-tab-bar-item=downloads]').click()
   await page.locator('[data-phone-downloads]').waitFor()
   await shot(page,join(out,`phone-downloads-${width}.png`))
   await page.locator('[data-phone-options]').click()
   const options=page.locator('[data-phone-media-options]');await options.waitFor()
   await options.getByRole('checkbox').first().click();await options.locator('[data-media-options]').waitFor()
   await options.locator('[data-media-options]').getByRole('checkbox').first().click()
   assert.equal(await page.getByRole('dialog').count(),1,'media choices belong in the existing phone options sheet')
   touchTargets=await options.locator('button:not([role=checkbox]),input,[data-ui-checkbox] label').evaluateAll(all=>all.filter(e=>e.getClientRects().length).map(e=>({kind:e.tagName,height:e.getBoundingClientRect().height})))
   assert.ok(touchTargets.length>0);assert.ok(touchTargets.every(t=>t.height>=44),'phone media choices need 44px targets')
   await shot(page,join(out,`media-options-${width}.png`))
   await page.locator('[data-phone-options-apply]').click()
   await page.locator('[data-phone-options-sheet]').waitFor({state:'hidden'})
   await page.locator('[data-phone-link] input,input[data-phone-link]').first().fill('https://video.example/phone-choice')
   await page.locator('[data-phone-start]').click()
   await page.waitForFunction(()=>window.fixture.calls.some(c=>c.method==='downloads.create_many')).catch(async e=>{console.error(await page.evaluate(()=>({methods:window.fixture.calls.map(c=>c.method),failure:document.querySelector('[data-phone-failure]')?.textContent,input:document.querySelector('[data-phone-link] input,input[data-phone-link]')?.value,valid:document.querySelector('[data-phone-add]')?.checkValidity()})));throw e})
   const batch=await page.evaluate(()=>window.fixture.calls.filter(c=>c.method==='downloads.create_many').at(-1).params)
   assert.equal(batch.mediaOptions.embedThumbnail,true);assert.equal(batch.args,undefined);assert.equal(batch.cookies,undefined)
  }else{
   await page.locator('[data-nav-item]').filter({hasText:'下載'}).first().click();await page.locator('[data-add-download]').click();await page.locator('[data-media-advanced]').click()
   const dialog=page.getByRole('dialog').last();await dialog.getByRole('checkbox').first().click();await shot(page,join(out,`media-options-${width}.png`))
  }
  const overflow=await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1);assert.equal(overflow,false,`viewport overflow at ${width}`);assert.deepEqual(errors,[]);assert.deepEqual(warnings,[])
  evidence.push({width,pageErrors:errors,buttonWarnings:warnings,viewportOverflow:overflow,touchTargets});await context.close()
 }
 await writeFile(join(out,'browser-evidence.json'),JSON.stringify({localFixture:true,evidence},null,2)+'\n');console.log(JSON.stringify(evidence))
}finally{await browser.close();await server.close()}
