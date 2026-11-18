import {expect,test,vi} from 'vitest'
import {extractLinks,parseLinks,readClipboardLinks} from '../src/links'

const ed2k='ed2k://|file|ubuntu.iso|1048576|0123456789ABCDEF0123456789ABCDEF|/'
const magnet='magnet:?xt=urn:btih:'+'a'.repeat(40)+'&dn=Album'

test('links are taken out of surrounding text and punctuation',()=>{
 expect(extractLinks(`Download: https://example.com/a.zip, then (${magnet}) 謝謝！\n${ed2k} end.`)).toEqual(['https://example.com/a.zip',magnet,ed2k])
 expect(extractLinks('看這個https://example.com/影片?id=1。還有「http://a.example/b.torrent」')).toEqual(['https://example.com/影片?id=1','http://a.example/b.torrent'])
 expect(extractLinks('https://en.wikipedia.org/wiki/Foo_(bar).')).toEqual(['https://en.wikipedia.org/wiki/Foo_(bar)'])
})
test('several links on one line or many lines keep their order without duplicates',()=>{
 expect(extractLinks('https://a.example/1 https://a.example/2\n\nhttps://a.example/1\thttps://a.example/3')).toEqual(['https://a.example/1','https://a.example/2','https://a.example/3'])
 expect(extractLinks('no links here, only text')).toEqual([])
})
test('lines without a link are reported, the others still count',()=>{
 expect(parseLinks('https://a.example/1\nnot a link\n\n  \nftp-ish words\nhttps://a.example/2')).toEqual({links:['https://a.example/1','https://a.example/2'],unknown:['not a link','ftp-ish words']})
})

function nav(state:string|Error|undefined,text:string|Error='https://a.example/1 hello'){
 const readText=vi.fn(async()=>{if(text instanceof Error)throw text;return text})
 const query=vi.fn(async()=>{if(state instanceof Error)throw state;return {state}})
 return {readText,value:{clipboard:{readText},permissions:state===undefined?undefined:{query}} as any}
}
test('the clipboard is read only when the browser already allows it',async()=>{
 const granted=nav('granted')
 expect(await readClipboardLinks(granted.value)).toEqual(['https://a.example/1'])
 for(const state of ['prompt','denied',new TypeError('clipboard-read is not a permission name'),undefined]){
  const n=nav(state)
  expect(await readClipboardLinks(n.value)).toEqual([])
  expect(n.readText).not.toHaveBeenCalled()
 }
})
test('a refused or failing clipboard read stays silent',async()=>{
 expect(await readClipboardLinks(nav('granted',new DOMException('Document is not focused','NotAllowedError')).value)).toEqual([])
 expect(await readClipboardLinks({} as any)).toEqual([])
 expect(await readClipboardLinks(nav('granted','just words').value)).toEqual([])
})
