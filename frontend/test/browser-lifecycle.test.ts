import {afterEach,expect,test,vi} from 'vitest'
import {createBrowserHelper,sanitizeBrowserHTML} from '../src/browser-helper'
import {beginSearch,createSearchState,receivePage} from '../src/search'
afterEach(()=>vi.useRealTimers())
const ticket=(n:number)=>({id:String(n),token:'b'.repeat(64),url:`https://source${n}.example/search`,operation:'search' as const,expires:new Date(Date.now()+300000).toISOString(),hosts:[`https://source${n}.example`],selector:'table.results'})
function fixture(fail?:string){
 const calls:any[]=[],tabs=new Map<string,string>();let active=0,peak=0
 const send=vi.fn(async(m:any)=>{
  calls.push(m)
  if(m.type==='listTabs')return {items:[...tabs].map(([tabKey,url])=>({tabKey,url}))}
  if(m.type==='openTab'){tabs.set(m.tabKey,m.url);peak=Math.max(peak,++active);return {}}
  if(m.type==='closeTab'){if(tabs.delete(m.tabKey))active--;return {}}
  if(m.type==='dom.extract'){
   if(fail==='error')throw {code:'search_rto_extract_limit_exceeded'}
   if(fail==='timeout')return new Promise((_,reject)=>setTimeout(()=>reject({code:'search_rto_timeout'}),8000))
   return {fields:{page:'<table class="results"><tr><td>Result</td></tr></table>',title:'Results'}}
  }
  return {}
 })
 const ctx:any={call:vi.fn(async()=>({sourceId:'s',results:[]}))}
 return {calls,tabs,send,ctx,peak:()=>peak}
}
test('four sources run concurrently, queued tokens are fresh, and all owned tabs close',async()=>{
 vi.useFakeTimers();const f=fixture(),h=createBrowserHelper(f.ctx,f.send),issue=Array.from({length:6},(_,i)=>vi.fn(async()=>ticket(i)))
 const jobs=issue.map(x=>h.request(x));await vi.advanceTimersByTimeAsync(1)
 expect(f.peak()).toBe(4);expect(issue[4]).not.toHaveBeenCalled()
 await vi.advanceTimersByTimeAsync(4000);await Promise.all(jobs)
 expect(f.ctx.call).toHaveBeenCalledTimes(6);expect(f.tabs.size).toBe(0)
 expect(f.calls.filter(x=>x.type==='openTab').every(x=>x.focus===false&&x.newTab===true&&x.reuse===false&&x.forceDuplicateForSameTabKey===true)).toBe(true)
 expect(new Set(f.calls.filter(x=>x.type==='openTab').map(x=>x.tabKey)).size).toBe(6)
 h.dispose()
})
test.each(['error','timeout','cancel'])('%s closes the tab without affecting unrelated tabs',async mode=>{
 vi.useFakeTimers();const f=fixture(mode),h=createBrowserHelper(f.ctx,f.send);f.tabs.set('user-tab','https://source0.example/search')
 const done=h.request(ticket(0)).catch(e=>typeof e.code==='string'?e.code:e.name)
 await vi.advanceTimersByTimeAsync(1)
 if(mode==='cancel')h.cancel()
 // An extract RTO never answers is retried until the ticket ends, then named.
 await vi.advanceTimersByTimeAsync(310000);expect(await done).toBe({error:'search_rto_extract_limit_exceeded',timeout:'search_rto_timeout',cancel:'AbortError'}[mode])
 expect([...f.tabs.keys()]).toEqual(['user-tab']);expect(f.ctx.call).not.toHaveBeenCalled();h.dispose()
})
// No fixed deadline: a slow source keeps waiting, shown in progress, while
// another finishes; the owner's Cancel ends it and closes its tab.
test('one slow source keeps waiting while another finishes, until Cancel',async()=>{
 vi.useFakeTimers();const f=fixture(),send=async(m:any)=>m.type==='dom.extract'&&m.tabKey.endsWith('-2')?new Promise(()=>{}):f.send(m)
 const h=createBrowserHelper(f.ctx,send),a=h.request(ticket(0));let b:any='pending';h.request(ticket(1)).then(v=>{b=v},e=>{b=typeof e.code==='string'?e.code:e.name})
 await vi.advanceTimersByTimeAsync(2000);await a;expect(f.ctx.call).toHaveBeenCalledOnce()
 await vi.advanceTimersByTimeAsync(120000);expect(b).toBe('pending');expect(h.state.activity).toBe('verification')
 h.cancel();await vi.advanceTimersByTimeAsync(1);expect(b).toBe('AbortError');expect(f.tabs.size).toBe(0);h.dispose()
})
// A NAS too busy to parse hands the token back: the same page goes again.
test('a busy NAS delivery is sent again with the same page until it is kept',async()=>{
 vi.useFakeTimers();const f=fixture();let tries=0
 f.ctx.call=vi.fn(async(_m:string,p:any)=>{if(++tries<3)throw {code:'search_browser_busy'};return {sourceId:'s',results:[],html:p.html}})
 const h=createBrowserHelper(f.ctx,f.send),done=h.request(ticket(0))
 await vi.advanceTimersByTimeAsync(9000);const page:any=await done
 expect(tries).toBe(3);expect(new Set(f.ctx.call.mock.calls.map((c:any)=>c[1].html)).size).toBe(1);expect(page.html).toContain('Result');expect(f.tabs.size).toBe(0);h.dispose()
})
test('large inline payload is removed before the unchanged delivery budget is measured',()=>{
 expect(sanitizeBrowserHTML(`<script>${'x'.repeat(300000)}</script><p data-payload="${'x'.repeat(300000)}">OK</p>`)).toBe('<p>OK</p>')
 expect(()=>sanitizeBrowserHTML(`<p>${'é'.repeat(140000)}</p>`)).toThrow()
})
test.each(['search_empty','search_no_data'])('%s maps to a neutral source state',code=>{
 const s=createSearchState();beginSearch(s,['s']);receivePage(s,{sourceId:'s',results:[],error:{code}})
 expect(s.statuses.s.state).toBe(code==='search_empty'?'empty':'noData')
})

