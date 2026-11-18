import {expect,test} from 'vitest'
import {canStart,linkSummary,startLabel,submitLinks,type LinkItem} from '../src/add-download'

test('the summary counts links by kind in the user language',()=>{
 expect(linkSummary(['file','media','file'],'zh-TW')).toBe('3 個連結：2 個檔案、1 個影片頁')
 expect(linkSummary(['file','media','file'],'en')).toBe('3 links: 2 files, 1 video page')
 expect(linkSummary(['torrent'],'en')).toBe('1 link: 1 torrent')
 expect(linkSummary(['ed2k','extension','unsupported'],'en')).toBe('3 links: 1 ED2K link, 1 extension link, 1 not recognized')
 expect(linkSummary([],'en')).toBe('')
})
test('kinds not classified yet count as links without a part',()=>{
 expect(linkSummary(['file',undefined],'en')).toBe('2 links: 1 file')
})

test('start needs at least one recognized link and a usable location',()=>{
 expect(canStart({count:3,ready:true,busy:false})).toBe(true)
 expect(canStart({count:0,ready:true,busy:false})).toBe(false)
 expect(canStart({count:3,ready:false,busy:false})).toBe(false)
 expect(canStart({count:3,ready:true,busy:true})).toBe(false)
 expect(startLabel('zh-TW',3)).toBe('加入 3 個任務')
 expect(startLabel('en',1)).toBe('Add 1 task')
 expect(startLabel('en',3)).toBe('Add 3 tasks')
 expect(startLabel('en',0)).toBe('Add')
})

type Reply={key:string;status:string;kind?:string;ids?:string[];code?:string}
function server(answer:(item:LinkItem,round:number)=>Omit<Reply,'key'>){
 const calls:{method:string;params:any}[]=[]
 let round=0
 const call=async(method:string,params:any)=>{
  calls.push({method,params});round++
  return {results:params.items.map((item:LinkItem)=>({key:item.key,...answer(item,round)}))}
 }
 return {calls,call}
}
const items=(...urls:string[])=>urls.map((url,i)=>({key:'key-000'+i,url}))

test('each link gets its own result and one failure does not stop the others',async()=>{
 const {calls,call}=server(item=>item.url.includes('bad')?{status:'failed',code:'url_denied'}:{status:'created',kind:'file',ids:['t-'+item.key]})
 const results=await submitLinks(call,'/home/Downloads',items('https://a.example/1','https://bad.example/2','https://a.example/3'))
 expect(results.map(r=>[r.url,r.status,r.code])).toEqual([['https://a.example/1','created',undefined],['https://bad.example/2','failed','url_denied'],['https://a.example/3','created',undefined]])
 expect(calls).toHaveLength(1)
 expect(calls[0]).toEqual({method:'downloads.create_many',params:{path:'/home/Downloads',items:items('https://a.example/1','https://bad.example/2','https://a.example/3')}})
})
test('pending admission receipts never poll metadata in the dialog',async()=>{
 const {calls,call}=server(()=>({status:'pending',code:'deferred'}))
 const results=await submitLinks(call,'/home',items('https://a.example/1'))
 expect(calls).toHaveLength(1);expect(results[0].status).toBe('created')
})
test('long lists and large torrent files are sent in parts',async()=>{
 const {calls,call}=server(()=>({status:'created',ids:['x']}))
 const many=Array.from({length:40},(_,i)=>({key:'key-'+String(i).padStart(4,'0'),url:'https://a.example/'+i}))
 const torrents=[0,1].map(i=>({key:'torrent-000'+i,url:'data:application/x-bittorrent;base64,'+'A'.repeat(600_000)}))
 const results=await submitLinks(call,'/home',[...many,...torrents])
 expect(results.every(r=>r.status==='created')).toBe(true)
 expect(calls.map(c=>c.params.items.length)).toEqual([32,9,1])
})
test('a refused request fails its links, but a lost session stops the dialog',async()=>{
 const results=await submitLinks(async()=>{throw {code:'engine_busy'}},'/home',items('https://a.example/1'))
 expect(results[0]).toMatchObject({status:'failed',code:'engine_busy'})
 await expect(submitLinks(async()=>{throw {code:'session_required'}},'/home',items('https://a.example/1'))).rejects.toEqual({code:'session_required'})
})
