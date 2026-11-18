import {afterEach,expect,test,vi} from 'vitest'
import {createBrowserHelper,sanitizeBrowserHTML,sendRto,type RtoSender} from '../src/browser-helper'
const clean:Array<()=>void>=[]
afterEach(()=>{clean.splice(0).forEach(f=>f());vi.restoreAllMocks()})
const incoming={id:'a'.repeat(32),token:'b'.repeat(64),url:'https://custom.example/search?q=linux',operation:'search' as const,expires:new Date(Date.now()+300000).toISOString(),hosts:['https://custom.example']}
test('RTO bridge correlates same-window same-origin replies and times out forged replies',async()=>{
 const spy=vi.spyOn(window,'postMessage').mockImplementation((data:any)=>{
  window.dispatchEvent(new MessageEvent('message',{source:window,origin:location.origin,data:{origin:'rto-extension',requestId:'wrong',ok:true,data:{forged:true}}}))
  window.dispatchEvent(new MessageEvent('message',{source:window,origin:location.origin,data:{origin:'rto-extension',requestId:data.requestId,ok:true,data:{version:'8.6.0'}}}))
 })
 expect(await sendRto({type:'detect'},100)).toEqual({version:'8.6.0'});expect(spy.mock.calls[0][0]).toMatchObject({origin:'rto-page',type:'detect'})
})
test('one bounded RTO batch keeps more than 5000 characters and sends sanitized DOM to the original job token',async()=>{
 const calls:any[]=[],deliveries:any[]=[];let key=''
 const html='<table>'+Array.from({length:20},(_,i)=>`<tr><td class="name">Linux ${i} ${'x'.repeat(300)}</td><td><a href="/detail/${i}">Get</a></td></tr>`).join('')+'</table><script>secret()</script><input value="secret"><iframe src="/private"></iframe>'
 const send:RtoSender=async m=>{calls.push(m);if(m.type==='openTab')key=String(m.tabKey);if(m.type==='listTabs')return {items:[{tabKey:key,url:incoming.url}]};if(m.type==='dom.extract')return {fields:{page:html,title:'Linux results'}};return {}}
 const ctx:any={call:async(m:string,p:any)=>{deliveries.push({m,p});return {sourceId:'custom',results:[]}}}
 const helper=createBrowserHelper(ctx,send);clean.push(helper.dispose)
 await helper.request(incoming)
 expect(calls.filter(m=>m.type==='dom.extract')).toHaveLength(2)
 expect(calls.find(m=>m.type==='dom.extract').args.maxBytes).toBe(262144)
 const p=deliveries[0].p;expect(p.id).toBe(incoming.id);expect(p.token).toBe(incoming.token);expect(p.html.length).toBeGreaterThan(5000)
 expect(new DOMParser().parseFromString(p.html,'text/html').querySelectorAll('tr')).toHaveLength(20)
 expect(p.html).not.toMatch(/secret|script|iframe/)
})
test('domain refusal is reported and never posts results or silently repeats the permission request',async()=>{
 const calls:any[]=[];const send:RtoSender=async m=>{calls.push(m);if(m.type==='openTab')throw {code:'search_rto_user_cancelled'};return {}}
 const ctx:any={call:vi.fn()};const helper=createBrowserHelper(ctx,send);clean.push(helper.dispose)
 await expect(helper.request(incoming)).rejects.toMatchObject({code:'search_rto_user_cancelled'})
 expect(ctx.call).not.toHaveBeenCalled();expect(calls.filter(m=>m.type==='openTab')).toHaveLength(1)
})
test('tab navigation prevents DOM ingestion',async()=>{
 const ctx:any={call:vi.fn()};const send:RtoSender=async m=>m.type==='listTabs'?{items:[]}:{}
 const helper=createBrowserHelper(ctx,send);clean.push(helper.dispose)
 await expect(helper.request(incoming)).rejects.toMatchObject({code:'search_rto_tab_closed'});expect(ctx.call).not.toHaveBeenCalled()
})
test('browser HTML strips executable and form data without running it',()=>{
 expect(sanitizeBrowserHTML('<p id="row" onclick="secret()">Title</p><form><input value="secret"></form>')).toBe('<p id="row">Title</p>')
})
test('imported source selectors retain data fields without transferring credential attributes',()=>{
 const html=sanitizeBrowserHTML('<article data-id="release-7" data-author="Linux project" data-cookie="private" data-session="private"><a data-download="/detail/7" aria-label="Get Linux" onclick="secret()">Linux</a></article>')
 const doc=new DOMParser().parseFromString(html,'text/html')
 expect(doc.querySelector('article[data-id="release-7"] a[data-download]')?.getAttribute('data-download')).toBe('/detail/7')
 expect(doc.querySelector('article')?.getAttribute('data-author')).toBe('Linux project')
 expect(html).not.toMatch(/private|onclick|secret\(\)/)
})

