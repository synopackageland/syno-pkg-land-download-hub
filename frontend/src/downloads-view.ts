// The Downloads page's pure view model: which group a task sits in, the file
// kind its tile shows, the source's host and the method that fetched it.
export type DownloadGroup = 'active' | 'paused' | 'failed' | 'done'
export const groupOrder: DownloadGroup[] = ['active', 'paused', 'failed', 'done']
// The Downloads page's tabs, as DSM's own lists; Downloading also lists
// paused downloads.
export type DownloadTab = 'all' | 'active' | 'done' | 'failed'
export const tabGroups: Record<DownloadTab, DownloadGroup[]> = { all: groupOrder, active: ['active', 'paused'], done: ['done'], failed: ['failed'] }
export const downloadTabs = Object.keys(tabGroups) as DownloadTab[]
// A tab's rows in page order: completed ones most recent first.
export function tabRows<T extends { group: DownloadGroup; completed?: number | null }>(rows: T[], tab: DownloadTab) {
  return groupRows(rows).filter((g) => tabGroups[tab].includes(g.key)).flatMap((g) => g.rows)
}

// A failed download or save is neither active nor complete; a cancel in
// progress is still active.
export function downloadFailed(x: any) {
  if (x.publication?.manualPaused || x.publication?.pausePending) return false
  return x.status === 'error' || (!!x.publication?.error && x.publication.error !== 'CANCEL_REQUESTED')
}
export function downloadComplete(x: any) {
  return x.status === 'done' && !downloadFailed(x)
}
// A finished download whose save is waiting, copying or paused by its owner
// still has work left, so it sits with the downloads and their Pause all.
function saving(x: any) {
  const p = x.publication
  return !!p && !p.published && (p.manualPaused || (!p.error && (p.state === 'pending' || p.state === 'copying')))
}
export function taskGroup(x: any): DownloadGroup {
  if (downloadFailed(x)) return 'failed'
  if (downloadComplete(x) && !saving(x)) return 'done'
  return x.status === 'pause' || x.publication?.manualPaused ? 'paused' : 'active'
}

// The groups in page order, empty ones left out. Completed rows show the most
// recent first; the others keep the engine's order.
export function groupRows<T extends { group: DownloadGroup; completed?: number | null }>(rows: T[]) {
  return groupOrder
    .map((key) => {
      const list = rows.filter((r) => r.group === key)
      if (key === 'done') list.sort((a, b) => (b.completed ?? 0) - (a.completed ?? 0))
      return { key, rows: list }
    })
    .filter((g) => g.rows.length)
}

export type FileKind = 'video' | 'audio' | 'image' | 'doc' | 'archive' | 'web' | 'text' | 'disc' | 'torrent' | 'file'
const extensions: Record<Exclude<FileKind, 'torrent' | 'file'>, string[]> = {
  video: ['mp4', 'm4v', 'mkv', 'webm', 'mov', 'avi', 'wmv', 'flv', 'ts', 'm2ts', 'mpg', 'mpeg', '3gp', 'ogv'],
  audio: ['mp3', 'm4a', 'aac', 'flac', 'wav', 'ogg', 'oga', 'opus', 'wma', 'alac', 'ape'],
  image: ['jpg', 'jpeg', 'png', 'gif', 'webp', 'avif', 'bmp', 'svg', 'ico', 'heic', 'tif', 'tiff'],
  doc: ['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'epub', 'rtf'],
  archive: ['zip', 'rar', '7z', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'zst', 'deb', 'rpm', 'apk', 'dmg', 'pkg', 'msi', 'exe', 'spk'],
  web: ['html', 'htm', 'xhtml', 'mhtml'],
  text: ['txt', 'md', 'markdown', 'log', 'csv', 'json', 'xml', 'yaml', 'yml', 'ini', 'conf', 'srt', 'vtt', 'ass', 'nfo', 'sh', 'py', 'js', 'ts'],
  disc: ['iso', 'img', 'bin', 'cue', 'vhd', 'vmdk', 'qcow2'],
}
// A name's last extension in lower case, or '' for none.
export function extension(name: string) {
  const m = /\.([a-z0-9]{1,8})$/i.exec(name || '')
  return m ? m[1].toLowerCase() : ''
}
// A video download has no extension until its file is known.
export function fileKind(name: string, protocol?: string): FileKind {
  const ext = extension(name)
  // .ts reads as TypeScript text only when it is not a media download.
  if (ext === 'ts' && protocol !== 'media') return 'text'
  for (const [kind, list] of Object.entries(extensions)) if (list.includes(ext)) return kind as FileKind
  if (protocol === 'media') return 'video'
  if (protocol === 'bt') return 'torrent'
  return 'file'
}
// The tile's short label: the extension, else the method.
export function tileLabel(name: string, protocol?: string) {
  const ext = extension(name)
  if (ext) return ext.slice(0, 4).toUpperCase()
  if (protocol === 'bt') return 'BT'
  if (protocol === 'ed2k') return 'ED2K'
  return ''
}

// The host a link came from, without "www."; '' for a link with no host
// (magnet, ed2k) or a search download whose link is hidden.
export function sourceHost(url: string) {
  if (!/^https?:\/\//i.test(url || '')) return ''
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return ''
  }
}
// The copy key of the method that fetched a task.
export function methodKey(protocol?: string) {
  return 'downloads.method.' + (['bt', 'ed2k', 'media'].includes(protocol || '') ? protocol : 'http')
}

// A task's link can be copied only when it is a real link the owner gave.
// The backend hides a search download's link, which may carry a passkey.
export function copyableLink(x: any) {
  const url = x.meta?.req?.url
  if (typeof url !== 'string') return ''
  return /^(https?|ftp|magnet|ed2k):/i.test(url) ? url : ''
}

// The page header's live summary.
export function liveSummary(rows: { group: DownloadGroup; speed: number | null }[]) {
  const active = rows.filter((r) => r.group === 'active')
  return { downloading: active.length, speed: active.reduce((n, r) => n + (r.speed || 0), 0) }
}

// Speed samples the detail panel takes while it is open: the last window of
// time only, one sample a poll.
export function createSampler(windowMs = 120_000) {
  let points: { t: number; v: number }[] = []
  return {
    add(t: number, v: number) {
      points.push({ t, v: Math.max(0, v || 0) })
      points = points.filter((p) => p.t > t - windowMs)
    },
    clear() {
      points = []
    },
    get points() {
      return points
    },
    get peak() {
      return points.reduce((n, p) => Math.max(n, p.v), 0)
    },
  }
}
// The sparkline's polyline for samples in a w×h box; the newest sits at the right.
export function sparkline(points: { t: number; v: number }[], w: number, h: number, windowMs = 120_000) {
  if (points.length < 2) return ''
  const end = points[points.length - 1].t,
    peak = Math.max(1, ...points.map((p) => p.v))
  return points
    .map((p) => `${Math.round((1 - (end - p.t) / windowMs) * w * 10) / 10},${Math.round((h - (p.v / peak) * (h - 2)) * 10) / 10}`)
    .join(' ')
}

// A row's bar in the kit's Progress tones; a saved download's panel keeps the primary one.
export const barTone = { active: 'primary', none: 'primary', paused: 'warning', failed: 'danger' } as const
