import {format} from '@synopackageland/sdk'
import {appCopy} from './app-copy'

// GitHub answers update checks and tool downloads. Anonymous calls share a
// small hourly limit per public address; an administrator's token raises it.

// GitHub's documented fine-grained token form, prefilled: a name, what it is
// for and a year's expiry. No permission is asked for, so the token reads
// public repositories only, which is all the checks need.
export const GITHUB_TOKEN_URL = 'https://github.com/settings/personal-access-tokens/new?name=Download+Hub+on+Synology&description=Raises+the+GitHub+rate+limit+for+Download+Hub+update+checks+and+downloads.+No+extra+permissions+needed&expires_in=365'

export type GitHubFailure = {kind: 'limited', reset?: string} | {kind: 'invalid'}

const limits = ['tool_source_rate_limited', 'search_update_rate_limited']

// The backend names the reset time in the message (RFC 3339, UTC); a tool
// download the daemon ran reports it as resetAt.
export function githubFailure(e: any): GitHubFailure | undefined {
  if (limits.includes(e?.code)) {
    const reset = String(e?.resetAt || e?.message || '').match(/\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ/)?.[0]
    return reset ? {kind: 'limited', reset} : {kind: 'limited'}
  }
  if (e?.code === 'github_token_invalid') return {kind: 'invalid'}
  return undefined
}

export function resetTime(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

// What went wrong, in the page's words: when a limit resets, or that the
// saved token no longer works.
export function githubText(f: GitHubFailure, locale: string) {
  const copy = (key: string) => appCopy(locale, key)
  if (f.kind === 'invalid') return copy('github.invalid')
  return f.reset ? format(copy('github.limited'), {time: resetTime(f.reset)}, locale) : copy('github.limitedLater')
}