test('a verification error closes the background tab; only explicit interaction focuses a fresh tab',async()=>{
 vi.useFakeTimers();clean.push(()=>vi.useRealTimers())
 const calls:any[]=[];let key='',challenge=true
 const send:RtoSender=async m=>{calls.push(m);if(m.type==='openTab')key=String(m.tabKey);if(m.type==='listTabs')return {items:[{tabKey:key,url:incoming.url}]};if(m.type==='dom.extract')return {fields:{page:'<p>Ready</p>',title:challenge?'Just a moment...':'Results'}};return {}}
 const helper=createBrowserHelper({call:vi.fn(async()=>({sourceId:'custom',results:[]}))} as any,send);clean.push(helper.dispose)
 const result=expect(helper.request(incoming)).rejects.toMatchObject({code:'search_browser_verification'})
 await vi.advanceTimersByTimeAsync(12000);await result
 expect(calls.find(m=>m.type==='openTab')).toMatchObject({focus:false,reuse:false,newTab:true})
 expect(calls.some(m=>m.type==='focusTab')).toBe(false)
 expect(calls.filter(m=>m.type==='closeTab')).toHaveLength(1)
 challenge=false
 const focused=helper.focus(incoming.url);await vi.advanceTimersByTimeAsync(1600);await focused
 expect(calls.filter(m=>m.type==='focusTab')).toEqual([{type:'focusTab',tabKey:key}])
 expect(calls.filter(m=>m.type==='closeTab')).toHaveLength(2)
 await expect(helper.focus('https://unrequested.example/')).rejects.toMatchObject({code:'search_rto_tab_closed'})
})

test('a loading tab is retried without reopening or losing its original ticket',async()=>{
 vi.useFakeTimers();clean.push(()=>vi.useRealTimers())
 let key='',extracts=0
 const send:RtoSender=async m=>{
  if(m.type==='openTab')key=String(m.tabKey)
  if(m.type==='listTabs')return {items:[{tabKey:key,url:incoming.url}]}
  if(m.type==='dom.extract'){if(++extracts===1)throw {code:'search_rto_tab_navigating'};return {fields:{page:'<p>Ready</p>',title:'Results'}}}
  return {}
 }
 const ctx:any={call:vi.fn(async()=>({sourceId:'custom',results:[]}))},helper=createBrowserHelper(ctx,send);clean.push(helper.dispose)
 const done=helper.request(incoming)
 await vi.advanceTimersByTimeAsync(3200);await done
 expect(extracts).toBe(3);expect(ctx.call).toHaveBeenCalledOnce()
 expect(ctx.call.mock.calls[0][1]).toMatchObject({id:incoming.id,token:incoming.token})
})

test('queued assistance obtains its ticket only on admission and cancellation never issues a queued ticket',async()=>{
 let release!:()=>void
 const blocked=new Promise<void>(resolve=>{release=resolve})
 const send:RtoSender=async m=>{if(m.type==='openTab'){await blocked;throw {code:'search_rto_user_cancelled'}};return {}}
 const helper=createBrowserHelper({call:vi.fn()} as any,send);clean.push(helper.dispose)
 const active=Array.from({length:4},()=>helper.request(incoming).catch(e=>e.name))
 for(let i=0;i<10;i++)await Promise.resolve()
 const issue=vi.fn(async()=>incoming),queued=helper.request(issue).catch(e=>e.name)
 expect(issue).not.toHaveBeenCalled()
 helper.cancel();release()
 expect(await queued).toBe('AbortError');await Promise.all(active)
 expect(issue).not.toHaveBeenCalled()
})

