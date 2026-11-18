export const mediaTools=['yt-dlp','ffmpeg','deno','gallery-dl'] as const
// Each tool as its project writes its name; the keys stay the backend's.
export const toolNames:Record<string,string>={'yt-dlp':'yt-dlp',ffmpeg:'FFmpeg',deno:'Deno','gallery-dl':'gallery-dl'}
// A failed media download restarts with yt-dlp's resume and archive.
export function mediaRetryable(task:any){return task?.protocol==='media'&&task.status==='error'}
// FFmpeg-Builds tags its builds by date and time; show them like yt-dlp's dates.
export function toolVersion(version?:string){
 const m=/^(\d{4})-(\d{2})-(\d{2})-(\d{2})-(\d{2})$/.exec(version||'')
 return m?`${m[1]}.${m[2]}.${m[3]} ${m[4]}:${m[5]}`:version||''
}
