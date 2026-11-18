import {expect,test} from 'vitest'
import {collectLinks,expandPattern,groupOf,patternCap,totalCap} from '../src/link-groups'
test('numbered patterns expand in order, keeping a zero-padded width',()=>{
 expect(expandPattern('https://a.example/img[1-3].jpg')).toEqual({urls:['https://a.example/img1.jpg','https://a.example/img2.jpg','https://a.example/img3.jpg'],capped:false})
 expect(expandPattern('https://a.example/ep[08-10].mp4')?.urls).toEqual(['https://a.example/ep08.mp4','https://a.example/ep09.mp4','https://a.example/ep10.mp4'])
 expect(expandPattern('https://a.example/s[1-2]e[1-2].mkv')?.urls).toEqual(['https://a.example/s1e1.mkv','https://a.example/s1e2.mkv','https://a.example/s2e1.mkv','https://a.example/s2e2.mkv'])
 expect(expandPattern('https://a.example/plain.zip')).toBeNull()
 expect(expandPattern('http://[::1]/file.zip')).toBeNull()
})
test('one pattern names at most 100 links and says it was capped',()=>{
 expect(patternCap).toBe(100)
 const big=expandPattern('https://a.example/[1-100000].jpg')!
 expect(big.urls).toHaveLength(100);expect(big.capped).toBe(true);expect(big.urls[99]).toBe('https://a.example/100.jpg')
 expect(expandPattern('https://a.example/[1-100].jpg')!.capped).toBe(false)
 expect(expandPattern('https://a.example/[1-20]/[1-20].jpg')!).toMatchObject({capped:true})
})
test('collecting removes repeats, counts expansions and caps the batch at 256',()=>{
 const c=collectLinks('see https://a.example/img[1-3].jpg\nhttps://a.example/img2.jpg and https://b.example/x.pdf\nhttps://b.example/x.pdf\nno link here')
 expect(c.links.map(l=>l.url)).toEqual(['https://a.example/img1.jpg','https://a.example/img2.jpg','https://a.example/img3.jpg','https://b.example/x.pdf'])
 expect(c.links.map(l=>l.pattern)).toEqual([true,true,true,false])
 expect(c.duplicates).toBe(2);expect(c.expanded).toBe(3);expect(c.unknown).toEqual(['no link here'])
 expect(totalCap).toBe(256)
 const lots=collectLinks(['https://a.example/a[1-100].jpg','https://a.example/b[1-100].jpg','https://a.example/c[1-100].jpg'].join('\n'))
 expect(lots.links).toHaveLength(256);expect(lots.cappedTotal).toBe(true);expect(lots.cappedPattern).toBe(false)
})
test('groups follow the backend kind and route; no classifier of its own',()=>{
 expect(groupOf(undefined)).toBe('pending')
 expect(groupOf({kind:'file'})).toBe('file')
 expect(groupOf({kind:'file',route:'gopeed'})).toBe('file')
 expect(groupOf({kind:'file',route:'yt-dlp'})).toBe('media')
 expect(groupOf({kind:'media'})).toBe('media')
 expect(groupOf({kind:'torrent'})).toBe('torrent')
 expect(groupOf({kind:'ed2k'})).toBe('ed2k')
 expect(groupOf({kind:'extension'})).toBe('extension')
 expect(groupOf({kind:'blocked'})).toBe('none')
 expect(groupOf({kind:'unsupported'})).toBe('none')
})
