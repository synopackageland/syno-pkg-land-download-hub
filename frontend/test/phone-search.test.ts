import {readFileSync} from 'node:fs'
import {resolve} from 'node:path'
import {expect,test} from 'vitest'
import {createSearchState,defaultSourceIDs,desktopOnlyCount,phoneSourceIDs,type SourceView} from '../src/search'

const source=(id:string,extra:Partial<SourceView>={}):SourceView=>({id,kind:'builtin',name:id,enabled:true,configured:false,supported:true,reasons:[],settings:[],...extra})
function state(solver=false){
 const s=createSearchState()
 s.sources=[source('server'),source('feed',{kind:'rss'}),source('cf',{cloudflare:true}),source('picked',{cloudflare:true,explicit:true}),source('off',{cloudflare:true,enabled:false}),source('old',{cloudflare:true,supported:false})]
 s.solverConfigured=solver
 return s
}
test('a phone search leaves out every source that needs the browser',()=>{
 const s=state()
 // Even one the user picked by hand: no phone browser can answer for it.
 s.selected=['server','cf','picked','feed']
 expect(phoneSourceIDs(s,defaultSourceIDs(s,false))).toEqual(['server','feed'])
})
test('the default choice keeps only server-side sources',()=>{
 const s=state()
 // A RTO-ready desktop would search the Cloudflare sources too.
 expect(defaultSourceIDs(s,true)).toEqual(['server','feed','cf','picked'])
 expect(phoneSourceIDs(s,defaultSourceIDs(s,true))).toEqual(['server','feed'])
})
test('an external solver answers on the server, so a phone keeps those sources',()=>{
 const s=state(true)
 expect(phoneSourceIDs(s,['server','cf'])).toEqual(['server','cf'])
 expect(desktopOnlyCount(s)).toBe(0)
})
test('the desktop-only count names enabled, supported browser sources',()=>{
 expect(desktopOnlyCount(state())).toBe(2)
 const none=createSearchState();none.sources=[source('server')]
 expect(desktopOnlyCount(none)).toBe(0)
})
test('an id the list no longer knows is left to the server to refuse',()=>{
 expect(phoneSourceIDs(state(),['gone'])).toEqual(['gone'])
})

import {searchSummary,searchTuned} from '../src/search'
test('the phone status line counts sources, results, work left and problems',()=>{
 const s=createSearchState()
 s.statuses={a:{state:'done',count:3},b:{state:'running',count:0},c:{state:'login',count:0},d:{state:'empty',count:0},e:{state:'network',count:0}}
 s.rows=[{id:'1'},{id:'2'}] as any;s.held=[{id:'3'}] as any
 expect(searchSummary(s)).toEqual({sources:5,results:3,running:true,problems:2})
 s.statuses.b={state:'done',count:0}
 expect(searchSummary(s).running).toBe(false)
})
test('the options button marks filters away from the defaults',()=>{
 const s=createSearchState()
 expect(searchTuned(s)).toBe(false)
 for(const change of [(x:any)=>{x.category='movie'},(x:any)=>{x.selected=['a']},(x:any)=>{x.sort={key:'size',direction:'desc'}},(x:any)=>{x.sourceFilter='a'}]){
  const t=createSearchState();change(t);expect(searchTuned(t)).toBe(true)
 }
})

// The page is a height-limited flex column whose result list takes the rest;
// with a 44px min-height the status line shrank to one row, and its wrapped
// "N sources work only on a computer" line covered the filter field.
test('the phone status line keeps the height of every line it wraps to',()=>{
 const page=readFileSync(resolve(__dirname,'../src/SearchPage.vue'),'utf8')
 const rule=page.match(/\.search-status \{([^}]*)\}/)?.[1]??''
 expect(rule).toMatch(/flex-wrap:wrap/)
 expect(rule).toMatch(/(^|;)flex:none(;|$)/)
})
