import {expect,test} from 'vitest'
import {sanitizeBrowserHTML} from '../src/browser-helper'
test('a large KAT result container keeps its 75 rows after inert data is stripped',()=>{
 const rows=Array.from({length:75},(_,i)=>`<tr id="r${i}" style="${'color:red;'.repeat(400)}"><td><a class="cellMainLink" href="/detail/${i}">Result ${i}</a><a data-download="1" href="/get/${i}">Get</a></td></tr>`).join('')
 const raw=`<table class="data">${rows}</table>`,html=sanitizeBrowserHTML(raw)
 expect(new TextEncoder().encode(raw).length).toBeGreaterThan(262144)
 expect(new DOMParser().parseFromString(html,'text/html').querySelectorAll('table.data tr[id]:has(a[data-download])')).toHaveLength(75)
 expect(new TextEncoder().encode(html).length).toBeLessThanOrEqual(262144)
})
