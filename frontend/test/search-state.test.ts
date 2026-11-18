import {expect,test} from 'vitest'
import {receiveSource,defaultSourceIDs,createSearchState,beginSearch,receivePage,mergeHeld,sortRows,seederTone,chipAction,emptyKind,enoughFound,searchEnough,skippedSources,type SourceView} from '../src/search'

const sources:SourceView[]=[
 {id:'a',kind:'builtin',name:'Tracker A',enabled:true,configured:false,supported:true,reasons:[],settings:[]},
 {id:'v',kind:'ytsearch',name:'YouTube',enabled:true,configured:false,supported:true,reasons:[],settings:[]},
]
const page=(sourceId:string,n:number,code?:string)=>({sourceId,page:1,results:code?[]:Array.from({length:n},(_,i)=>({resultId:sourceId+i,sourceId,title:'T'+i,...(i?{}:{size:10,seeders:3,date:'2026-10-17T00:00:00Z'})})),...(code?{error:{code}}:{})})

test('a search starts every source running and clears the last results',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 beginSearch(s,['a','v'])
 receivePage(s,page('a',1))
 beginSearch(s,['a','v'])
 expect(s.rows).toEqual([])
 expect(s.statuses).toEqual({a:{state:'running',count:0},v:{state:'running',count:0}})
 expect(s.running).toBe(true)
})

// Unknown values stay empty for the cells to show "—"; a video source's
// results are videos.
test('each source answers into rows and its own status',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 beginSearch(s,['a','v'])
 receivePage(s,page('a',2))
 expect(s.rows[0]).toEqual({id:'a0',title:'T0',type:null,size:10,seeders:3,leechers:null,date:Date.parse('2026-10-17T00:00:00Z'),source:'Tracker A',sourceId:'a',hash:null,sourceIds:['a']})
 expect(s.rows[1]).toMatchObject({size:null,seeders:null,date:null})
 expect(s.statuses.a).toEqual({state:'done',count:2})
 expect(s.running).toBe(true)
 receivePage(s,page('v',0,'search_cloudflare'))
 expect(s.statuses.v).toEqual({state:'cloudflare',count:0,code:'search_cloudflare'})
 expect(s.running).toBe(false)
})

// Once the user sorts, later results wait to be merged so rows do not move
// under the pointer.
test('a sorted list holds new results until they are merged',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 beginSearch(s,['a','v'])
 receivePage(s,page('a',1))
 s.sort={key:'size',direction:'desc'}
 receivePage(s,page('v',3))
 expect(s.rows.map(r=>r.id)).toEqual(['a0'])
 expect(s.held.length).toBe(3)
 mergeHeld(s)
 expect(s.rows.map(r=>r.id)).toEqual(['a0','v0','v1','v2'])
 expect(s.held).toEqual([])
})

// Once a screenful is in, the rest can be stopped, and late answers wait
// behind "N new results" instead of moving the list.
test('enough results hold late answers and offer to stop the rest',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 beginSearch(s,['a','v'])
 expect(enoughFound(s)).toBe(false)
 receivePage(s,page('a',searchEnough))
 expect(enoughFound(s)).toBe(true)
 s.statuses.v.state='browser'
 expect(enoughFound(s)).toBe(true)
 receivePage(s,page('v',2))
 expect(s.rows.length).toBe(searchEnough)
 expect(s.held.length).toBe(2)
 expect(enoughFound(s)).toBe(false)
})

test('a stopped source can be searched again',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 beginSearch(s,['a'])
 receivePage(s,page('a',0,'search_job_canceled'))
 expect(s.statuses.a.state).toBe('stopped')
 expect(chipAction(s.statuses.a)).toBe('retry')
})

// A source's answer to an earlier search never lands in a newer one.
test('an answer for a finished search is ignored',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 const first=beginSearch(s,['a'])
 beginSearch(s,['v'])
 receivePage(s,page('a',1),first)
 expect(s.rows).toEqual([])
})

test('peer and category values come from results, with missing values unknown',()=>{
 const s=createSearchState();s.sources=structuredClone(sources);beginSearch(s,['a'])
 receivePage(s,{sourceId:'a',results:[{resultId:'x',sourceId:'a',title:'Anime',seeders:5,leechers:0,category:'anime'},{resultId:'y',sourceId:'a',title:'Unknown'}]})
 expect(s.rows[0]).toMatchObject({type:'anime',seeders:5,leechers:0})
 expect(s.rows[1]).toMatchObject({type:null,seeders:null,leechers:null})
})

test('Cloudflare defaults exclude only implicit selections without a solver',()=>{
 const s=createSearchState();s.sources=[...structuredClone(sources),{...sources[0],id:'cf',cloudflare:true}]
 expect(defaultSourceIDs(s,false)).toEqual(['a','v'])
 expect(defaultSourceIDs(s,true)).toEqual(['a','v','cf'])
 s.selected=['cf'];expect(defaultSourceIDs(s,false)).toEqual(['cf'])
})

