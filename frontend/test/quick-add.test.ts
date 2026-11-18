import {expect,test} from 'vitest'
import {addRecent,clipboardReadable,pastedLinks,pinRecent,quickLinks} from '../src/quick-add'

test('the one-line field takes every link it holds, up to one batch',()=>{
 expect(quickLinks('  https://a.example/1  ')).toEqual(['https://a.example/1'])
 expect(quickLinks('copy: https://a.example/1 and magnet:?xt=urn:btih:'+'b'.repeat(40))).toHaveLength(2)
 expect(quickLinks('not a link')).toEqual([])
 expect(quickLinks(Array.from({length:40},(_,i)=>'https://a.example/'+i).join(' '))).toHaveLength(32)
})
test('Paste reads the clipboard only in a secure context with the async API',()=>{
 const readText=async()=>''
 expect(clipboardReadable({isSecureContext:true,navigator:{clipboard:{readText}}})).toBe(true)
 // Plain HTTP: the browser has no navigator.clipboard, or refuses the read.
 expect(clipboardReadable({isSecureContext:false,navigator:{clipboard:{readText}}})).toBe(false)
 expect(clipboardReadable({isSecureContext:true,navigator:{}})).toBe(false)
})
test('a refused clipboard read is no error, only no links',async()=>{
 expect(await pastedLinks({clipboard:{readText:async()=>'see https://a.example/x.iso'}})).toEqual(['https://a.example/x.iso'])
 expect(await pastedLinks({clipboard:{readText:async()=>{throw new DOMException('denied','NotAllowedError')}}})).toBeUndefined()
 expect(await pastedLinks({})).toBeUndefined()
})
test('downloads added here lead the list, newest first',()=>{
 const rows=['a','b','c','d'].map(id=>({id}))
 expect(pinRecent(rows,['d','b']).map(r=>r.id)).toEqual(['d','b','a','c'])
 expect(pinRecent(rows,[])).toBe(rows)
 // An id the list no longer has (cleared) pins nothing.
 expect(pinRecent(rows,['gone','c']).map(r=>r.id)).toEqual(['c','a','b','d'])
})
test('the recent list keeps each id once, newest first, and stays short',()=>{
 expect(addRecent(['a','b'],['c','d'])).toEqual(['d','c','a','b'])
 expect(addRecent(['a','b'],['b'])).toEqual(['b','a'])
 expect(addRecent([],Array.from({length:30},(_,i)=>String(i)))).toHaveLength(20)
})