test('source row and category selectors retain inert bgcolor and image src attributes',()=>{
 const html=sanitizeBrowserHTML('<table class="table2"><tbody><tr bgcolor="#fff" onclick="secret()"><td><img src="/templates/new/images/icons/menu_icon1.gif" onerror="secret()"></td><td>Result</td></tr></tbody></table>')
 const doc=new DOMParser().parseFromString(html,'text/html')
 expect(doc.querySelectorAll('.table2 > tbody > tr[bgcolor]')).toHaveLength(1)
 expect(doc.querySelector('img[src^="/templates/new/images/icons/menu_icon"]')).toBeTruthy()
 expect(html).not.toMatch(/onclick|onerror|secret/)
})

test('a repeated search opens a fresh owned tab after closing the previous one',async()=>{
 vi.useFakeTimers();clean.push(()=>vi.useRealTimers())
 const calls:any[]=[];let key=''
 const send:RtoSender=async m=>{calls.push(m);if(m.type==='openTab')key=String(m.tabKey);if(m.type==='listTabs')return {items:key?[{tabKey:key,url:incoming.url,isOpen:true}]:[]};if(m.type==='dom.extract')return {fields:{page:'<p>Stable results</p>',title:'Results'}};return {}}
 const helper=createBrowserHelper({call:vi.fn(async()=>({sourceId:'custom',results:[]}))} as any,send);clean.push(helper.dispose)
 const first=helper.request(incoming);await vi.advanceTimersByTimeAsync(1600);await first
 const second=helper.request(incoming);await vi.advanceTimersByTimeAsync(1600);await second
 expect(calls.filter(m=>m.type==='openTab')).toHaveLength(2)
 expect(calls.filter(m=>m.type==='closeTab')).toHaveLength(2)
 expect(calls.filter(m=>m.type==='navigate')).toHaveLength(0)
})

test('a successfully answered but partial navigation DOM is not delivered as an empty result page',async()=>{
 vi.useFakeTimers();clean.push(()=>vi.useRealTimers())
 let key='',reads=0;const html='<table><tr><td>Complete result</td></tr></table>'
 const send:RtoSender=async m=>{if(m.type==='openTab')key=String(m.tabKey);if(m.type==='listTabs')return {items:key?[{tabKey:key,url:incoming.url}]:[]};if(m.type==='dom.extract')return {fields:{page:++reads===1?'<header>Loading</header>':html,title:'Results'}};return {}}
 const ctx:any={call:vi.fn(async()=>({sourceId:'custom',results:[]}))},helper=createBrowserHelper(ctx,send);clean.push(helper.dispose)
 const done=helper.request(incoming);await vi.advanceTimersByTimeAsync(3200);await done
 expect(ctx.call).toHaveBeenCalledOnce()
 const delivered=new DOMParser().parseFromString(ctx.call.mock.calls[0][1].html,'text/html')
 expect(delivered.querySelector('td')?.textContent).toBe('Complete result');expect(delivered.querySelector('header')).toBeNull()
})

test('a browser error document is diagnosed before HTML ingestion',async()=>{
 const ctx:any={call:vi.fn()};let key=''
 const send:RtoSender=async m=>{if(m.type==='openTab')key=String(m.tabKey);if(m.type==='listTabs')return {items:[{tabKey:key,url:'chrome-error://chromewebdata/'}]};return {}}
 const helper=createBrowserHelper(ctx,send);clean.push(helper.dispose)
 await expect(helper.request(incoming)).rejects.toMatchObject({code:'search_browser_network'})
 expect(ctx.call).not.toHaveBeenCalled()
})
