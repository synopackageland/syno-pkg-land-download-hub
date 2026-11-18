const torrentPrefix='data:application/x-bittorrent;base64,';
export const maxTorrentBytes=512*1024;

// A task's size: the files chosen for download, else every file, else the
// engine's total or the bytes downloaded so far.
export function taskSize(task:any):number{
 const files=task?.meta?.res?.files,chosen=task?.meta?.opts?.selectFiles;
 const picked=Array.isArray(files)&&Array.isArray(chosen)&&chosen.length?chosen.map((i:number)=>files[i]):files;
 const size=Array.isArray(picked)?picked.reduce((n:number,f:any)=>n+(f?.size||0),0):0;
 return size||task?.meta?.res?.size||task?.progress?.downloaded||0;
}

// The backend re-checks size and bencoded metadata; this only fails early.
export async function readTorrent(file:File):Promise<string>{
 if(!file.name.toLowerCase().endsWith('.torrent'))throw {code:'invalid_torrent'};
 if(file.size>maxTorrentBytes)throw {code:'torrent_too_large'};
 const url=await new Promise<string>((resolve,reject)=>{
  const reader=new FileReader();
  reader.onload=()=>resolve(String(reader.result));
  reader.onerror=()=>reject({code:'invalid_torrent'});
  reader.readAsDataURL(file);
 });
 return torrentPrefix+url.slice(url.indexOf(',')+1);
}
