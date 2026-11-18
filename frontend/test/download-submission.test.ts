import {expect,test} from 'vitest'
import {readTorrent,taskSize} from '../src/download-submission'

const btRes={name:'Album',hash:'a'.repeat(40),files:[{name:'a.flac',path:'cd1',size:3},{name:'cover.jpg',path:'',size:4}]}
test('uploaded torrents become bounded data addresses',async()=>{
 const file=new File([new Uint8Array([100,101])],'a.torrent',{type:''})
 expect(await readTorrent(file)).toBe('data:application/x-bittorrent;base64,ZGU=')
 await expect(readTorrent(new File([new Uint8Array(512*1024+1)],'big.torrent'))).rejects.toEqual({code:'torrent_too_large'})
 await expect(readTorrent(new File(['x'],'notes.txt'))).rejects.toEqual({code:'invalid_torrent'})
})
// A torrent's size is the files chosen for download, else every file.
test('a task size counts the chosen files',()=>{
 const res={size:7,files:btRes.files}
 expect(taskSize({meta:{res}})).toBe(7)
 expect(taskSize({meta:{res,opts:{selectFiles:[]}}})).toBe(7)
 expect(taskSize({meta:{res,opts:{selectFiles:[1]}}})).toBe(btRes.files[1].size)
 expect(taskSize({meta:{res:{size:9,files:[]}}})).toBe(9)
 expect(taskSize({progress:{downloaded:5}})).toBe(5)
})
