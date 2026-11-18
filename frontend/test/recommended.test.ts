import {expect,test} from 'vitest'
import {cardIcon,recommendedExtensions,recommendedCards} from '../src/recommended'
const store=[
 {id:'leen@gopeed-extention-gofile',title:'Gofile拓展',url:'https://github.com/leenc123/gopeed-extension-gofile',repoUrl:'https://github.com/leenc123/gopeed-extension-gofile'},
 {id:'Gwkang@gopeed-extension-gofile',title:'Gofile 下载器',url:'https://github.com/iGwkang/gopeed-extension-gofile',repoUrl:'https://github.com/iGwkang/gopeed-extension-gofile',icon:'https://raw.githubusercontent.com/iGwkang/gopeed-extension-gofile/main/icon.png'},
 {id:'someone@renamed',title:'MediaFire Downloader',url:'https://github.com/Locon213/gopeed-extension-mediafire',repoUrl:'https://github.com/Locon213/gopeed-extension-mediafire/'},
 {id:'impostor@pixeldrain',title:'Pixeldrain',url:'https://github.com/impostor/pixeldrain',repoUrl:'https://github.com/impostor/pixeldrain'},
 {id:'silasmariusz@debrid',title:'Debrid & premium hosts',url:'https://github.com/silasmariusz/gopeed-ext-debrid',repoUrl:'https://github.com/silasmariusz/gopeed-ext-debrid'},
]
test('the recommended set is the four file-hosting extensions, without MEGA',()=>{
 expect(recommendedExtensions.map(x=>x.key)).toEqual(['gofile','mediafire','pixeldrain','debrid'])
 expect(JSON.stringify(recommendedExtensions).toLowerCase()).not.toContain('mega')
 expect(new Set(recommendedExtensions.map(x=>x.id)).size).toBe(4)
})
test('cards match the store by identity or repository, never by title, and hide what the store lacks',()=>{
 const cards=recommendedCards(store,[])
 // One Gofile only, the chosen identity; a title match is not enough for Pixeldrain.
 expect(cards.map(c=>[c.key,c.entry.id])).toEqual([['gofile','Gwkang@gopeed-extension-gofile'],['mediafire','someone@renamed'],['debrid','silasmariusz@debrid']])
 expect(cards.every(c=>!c.installed)).toBe(true)
 expect(recommendedCards([],[])).toEqual([])
})
test('an installed extension shows Installed, by identity or by its repository',()=>{
 const cards=recommendedCards(store,[{identity:'Gwkang@gopeed-extension-gofile'},{identity:'other',repository:{url:'https://github.com/silasmariusz/gopeed-ext-debrid.git'}}])
 expect(Object.fromEntries(cards.map(c=>[c.key,c.installed]))).toEqual({gofile:true,mediafire:false,debrid:true})
})
test('a card shows only an inline or same-origin icon, since DSM blocks images from other hosts',()=>{
 expect(cardIcon('https://raw.githubusercontent.com/iGwkang/gopeed-extension-gofile/main/icon.png')).toBeUndefined()
 expect(cardIcon('//raw.githubusercontent.com/x.png')).toBeUndefined()
 expect(cardIcon('data:image/svg+xml;base64,PHN2Zz4=')).toBeUndefined()
 expect(cardIcon(undefined)).toBeUndefined()
 expect(cardIcon('data:image/png;base64,iVBORw0KGgo=')).toBe('data:image/png;base64,iVBORw0KGgo=')
 expect(cardIcon('/webman/3rdparty/SynoPkgGopeed/icon.png')).toBe('/webman/3rdparty/SynoPkgGopeed/icon.png')
})
