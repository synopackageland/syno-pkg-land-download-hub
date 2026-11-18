import {expect,test} from 'vitest'
import {decodeText,fetchSaved,fileStationURL,htmlDocument,htmlPolicy,playable,previewLimits,previewType,readLimited,withinLimit} from '../src/preview'
test('each previewable type maps to its viewer; others have none',()=>{
 expect(previewType('a.JPG')).toEqual({kind:'image',mime:'image/jpeg'})
 expect(previewType('logo.svg')).toEqual({kind:'image',mime:'image/svg+xml'})
 expect(previewType('page.htm')?.kind).toBe('html')
 expect(previewType('guide.pdf')?.kind).toBe('pdf')
 expect(previewType('clip.mkv')?.kind).toBe('video')
 expect(previewType('song.flac')?.kind).toBe('audio')
 // Markdown shows as plain text.
 expect(previewType('README.md')).toEqual({kind:'text',mime:'text/plain'})
 expect(previewType('setup.exe')).toBeUndefined()
 expect(previewType('noext')).toBeUndefined()
})
test('size caps: image 50 MiB, HTML 5 MiB, PDF 200 MiB; text reads its first 1 MiB; media streams',()=>{
 const MiB=1024**2
 expect(previewLimits).toEqual({image:50*MiB,html:5*MiB,pdf:200*MiB,text:MiB})
 expect(withinLimit('image',50*MiB)).toBe(true);expect(withinLimit('image',50*MiB+1)).toBe(false)
 expect(withinLimit('html',5*MiB+1)).toBe(false)
 expect(withinLimit('pdf',200*MiB+1)).toBe(false)
 expect(withinLimit('text',10*1024*MiB)).toBe(true)
 expect(withinLimit('video',10*1024*MiB)).toBe(true)
})
test('a type the browser cannot play falls back',()=>{
 expect(playable('video/mp4',()=> 'maybe')).toBe(true)
 expect(playable('video/x-matroska',()=> '')).toBe(false)
})
test('the media URL is File Station open mode with the session token, same origin',()=>{
 const u=new URL(fileStationURL('/home/Downloads/a b&c.mp4','tok'),'https://nas.invalid')
 expect(u.pathname).toBe('/webapi/entry.cgi')
 expect(Object.fromEntries(u.searchParams)).toEqual({api:'SYNO.FileStation.Download',version:'2',method:'download',path:'["/home/Downloads/a b&c.mp4"]',mode:'open',SynoToken:'tok'})
})
test('a saved page gets a no-script, no-network policy ahead of its own markup',()=>{
 expect(htmlPolicy).toContain("default-src 'none'");expect(htmlPolicy).toContain('img-src data:');expect(htmlPolicy).toContain("style-src 'unsafe-inline'")
 expect(htmlPolicy).not.toContain('script-src')
 const doc=htmlDocument('<html><head><script>alert(1)</script></head><body>hi</body></html>')
 expect(doc.startsWith('<meta http-equiv="Content-Security-Policy"')).toBe(true)
 expect(doc).toContain('<body>hi</body>')
})
function stream(chunks:number[][],onCancel=()=>{}){
 return new Response(new ReadableStream({pull(c){const next=chunks.shift();if(next)c.enqueue(new Uint8Array(next));else c.close()},cancel:onCancel}))
}
test('reading stops at the cap and cancels the rest of the transfer',async()=>{
 let canceled=false
 const r=await readLimited(stream([[1,2,3],[4,5,6],[7,8,9]],()=>{canceled=true}),5)
 expect([...r.bytes]).toEqual([1,2,3,4,5]);expect(r.truncated).toBe(true);expect(canceled).toBe(true)
 const all=await readLimited(stream([[1,2],[3]]),5)
 expect([...all.bytes]).toEqual([1,2,3]);expect(all.truncated).toBe(false)
})
test('bytes come from a same-origin File Station POST with the token in DSM\'s header; a DSM refusal becomes a preview error',async()=>{
 const seen:any[]=[]
 const ok=(async(url:string,init:any)=>{seen.push({url,init});return new Response(new Uint8Array([137,80]),{headers:{'content-type':'image/png'}})}) as any
 const r=await fetchSaved('/home/a.png','tok',10,new AbortController().signal,ok)
 expect([...r.bytes]).toEqual([137,80])
 expect(seen[0].url).toBe('/webapi/entry.cgi')
 expect(seen[0].init).toMatchObject({method:'POST',credentials:'same-origin',mode:'same-origin'})
 expect(seen[0].init.headers).toEqual({'X-SYNO-TOKEN':'tok'})
 expect(Object.fromEntries(seen[0].init.body)).toMatchObject({api:'SYNO.FileStation.Download',mode:'open',path:'["/home/a.png"]'})
 expect(seen[0].init.body.has('SynoToken')).toBe(false)
 const denied=(async()=>new Response('{"success":false,"error":{"code":407}}',{headers:{'content-type':'application/json; charset=utf-8'}})) as any
 await expect(fetchSaved('/home/a.png','tok',10,new AbortController().signal,denied)).rejects.toEqual({code:'preview_denied'})
 const missing=(async()=>new Response('{"success":false,"error":{"code":408}}',{headers:{'content-type':'application/json'}})) as any
 await expect(fetchSaved('/home/a.png','tok',10,new AbortController().signal,missing)).rejects.toEqual({code:'preview_missing'})
 const saved=(async()=>new Response('{"a":1}',{headers:{'content-type':'application/json','content-disposition':'inline; filename="a.json"'}})) as any
 expect(decodeText((await fetchSaved('/home/a.json','tok',10,new AbortController().signal,saved)).bytes)).toBe('{"a":1}')
})
test('text decodes as UTF-8 unless it carries a UTF-16 mark',()=>{
 expect(decodeText(new TextEncoder().encode('héllo'))).toBe('héllo')
 expect(decodeText(new Uint8Array([0xff,0xfe,104,0,105,0]))).toBe('hi')
})
