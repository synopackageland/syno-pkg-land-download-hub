// Previews a saved file in the App. The path comes only from the backend's
// owner-bound reveal; the bytes come through File Station in the user's own
// DSM session, so DSM decides whether this user may read the file.
import { extension } from './downloads-view'

export type PreviewKind = 'image' | 'html' | 'pdf' | 'video' | 'audio' | 'text'
const MiB = 1024 ** 2
// The most the App reads into memory per kind. Video and audio stream; text
// shows only its start.
export const previewLimits: Record<'image' | 'html' | 'pdf' | 'text', number> = { image: 50 * MiB, html: 5 * MiB, pdf: 200 * MiB, text: 1 * MiB }

const types: Record<string, [PreviewKind, string]> = {
  jpg: ['image', 'image/jpeg'], jpeg: ['image', 'image/jpeg'], png: ['image', 'image/png'], gif: ['image', 'image/gif'],
  webp: ['image', 'image/webp'], avif: ['image', 'image/avif'], bmp: ['image', 'image/bmp'], ico: ['image', 'image/x-icon'],
  // An SVG shows only through <img>, where it runs no script.
  svg: ['image', 'image/svg+xml'],
  html: ['html', 'text/html'], htm: ['html', 'text/html'], xhtml: ['html', 'text/html'],
  pdf: ['pdf', 'application/pdf'],
  mp4: ['video', 'video/mp4'], m4v: ['video', 'video/mp4'], webm: ['video', 'video/webm'], ogv: ['video', 'video/ogg'],
  mov: ['video', 'video/quicktime'], mkv: ['video', 'video/x-matroska'], avi: ['video', 'video/x-msvideo'], ts: ['video', 'video/mp2t'],
  mp3: ['audio', 'audio/mpeg'], m4a: ['audio', 'audio/mp4'], aac: ['audio', 'audio/aac'], flac: ['audio', 'audio/flac'],
  wav: ['audio', 'audio/wav'], ogg: ['audio', 'audio/ogg'], oga: ['audio', 'audio/ogg'], opus: ['audio', 'audio/ogg; codecs=opus'],
  txt: ['text', 'text/plain'], md: ['text', 'text/plain'], markdown: ['text', 'text/plain'], log: ['text', 'text/plain'],
  csv: ['text', 'text/plain'], json: ['text', 'text/plain'], xml: ['text', 'text/plain'], yaml: ['text', 'text/plain'],
  yml: ['text', 'text/plain'], ini: ['text', 'text/plain'], conf: ['text', 'text/plain'], nfo: ['text', 'text/plain'],
  srt: ['text', 'text/plain'], vtt: ['text', 'text/plain'], sh: ['text', 'text/plain'], py: ['text', 'text/plain'], js: ['text', 'text/plain'],
}
export function previewType(name: string): { kind: PreviewKind; mime: string } | undefined {
  const t = types[extension(name)]
  return t && { kind: t[0], mime: t[1] }
}
// Whether this browser can play a video or audio type; others get File Station.
export function playable(mime: string, canPlay: (mime: string) => string) {
  return canPlay(mime) !== ''
}
// Whether a file of this size can be shown at all; text shows its first part.
export function withinLimit(kind: PreviewKind, size: number) {
  if (kind === 'video' || kind === 'audio' || kind === 'text') return true
  return !(size > previewLimits[kind])
}

export const DSM_ENTRY = '/webapi/entry.cgi'
// File Station's own download in "open" mode, as a same-origin URL a media
// element can stream with Range requests.
export function fileStationURL(path: string, token: string) {
  const q = new URLSearchParams({ api: 'SYNO.FileStation.Download', version: '2', method: 'download', path: JSON.stringify([path]), mode: 'open', SynoToken: token })
  return `${DSM_ENTRY}?${q}`
}

// The page's policy: no script, no network, inline styles and data: images only.
export const htmlPolicy = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'"
// A saved page for <iframe sandbox="" srcdoc>: the sandbox runs no script and
// gives the page an opaque origin; the policy blocks every request it makes.
export function htmlDocument(html: string) {
  return `<meta http-equiv="Content-Security-Policy" content="${htmlPolicy}"><meta name="referrer" content="no-referrer">${html}`
}

// Reads at most `max` bytes, then stops the transfer.
export async function readLimited(res: Response, max: number) {
  const reader = res.body?.getReader()
  if (!reader) {
    const all = new Uint8Array(await res.arrayBuffer())
    return { bytes: all.slice(0, max), truncated: all.length > max }
  }
  const parts: Uint8Array[] = []
  let total = 0,
    truncated = false
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    const room = max - total
    if (value.length > room) {
      parts.push(value.slice(0, room))
      total = max
      truncated = true
      await reader.cancel().catch(() => {})
      break
    }
    parts.push(value)
    total += value.length
  }
  const bytes = new Uint8Array(total)
  let at = 0
  for (const p of parts) {
    bytes.set(p, at)
    at += p.length
  }
  return { bytes, truncated }
}

// Fetches the file's bytes in the user's DSM session. A File Station refusal
// answers JSON; any other answer is the file. DSM 7.4 ignores a SynoToken in
// a POST body (error 119), so the token goes in DSM's header.
export async function fetchSaved(path: string, token: string, max: number, signal: AbortSignal, fetcher: typeof fetch = globalThis.fetch) {
  const body = new URLSearchParams({ api: 'SYNO.FileStation.Download', version: '2', method: 'download', path: JSON.stringify([path]), mode: 'open' })
  const res = await fetcher(DSM_ENTRY, { method: 'POST', body, headers: { 'X-SYNO-TOKEN': token }, credentials: 'same-origin', mode: 'same-origin', redirect: 'error', cache: 'no-store', signal })
  const type = res.headers.get('content-type') || ''
  if (!res.ok) throw { code: 'preview_unavailable' }
  if (/^application\/json|^text\/plain;.*charset/i.test(type) && res.headers.get('content-disposition') === null) {
    // DSM answers a refusal as JSON with success false; a saved .json file
    // comes with its own disposition.
    const text = new TextDecoder().decode((await readLimited(res, 64 * 1024)).bytes)
    try {
      const json = JSON.parse(text)
      if (json && json.success === false) throw { code: json.error?.code === 408 ? 'preview_missing' : 'preview_denied' }
    } catch (e: any) {
      if (e?.code) throw e
    }
    return { bytes: new TextEncoder().encode(text), truncated: false }
  }
  return readLimited(res, max)
}

// Text as the browser would read it: UTF-8 unless it starts with a UTF-16 mark.
export function decodeText(bytes: Uint8Array) {
  const label = bytes[0] === 0xff && bytes[1] === 0xfe ? 'utf-16le' : bytes[0] === 0xfe && bytes[1] === 0xff ? 'utf-16be' : 'utf-8'
  return new TextDecoder(label).decode(bytes)
}