test('RTO reused tabs are never closed even when it ignores newTab',async()=>{
 vi.useFakeTimers();const f=fixture(),send=async(m:any)=>{const out=await f.send(m);return m.type==='openTab'?{reused:true}:out}
 const h=createBrowserHelper(f.ctx,send),done=h.request(ticket(0));await vi.advanceTimersByTimeAsync(2000);await done
 expect(f.calls.filter(x=>x.type==='closeTab')).toHaveLength(0);h.dispose()
})
test('late open reply after cancellation closes the newly created tab',async()=>{
 vi.useFakeTimers();const f=fixture();let open!:(v:any)=>void
 const send=async(m:any)=>m.type==='openTab'?new Promise(resolve=>{open=async(v:any)=>{await f.send(m);resolve(v)}}):f.send(m)
 const h=createBrowserHelper(f.ctx,send),done=h.request(ticket(0)).catch(e=>e.name)
 await vi.advanceTimersByTimeAsync(1);h.cancel();expect(await done).toBe('AbortError')
 open({reused:false});await vi.advanceTimersByTimeAsync(1)
 expect(f.tabs.size).toBe(0);expect(f.ctx.call).not.toHaveBeenCalled();h.dispose()
})
test('a failing delivery closes the tab and a forged high budget stays at 256 KiB',async()=>{
 vi.useFakeTimers();const f=fixture();f.ctx.call.mockRejectedValue({code:'search_browser_expired'})
 const h=createBrowserHelper(f.ctx,f.send),done=h.request({...ticket(0),maxBytes:1048576}).catch(e=>e.code)
 await vi.advanceTimersByTimeAsync(2000);expect(await done).toBe('search_browser_expired')
 expect(f.tabs.size).toBe(0);expect(f.calls.find(x=>x.type==='dom.extract').args.maxBytes).toBe(262144);h.dispose()
})
test('presence polling does not mark a busy working browser disconnected',async()=>{
 vi.useFakeTimers();const f=fixture('timeout');let fail=false
 const send=async(m:any)=>{if(m.type==='detect')return {version:'8.6.0'};if(m.type==='listTabs'&&fail)throw {code:'search_rto_timeout'};return f.send(m)}
 const h=createBrowserHelper(f.ctx,send);await h.check();expect(h.state.status).toBe('ready')
 const pending=h.request(ticket(0)).catch(()=>{});await vi.advanceTimersByTimeAsync(1)
 fail=true;await h.check();expect(h.state.status).toBe('ready')
 h.cancel();await pending;h.dispose()
})
test('explicit verification stays open beyond the background grace and closes after completion',async()=>{
 vi.useFakeTimers();const f=fixture();let challenge=true
 const send=async(m:any)=>{const out=await f.send(m);return m.type==='dom.extract'?{fields:{page:'<p>Ready</p>',title:challenge?'Just a moment...':'Results'}}:out}
 const h=createBrowserHelper(f.ctx,send),auto=h.request(ticket(0)).catch(e=>e.code)
 await vi.advanceTimersByTimeAsync(12000);expect(await auto).toBe('search_browser_verification')
 const manual=h.focus(ticket(0).url);await vi.advanceTimersByTimeAsync(12000)
 expect(f.tabs.size).toBe(1)
 challenge=false;await vi.advanceTimersByTimeAsync(3500);await manual;expect(f.tabs.size).toBe(0);h.dispose()
})
test('ticket and open admission do not spend the source loading timeout',async()=>{
 vi.useFakeTimers();const f=fixture();const send=async(m:any)=>{if(m.type==='openTab')await new Promise(r=>setTimeout(r,50000));return f.send(m)};const h=createBrowserHelper(f.ctx,send)
 const done=h.request(async()=>{await new Promise(r=>setTimeout(r,50000));return ticket(0)},false,'slow-admission').catch(e=>e.code)
 await vi.advanceTimersByTimeAsync(103000);expect(await done).toEqual({sourceId:'s',results:[]})
 expect(h.state.timings['slow-admission'].phases.map(x=>x.phase)).toEqual(expect.arrayContaining(['queued','open','opened','loaded','stable','extracted','delivered','closed']))
 expect(f.tabs.size).toBe(0);h.dispose()
})
test.each([true,false])('only opted-in same-origin redirects are accepted: %s',async allow=>{
 vi.useFakeTimers();const f=fixture(),send=async(m:any)=>{const out=await f.send(m);if(m.type==='listTabs')out.items=out.items.map((x:any)=>({...x,url:x.url+'?challenge=private-value'}));return out}
 const h=createBrowserHelper(f.ctx,send),done=h.request({...ticket(0),allowSameOriginRedirect:allow},false,'redirect').catch(e=>e.code)
 await vi.advanceTimersByTimeAsync(3000);expect(await done).toEqual(allow?{sourceId:'s',results:[]}:'search_rto_tab_changed')
 expect(JSON.stringify(h.state.timings)).not.toContain('private-value');expect(f.tabs.size).toBe(0);h.dispose()
})
test('an opted-in redirect still rejects another origin',async()=>{
 vi.useFakeTimers();const f=fixture(),send=async(m:any)=>{const out=await f.send(m);if(m.type==='listTabs')out.items=out.items.map((x:any)=>({...x,url:'https://other.example/search'}));return out}
 const h=createBrowserHelper(f.ctx,send),done=h.request({...ticket(0),allowSameOriginRedirect:true}).catch(e=>e.code)
 await vi.advanceTimersByTimeAsync(1);expect(await done).toBe('search_rto_tab_changed');expect(f.tabs.size).toBe(0);h.dispose()
})