// Unlocking in either Settings or Search restores an explicitly omitted source.

test('connection resets retain their diagnosis instead of requesting a browser',()=>{
 const s=createSearchState();s.sources=structuredClone(sources);beginSearch(s,['a']);receivePage(s,page('a',0,'search_connection_reset'))
 expect(s.statuses.a).toEqual({state:'reset',count:0,code:'search_connection_reset'})
})

const hash='c12fe1c06bba254a9dc9f519b335aa7c1367a88a'
test('the same torrent from two sources is one row naming both; no hash, no merge',()=>{
 const s=createSearchState();s.sources=structuredClone(sources);beginSearch(s,['a','v'])
 receivePage(s,{sourceId:'a',results:[{resultId:'a1',sourceId:'a',title:'Same',seeders:3,hash},{resultId:'a2',sourceId:'a',title:'Same title, no hash'}]})
 receivePage(s,{sourceId:'v',results:[{resultId:'v1',sourceId:'v',title:'Same again',seeders:9,leechers:2,hash},{resultId:'v2',sourceId:'v',title:'Same title, no hash'}]})
 expect(s.rows.map(r=>r.id)).toEqual(['a1','a2','v2'])
 expect(s.rows[0]).toMatchObject({sourceIds:['a','v'],seeders:9,leechers:2})
 // Not a v1 info-hash: never a merge key.
 receivePage(s,{sourceId:'a',results:[{resultId:'a3',sourceId:'a',title:'Bad',hash:'xyz'}]})
 expect(s.rows.at(-1)).toMatchObject({id:'a3',hash:null})
})
test('sorting orders by the chosen value with unknown values last',()=>{
 const rows=[{id:'1',seeders:null,size:5,date:1,title:'b'},{id:'2',seeders:9,size:null,date:3,title:'a'},{id:'3',seeders:2,size:7,date:2,title:'c'}] as any[]
 expect(sortRows(rows,{key:'seeders',direction:'desc'}).map(r=>r.id)).toEqual(['2','3','1'])
 expect(sortRows(rows,{key:'size',direction:'desc'}).map(r=>r.id)).toEqual(['3','1','2'])
 expect(sortRows(rows,{key:'title',direction:'asc'}).map(r=>r.id)).toEqual(['2','1','3'])
 expect(sortRows(rows,null)).toBe(rows)
})
test('seeders read red at none, amber at a few and green at five or more',()=>{
 expect([seederTone(null),seederTone(0),seederTone(1),seederTone(4),seederTone(5)]).toEqual(['','danger','warning','warning','success'])
})
test('a chip offers sign-in, the browser or a new try by its state',()=>{
 expect(chipAction({state:'login',count:0})).toBe('signin')
 expect(chipAction({state:'cloudflare',count:0})).toBe('browser')
 expect(chipAction({state:'timeout',count:0})).toBe('retry')
 expect(chipAction({state:'reset',count:0})).toBe('')
 expect(chipAction({state:'done',count:3})).toBe('')
 expect(chipAction({state:'empty',count:0})).toBe('')
})
test('the empty results area names what happened',()=>{
 const s=createSearchState()
 expect(emptyKind(s)).toBe('start')
 s.statuses={a:{state:'running',count:0},b:{state:'empty',count:0}};expect(emptyKind(s)).toBe('searching')
 s.statuses={a:{state:'empty',count:0},b:{state:'noData',count:0}};expect(emptyKind(s)).toBe('none')
 s.statuses={a:{state:'login',count:0},b:{state:'login',count:0}};expect(emptyKind(s)).toBe('login')
 s.statuses={a:{state:'login',count:0},b:{state:'network',count:0}};expect(emptyKind(s)).toBe('failed')
 s.statuses={a:{state:'empty',count:0},b:{state:'network',count:0}};expect(emptyKind(s)).toBe('none')
 s.rows=[{} as any];expect(emptyKind(s)).toBe('')
})
test('without a browser or solver, Cloudflare sources are skipped unless the user chose them',()=>{
 const s=createSearchState();s.sources=[...structuredClone(sources),{...sources[0],id:'cf',cloudflare:true},{...sources[0],id:'off',cloudflare:true,enabled:false}]
 const ids=defaultSourceIDs(s,false)
 expect(skippedSources(s,ids,false)).toEqual(['cf'])
 expect(skippedSources(s,defaultSourceIDs(s,true),true)).toEqual([])
 s.selected=['a'];expect(skippedSources(s,['a'],false)).toEqual([])
})

// Older helpers sent Go's zero time for a date the definition could not parse.
test('an unparsed year-1 date shows no age',()=>{
 const s=createSearchState();s.sources=structuredClone(sources)
 beginSearch(s,['a'])
 receivePage(s,{sourceId:'a',page:1,results:[{resultId:'z',sourceId:'a',title:'Z',date:'0001-01-01T00:00:00Z'}]} as any)
 expect(s.rows[0].date).toBeNull()
})
