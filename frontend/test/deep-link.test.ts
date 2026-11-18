import {expect,test} from 'vitest'
import {deepLinkURL,maxDeepLinkURL,parseDeepLink,withoutDeepLink} from '../src/deep-link'

const q=(s:string)=>new URLSearchParams(s)
const magnet='magnet:?xt=urn:btih:'+'a'.repeat(40)+'&dn=Album'
test('?add= queues one link for the task list',()=>{
 expect(parseDeepLink(q('add='+encodeURIComponent('https://example.com/a.zip')))).toEqual({page:'downloads',add:'https://example.com/a.zip'})
 expect(parseDeepLink(q('add='+encodeURIComponent(magnet)))).toEqual({page:'downloads',add:magnet})
 // A link to add always opens the task list, whatever view says.
 expect(parseDeepLink(q('view=settings&add=https%3A%2F%2Fexample.com%2Fa')).page).toBe('downloads')
})
test('only a value that is exactly one supported link is taken',()=>{
 for(const bad of ['javascript:alert(1)','data:text/html,hi','file:///etc/passwd','ftp://example.com/a','https://a.example/1 https://a.example/2','see https://a.example/1','//example.com/a','https://a.example/\u0000x','',' '])
  expect(deepLinkURL(bad),bad).toBeUndefined()
 expect(deepLinkURL('  https://a.example/1  ')).toBe('https://a.example/1')
 expect(deepLinkURL('https://a.example/'+'x'.repeat(maxDeepLinkURL))).toBeUndefined()
 expect(parseDeepLink(q('add=javascript%3Aalert(1)'))).toEqual({})
})
test('?view= opens one of the App\'s own pages; anything else is ignored',()=>{
 expect(parseDeepLink(q('view=downloads'))).toEqual({page:'downloads'})
 expect(parseDeepLink(q('view=search'))).toEqual({page:'search'})
 expect(parseDeepLink(q('view=settings'))).toEqual({page:'settings'})
 for(const bad of ['view=plugins','view=binary','view=https://evil.example','view=..%2Fdownloads','view=Downloads'])expect(parseDeepLink(q(bad)),bad).toEqual({})
})
test('a filter opens the task list on that tab',()=>{
 expect(parseDeepLink(q('view=downloads&filter=failed'))).toEqual({page:'downloads',filter:'failed'})
 expect(parseDeepLink(q('filter=active'))).toEqual({page:'downloads',filter:'active'})
 expect(parseDeepLink(q('view=search&filter=active'))).toEqual({page:'search'})
 expect(parseDeepLink(q('view=downloads&filter=everything'))).toEqual({page:'downloads'})
})
test('the DSM launch\'s parameters come before the page address',()=>{
 expect(parseDeepLink({view:'search'},q('view=settings'))).toEqual({page:'search'})
 expect(parseDeepLink({route:'/a'},q('view=settings'))).toEqual({page:'settings'})
 expect(parseDeepLink(undefined,q(''))).toEqual({})
})
test('the address loses only the deep link\'s parameters',()=>{
 expect(withoutDeepLink('https://nas.example/download-hub/?add=https%3A%2F%2Fa.example&view=downloads&lang=de#x')).toBe('https://nas.example/download-hub/?lang=de#x')
 expect(withoutDeepLink('https://nas.example/download-hub/?lang=de')).toBeUndefined()
})