test('a queued fifth source gets a fresh ticket after four sources end with their ticket',async()=>{
 vi.useFakeTimers();const f=fixture(),send=async(m:any)=>m.type==='dom.extract'&&!m.tabKey.endsWith('-5')?new Promise((_,reject)=>setTimeout(()=>reject({code:'search_rto_timeout'}),8000)):f.send(m)
 let issued=0;const h=createBrowserHelper(f.ctx,send),jobs=Array.from({length:5},(_,i)=>h.request(()=>{issued++;return Promise.resolve(ticket(i))},false,String(i)).catch(e=>e.code))
 await vi.advanceTimersByTimeAsync(1000);expect(issued).toBe(4)
 await vi.advanceTimersByTimeAsync(320000);const out=await Promise.all(jobs)
 expect(out.slice(0,4)).toEqual(Array(4).fill('search_rto_timeout'));expect(out[4]).toEqual({sourceId:'s',results:[]})
 expect(h.state.timings['4'].phases.find(x=>x.phase==='open')!.ms).toBeGreaterThanOrEqual(300000);expect(f.tabs.size).toBe(0);h.dispose()
})
test('all matching source containers are delivered rather than only the first navigation container',async()=>{
 vi.useFakeTimers();const f=fixture(),send=async(m:any)=>m.type==='dom.extract'?{fields:{page:['<div>Navigation</div>','<table class="results"><tr><td>Result</td></tr></table>'],title:'Results'}}:f.send(m)
 const h=createBrowserHelper(f.ctx,send),done=h.request(ticket(0)).catch(e=>e.code);await vi.advanceTimersByTimeAsync(50000)
 expect(await done).toEqual({sourceId:'s',results:[]});expect(f.ctx.call.mock.calls[0][1].html).toContain('Result');expect(f.tabs.size).toBe(0);h.dispose()
})

