import {expect,test} from 'vitest'
import {createSearchTabs,openSearchTab,closeSearchTab,activeSearch,shownRows,queryOptional,searchUsed,type SourceView} from '../src/search'

const sources:SourceView[]=[
 {id:'a',kind:'builtin',name:'A',enabled:true,configured:false,supported:true,reasons:[],settings:[]},
 {id:'r',kind:'rss',name:'Feed',enabled:true,configured:false,supported:true,reasons:[],settings:[]},
]

test('a new tab keeps the sources and choice, starts pending and restores the old tab query',()=>{
 const t=createSearchTabs(),first=activeSearch(t)
 first.sources=structuredClone(sources);first.selected=['a'];first.searched='old';first.query='new';first.category='movie';first.statuses={a:{state:'done',count:1}}
 const tab=openSearchTab(t)
 expect(t.tabs.length).toBe(2)
 expect(t.active).toBe(tab.id)
 expect(tab).toMatchObject({query:'new',category:'movie',selected:['a'],pending:true,rows:[]})
 expect(tab.sources).toEqual(first.sources)
 expect(first.query).toBe('old')
})

test('at most five tabs stay; the oldest idle one closes first',()=>{
 const t=createSearchTabs()
 for(let i=0;i<4;i++)openSearchTab(t)
 const oldest=t.tabs[0].id
 const closed:string[]=[]
 openSearchTab(t,tab=>closed.push(tab.id))
 expect(t.tabs.length).toBe(5)
 expect(closed).toEqual([oldest])
})

test('closing a tab picks its neighbour, and the last tab is replaced by a fresh one',()=>{
 const t=createSearchTabs(),a=activeSearch(t),b=openSearchTab(t)
 closeSearchTab(t,b.id)
 expect(t.active).toBe(a.id)
 a.query='x';a.sources=structuredClone(sources)
 closeSearchTab(t,a.id)
 expect(t.tabs.length).toBe(1)
 expect(activeSearch(t)).toMatchObject({query:'',rows:[]})
 expect(activeSearch(t).sources).toEqual(sources)
})

test('a category hides results of another known type and keeps unknown ones',()=>{
 const rows=[{type:'movie'},{type:'series'},{type:null}] as any
 expect(shownRows(rows,'')).toHaveLength(3)
 expect(shownRows(rows,'movie')).toEqual([{type:'movie'},{type:null}])
})

test('only RSS sources may search without words',()=>{
 expect(queryOptional(sources,['r'])).toBe(true)
 expect(queryOptional(sources,['a','r'])).toBe(false)
 expect(queryOptional(sources,[])).toBe(false)
})

test('a tab is used once it has searched',()=>{
 const t=createSearchTabs(),s=activeSearch(t)
 expect(searchUsed(s)).toBe(false)
 s.statuses={a:{state:'running',count:0}}
 expect(searchUsed(s)).toBe(true)
})
