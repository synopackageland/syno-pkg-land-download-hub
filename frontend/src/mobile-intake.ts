import type { NativeIntakeItem } from '@synopackageland/sdk/mobile'
import { deepLinkURL } from './deep-link'
import { maxTorrentBytes } from './download-submission'
import { extractLinks } from './links'
import { totalCap } from './link-groups'

export type IntakeDraft = { text: string; files: File[] }

// The SDK consumes the handle; this App only turns its contents into an
// editable draft. No intake item creates a download until the owner confirms.
export function intakeDraft(items: readonly NativeIntakeItem[]): IntakeDraft {
 const links: string[] = [], files: File[] = []
 const refuse = () => { throw { code: 'intake_unsupported' } }
 for (const item of items) {
  if (item.kind === 'file') {
   if (!item.filename.toLowerCase().endsWith('.torrent') || item.size > maxTorrentBytes) refuse()
   let bytes: Uint8Array
   try { bytes = Uint8Array.from(atob(item.base64), c => c.charCodeAt(0)) } catch { return refuse() }
   if (bytes.length !== item.size) refuse()
   files.push(new File([bytes! as BlobPart], item.filename, { type: 'application/x-bittorrent' }))
  } else {
   const found = item.kind === 'url' ? [item.url] : extractLinks(item.text)
   if (!found.length || found.some(url => !deepLinkURL(url))) refuse()
   links.push(...found)
  }
 }
 const unique = [...new Set(links)]
 if ((!unique.length && !files.length) || unique.length + files.length > totalCap) refuse()
 return { text: unique.join('\n'), files }
}