// RTO 8.6.0 adopts a browser tab showing the same URL whenever a tabKey is
// given, unless forceDuplicateForSameTabKey asks it for a tab of its own.
test('every source asks RTO for a tab of its own and closes it by the id RTO reported',async()=>{
 vi.useFakeTimers();const f=fixture();let id=40
 const send=async(m:any)=>{const out=await f.send(m);return m.type==='openTab'?{reused:false,tabId:++id}:out}
 const h=createBrowserHelper(f.ctx,send);await Promise.all([h.request(ticket(0)),h.request(ticket(1)),vi.advanceTimersByTimeAsync(2000)])
 const closes=f.calls.filter(x=>x.type==='closeTab')
 expect(closes.map(x=>x.tabId).sort()).toEqual([41,42]);expect(closes.every(x=>/^download-hub-/.test(x.tabKey))).toBe(true)
 expect(f.tabs.size).toBe(0);expect(h.state.unclosed).toBe(0);h.dispose()
})
// closeTab finds tabs only in RTO's in-memory registry; listTabs restores it
// from storage after the MV3 worker restarted.
test('a close RTO refuses is retried once after listTabs restores its registry',async()=>{
 vi.useFakeTimers();const f=fixture(),log:string[]=[];let refused=false
 const send=async(m:any)=>{log.push(m.type);if(m.type==='closeTab'&&!refused){refused=true;throw {code:'search_rto_no_controlled_tab'}}return f.send(m)}
 const h=createBrowserHelper(f.ctx,send);await Promise.all([h.request(ticket(0)),vi.advanceTimersByTimeAsync(2000)])
 const at=log.indexOf('closeTab');expect(log.slice(at)).toEqual(['closeTab','listTabs','closeTab'])
 expect(f.tabs.size).toBe(0);expect(h.state.unclosed).toBe(0);h.dispose()
})
test('a tab RTO still cannot close is counted for a hint, tried only twice and the count clears with the next search',async()=>{
 vi.useFakeTimers();const f=fixture();let closes=0
 const send=async(m:any)=>{if(m.type==='closeTab'){closes++;throw {code:'search_rto_no_controlled_tab'}}return f.send(m)}
 const h=createBrowserHelper(f.ctx,send);await Promise.all([h.request(ticket(0)),h.request(ticket(1)),vi.advanceTimersByTimeAsync(2000)])
 expect(closes).toBe(4);expect(h.state.unclosed).toBe(2)
 h.cancel();expect(h.state.unclosed).toBe(0);h.dispose()
})
