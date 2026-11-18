# Download Hub search API

All calls use the existing DSM CGI transport: POST `api.cgi` with JSON
`{"method":"…","params":{…}}`, the current DSM cookie and `X-SYNO-TOKEN`.
Use the SDK's `ctx.call(method, params)` from the UI. No username/UID parameter
is accepted. Sources, credentials, results and downloads belong to the verified
DSM username **and** UID; administrators cannot inspect another user's sources.
The existing Application Privileges entry gate applies to every method.

Success: `{"success":true,"data":…}`. Failure:
`{"success":false,"error":{"code":"search_empty","message":"…"}}`.
Branch on `error.code`, never the translated diagnostic text. Invalid or unknown
JSON fields are refused. The App uses the same APIs below.

## Methods

| Method | Params | Data | Access |
| --- | --- | --- | --- |
| `search.catalog` | `{}` | `Catalog` | Authenticated |
| `search.sources.list` | `{}` | `SourceView[]` | Own sources (a user's first listing seeds the starter sites, below) |
| `search.sources.save` | `SourceInput` | `SourceView` | Own sources |
| `search.sources.enabled` | `{id, enabled}` | `{enabled}` | Own sources |
| `search.sources.delete` | `{id}` | `{deleted:true}` | Own sources |
| `search.sources.test.start` | `{key,id}` | SDK admission | Own source |
| `search.sources.export` | `{id}` | `{yaml}` | Own imported YAML only |
| `search.jobs.start` | `{key,sourceIds,query,page,category?}` | SDK accepted/busy + original job | Own sources |
| `search.jobs.status` | `{id}` | Projected state/progress/operation | Own job, including admins |
| `search.jobs.result` | `{id}` | Completed SDK structured result | Own job |
| `search.jobs.pages` | `{id,offset,limit}` | `{items,offset,total,nextOffset?}` | Own running/succeeded search job |
| `search.jobs.list` | `{}` | `{jobs:[…]}` | Own jobs only |
| `search.jobs.cancel` | `{id}` | Projected SDK job | Own job |
| `search.download` | `{resultId}` | Existing Add's `{results:[…]}` | Own result |
| `search.definitions.update` | `{}` | `Catalog` | Administrator |
| `search.definitions.rollback` | `{}` | `{rolledBack:true}` | Administrator |

Global definition updates are shared read-only catalog maintenance, not shared
credentials or administrator-owned search sources. There is no update schedule.

## Starter sites

A user's first `search.sources.list` seeds their own rows for a fixed set of
general-purpose public sites (see `starterSites` in `starter.go`), keeping only
those the installed catalog supports and that need no sign-in. A user who
already has sources gets none. Either way the user is marked (`starter` in their
state file), so a deleted starter site never comes back and the catalog is read
once; if it cannot be read, the user stays unmarked and the next listing tries
again. Private and adult sites are never seeded; the user opens them in Settings.

## Catalog

```json
{
  "commit": "43359e0d7330606ec5aea76d5b9c5d274a73e7a8",
  "generation": "<64 hex SHA-256>",
  "schema": 11,
  "total": 580,
  "supported": 486,
  "rollbackAvailable": false,
  "updatedAt": "2026-10-17T06:00:00Z",
  "sites": [{
    "id": "linuxtracker",
    "name": "LinuxTracker",
    "type": "public",
    "supported": true,
    "reasons": [],
    "settings": []
  }]
}
```

`updatedAt` is when the current generation was fetched on this NAS (after a
rollback, when that older generation was fetched); the bundled baseline has none.
Catalog entries also include `file`, the upstream filename; use `id` for selection.
The backend maintains an explicit ID-to-file index (six baseline filenames differ).

The baseline is Prowlarr/Indexers `definitions/v11` at the commit above.
486/580 (83.8%) pass this host's **static execution gate**, not a live health
check. Reasons can overlap: 58 `filter:andmatch`, 16
`custom_tls_certificates`, 25 `credentials_in_url`. The fixed engine does not
implement `andmatch`; custom TLS trust and URL-carried credentials are refused.
Unknown schema/engine syntax and template functions are also refused. Never
present `supported` as proof that a tracker is online or login works.

A setting is `{name,label,type,options?}`. Types follow the definition, including
`text`, `password`, `checkbox`, `select`. No stored values or secret defaults
are returned. Omitted values use engine defaults. Render passwords and cookie/API
key values as secret fields; omit existing values when retaining them.

## Source management

`SourceInput`:

```json
{
  "id": "<existing id; omit to create>",
  "kind": "builtin",
  "name": "My tracker",
  "enabled": true,
  "definitionId": "linuxtracker",
  "values": {"username":"example", "password":"<secret>", "cookie":"<secret>"},
  "clearValues": false
}
```

`kind` is one of:

| Kind | Additional input | Behavior |
| --- | --- | --- |
| `builtin` | `definitionId` | Enable a bundled/updated Cardigann definition |
| `cardigann` | `yaml` | Import pasted/uploaded YAML; same schema and feature gate |
| `torznab` | `url`, `values.apikey` optional | User's Torznab endpoint; 100 results/page |
| `rss` | `url`, `values.cookie` optional | gofeed RSS/Atom; query filters item titles |
| `ytsearch` | None | yt-dlp YouTube search; managed yt-dlp must be installed |
| `bilisearch` | None | yt-dlp Bilibili search; managed yt-dlp must be installed |

On edit, send `id`, `kind`, `name`, `enabled` and the source configuration.
Imported YAML can be omitted to retain the previous YAML. `values` merge by key;
omit a key to retain it, send an empty value to clear that key, or set
`clearValues:true` to replace the whole credentials map. Use
`search.sources.enabled` for a toggle without resending hidden configuration.
`SourceView` omits YAML/values and includes a display-only redacted `url` for
Torznab/RSS. Every query value is `***`, userinfo is removed, recognized path
credentials and stored credential values are masked. The host and ordinary path
remain visible. Never use this display URL to fetch a feed.

On an edit of the same source kind, omit `url` (recommended) or send its unchanged
redacted view to retain the exact private endpoint and keys. A changed `url` is a
complete replacement; enter its real credential values or set `values.apikey` as
appropriate. No partial merging of masked URL fields occurs.

Example:

```json
{
  "id":"<32 hex>", "kind":"builtin", "name":"My tracker", "enabled":true,
  "definitionId":"linuxtracker", "configured":false,
  "supported":true, "reasons":[], "settings":[]
}
```

`configured` means some backend values exist, not successful authentication.
Unsupported imported definitions remain visible with reasons and cannot execute.
Invalid YAML or an unidentified definition is rejected. Export returns only the
user's original imported YAML, never the separate credentials map. Put secrets
in `values`, not literal YAML. Edits, toggles and deletion invalidate prior search
results and queued tickets for that source. They do not remove completed files.

Limits: 64 sources/user; 256 KiB/YAML; 64 values with at most 8 KiB/value;
120 bytes/name. Reaching a storage bound fails explicitly, without replacing the
previous file. Files are package-private (directories 0700, records 0600).
No DSM session/password/cookie is saved for background authority.

Torznab permits private LAN addresses, with no LAN approval flow; loopback,
link-local, multicast and unspecified destinations are refused at dial time.
Cardigann and RSS additionally refuse private destinations. DNS results are
validated and the checked IP is dialed directly. Redirects are bounded; secret
headers do not cross origins. Endpoint userinfo is refused. Torznab uses the standard `apikey` query parameter for Jackett/Prowlarr.
The user enters it in `values.apikey`; it remains in backend source settings.
A pasted endpoint query key is moved into those settings and the stored endpoint
is redacted. The private helper adds the real key only when constructing the HTTP
request. Result URLs, tickets and journals retain `apikey=***` (including
`jackett_apikey`/`api_key` variants), never the real value. Errors and helper stderr
are not forwarded. Cross-origin redirects strip query keys and credential headers.

For a `.torrent` result the worker sends the redacted link plus the private source
settings to the helper. The helper reconstructs authentication, fetches bounded
bytes, and hands metainfo bytes to the existing Add flow. An authenticated URL is
never handed to Gopeed or the frontend. Public RSS links can continue through the
existing HTTP/media intake. Fixtures cover query authentication, redaction,
redirect stripping and bytes-only torrent handoff.

## Browser-assisted source pages

Search and source probes retain SDK admission, owner/UID checks, original IDs, status, result, pages and cancellation. Browser-assisted results use `search.browser.start {id,sourceId}` and `search.browser.deliver {id,token,url,html}`. Download-detail assistance uses `search.browser.download {resultId}` and one delivery containing `magnet`. `search.browser.cancel {id}` invalidates pending requests. These routes are authenticated and enforce the same owner even for administrators.

`search.jobs.wait {id,timeout,browser}` is an observational ctl read (1–20 seconds). It merges accepted browser pages; challenged server pages carry `needs_browser` and a site URL. The browser page owns the RTO session. See [the browser contract](cloudflare-search.md).

## Categories

`category` is one of `movie`, `music`, `software`, `series`, `anime`, `book` or
`other`; empty means every category and any other value is `invalid_request`.
It is sent as the Newznab ID (2000, 3000, 4000, 5000, 5070, 7000, 8000) to
Torznab (`cat`) and Cardigann sources, which map it to the site's own
categories. Browser-assisted and RSS sources ignore it; the page hides results
of another known type and keeps results without one.
