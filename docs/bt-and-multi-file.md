# BT downloads and multi-file publication

## One input, automatic routing

The New download dialog has a multi-line link field and the save location.
Pasted text is reduced to its links; `.torrent` files can be dropped on the
field or added with a small link. `downloads.create_many` takes up to 32 links
(`{path, items: [{key, url}]}`) and answers one result per link: `created`
with its task IDs and kind (`file`, `torrent`, `ed2k`, `media`, `extension`),
`pending` (`metadata_pending`, or `deferred` when the batch ran out of its
6-second start budget inside the 20-second CGI limit) or `failed` with a
code. One link's failure never affects the others. A key the caller already
used answers with the tasks it created, so a batch repeated after a lost reply
never downloads a link twice; the dialog sends pending links again with the
same keys. Torrents and extension results download every file, media pages the
best quality. `downloads.classify` names each link's kind for the dialog's
summary without contacting any source (media by known site, extension links by
enabled extensions' `onResolve` URL patterns).

The backend, not the browser, chooses the downloader (`ClassifySource`). There
is no scheme whitelist: BT and media are recognized first, and every other
address goes to the Gopeed engine, which tries its own protocols (HTTP, BT,
ED2K) and then every enabled extension.

| Source | Downloader |
|---|---|
| `magnet:` with `xt=urn:btih:` (40 hex or 32 base32) | BT |
| Uploaded `.torrent` file (at most 512 KiB, bencoded metadata checked) | BT |
| HTTP(S) path ending in `.torrent` | BT (the file is fetched, at most 512 KiB) |
| HTTP(S) answering `HEAD` with `Content-Type: application/x-bittorrent` | BT |
| Address yt-dlp recognizes as a media page | Media downloader (yt-dlp), best quality; a known media site without the tool fails with `media_tool_missing` |
| `ed2k://|file|…|/` link | Engine ED2K (single file) |
| `magnet:` with `xt=urn:ed2k:<32 hex>`, `xl` and `dn` | Engine ED2K; converted to the equivalent `ed2k://` link, otherwise `ed2k_link_incomplete` |
| Any other address with a scheme (HTTP(S) or one an installed extension handles) | Engine and its enabled extensions |

If no protocol or extension can resolve the address, the engine's error becomes
`source_unsupported` ("Gopeed cannot download this address. Check it, or ask
an administrator to install and enable an extension that supports it."). Codes
are never shown; all nine locales have the message.

`Service.Media` (`MediaDetector`) is only an interface hook for the media
downloader task. `btmh` (v2-only) magnets and magnets with neither a BT nor an
ED2K hash are refused.

### Safety floor (blacklist)

These are refused before the engine is called, whatever extensions are
installed. The engine runs as the package account, never root.

| Refused | Reason |
|---|---|
| `file:` | The engine would read a local file with the package account's rights, bypassing the caller's DSM ACL and exposing package-private data (engine token, journals, other users' staging). |
| `data:` other than `data:application/x-bittorrent;base64,` | Not a download source; the engine's BT filter decodes inline data. Only the torrent form is admitted, through the BT path that validates it. |
| Addresses without a scheme, and non-HTTP(S) addresses ending in `.torrent` | The engine's BT file filter opens a `.torrent`-suffixed path from local disk. |
| Loopback, private, link-local, unspecified or `localhost` hosts, for every scheme | Same floor as the HTTP downloader: users must not reach the NAS itself, the private engine API (`127.0.0.1:19893`) or the LAN through the shared engine. |
| Credentials in the address (`user:pass@`) | Not stored or sent on the user's behalf. |
| Extension results whose file request is BT, magnet, media, local or private | A file request is checked like a pasted address; BT keeps one stage per info hash only through the BT path. |

App-owned HTTP probes and torrent fetches resolve every connection, reject the
whole DNS answer when any address is non-public, and dial a checked numeric
address without another lookup. Redirects use the same policy, with no proxy.
Literal carrier NAT, reserved, multicast and IPv6 translation/tunnel addresses
are refused along with private, loopback and link-local addresses.

Torrent announce/announce-list and web-seed url-list/httpseeds fields, and
magnet tr/ws/xs/as/x.pe endpoints, receive the same address admission checks.
These checks cannot control the later DNS lookups, redirects, metadata-provided
web seeds or peer sockets opened by Gopeed itself. The engine and yt-dlp still
own their outbound connections; their traffic is not covered by the App HTTP
transport. Extensions run in Gopeed's own runtime and are not a network sandbox.

## BT resolve, selection and ownership

Gopeed checks the info hash before calling the engine and compares it with the
hash the engine resolves. Magnet metadata can take time, so the engine resolve
runs outside the engine and jobs locks with a 12-second budget, inside the
SDK's 20-second CGI request limit. If it runs out, the link is `pending` with
`metadata_pending`. Upstream keeps waiting for the metadata; the preparation
worker retries the resolve until metadata arrives or the owner cancels; a slow
swarm is not an error. The task shows "Finding peers" (`waiting`) after the
first attempt runs out. New requests go before waiting ones, which retry
oldest attempt first, and the worker rotates between runnable owners. This bounds the App worker, while
upstream metadata waits may remain alive after its HTTP request ends. Uploaded or fetched
torrents are stored privately at `<data>/torrents/<hash>.torrent`.

Every file of a torrent is downloaded unless its owner chooses files from the
task's Files tab (`downloads.select_files {id, files}`, owner only). The
choice is validated against the torrent's file count, stored as `Job.select`
(absent when every file is chosen) and sent to the engine as a PATCH of
`opts.selectFiles`; the save pass sends it again when the engine's choice
differs. Only chosen files are planned and saved. Once the files are planned
(`Job.files`), published or deleting, the choice is closed
(`selection_closed`). The New download opt-in "Show dialog to select files for
download" (off by default, kept per DSM user in the browser) only opens that
tab for each new multi-file torrent; downloading starts at once, and pieces of
unchosen files fetched before the choice arrives stay in the stage unsaved.
A lost create reply is reconciled by private stage plus info hash.

The engine keeps one storage location per info hash. Each hash therefore has
one stage, `executor-staging/bt-<hash>`, and at most one submitted task across
all users (`torrent_in_use`). Previews do not reserve the hash. A user's newer
preview of the same torrent replaces their older preview. BT tasks belong only
to their owner, like HTTP tasks. Administrators cannot list, pause or delete
another user's BT task.

Stage folders are removed once nothing uses them: when a resolve is refused
or still waiting for magnet metadata, when a user's newer HTTP/ED2K resolve
replaces the older unsubmitted one, and when a task is deleted. Only empty
folders are removed, so the private bytes of an unsaved task stay for
diagnostics. At daemon start, empty folders older than one hour that no task
or preview refers to are removed as well.

## Seeding and moving

| Setting | Range | Default | Engine value |
|---|---|---|---|
| Seed ratio | 0–100 | 0 | `protocolConfig.bt.seedRatio` |
| Seed minutes | 0–10080 | 0 | `seedTime` = minutes × 60 s |
| BT listening port | 1024–65535 | 16889 | `listenPort` |

The settings are administrator-only and apply to every user's BT tasks.

- **Seeding stops at completion by default.** Upstream seeds forever when ratio
  and time are both 0, so "0 and 0" is sent as `seedTime: 1` (one second).
- **Seeding comes before moving.** Files move only when the engine reports
  `done` and is no longer `uploading`. Moving files would remove the data the
  engine is still serving, so a longer seeding limit delays saving.
- **Alignment on upgrade.** The engine applies `downloadConfig` only on its
  first start, so upgraded installs still carry upstream's seeding defaults.
  Settings read and every BT resolve align the live engine config first.

**Firewall:** allow the BT port for **TCP and UDP** in the DSM firewall and any
router port forwarding. The port carries peer and DHT traffic. The engine binds
it on all interfaces. A changed port takes effect when every BT task has
stopped or the package restarts. The private engine API port (`127.0.0.1:19893`)
must not be opened.

## ED2K

The engine's ED2K client starts once with the package and binds on all
interfaces. Its server list, `server.met` and `nodes.dat` keep upstream's
defaults; DHT and UPnP are on upstream.

| Setting | Range | Default | Engine value |
|---|---|---|---|
| ED2K TCP port | 1024–65535 | 4662 | `protocolConfig.ed2k.listenPort` |
| ED2K UDP port | 1024–65535 | 4672 | `protocolConfig.ed2k.udpPort` |

Upstream's default is 0 (a random port each start), which cannot be opened in
a firewall, so the App pins the ports above. Neither may equal the BT port.
Saving writes only the two ports and keeps the rest of the ED2K config. Every
ED2K resolve aligns the live config first. **A changed port takes effect after
the package restarts.**

**Firewall:** allow the TCP port for TCP and the UDP port for UDP in the DSM
firewall and any router port forwarding. Without it, ED2K sources connect
slowly or not at all (low ID).

An ED2K link names one file, so it uses the single-file flow: private staging,
then one SDK `files.moveFromStaging`.

## Extension results

An extension's resolve result has no engine resolve ID; upstream creates one
task per file from each file's own request. The App does the same:

1. Resolve stores the result as the owner's private preview (`x…` RID). Each
   file's request is checked like a pasted address (at most 16 KiB, engine
   route, safety floor, no `skipVerifyCert`, no proxy). The browser receives
   names and sizes only.
2. A result with two or more files opens the same file selection as BT (all
   selected, total size). Names are planned by `multisave.Plan`, so folders are
   flattened the same way; at most 256 files.
3. Create turns each selected file into its own single-file job (`<rid>.<n>`)
   with its own private stage and durable intent, fsynced before its POST. Each job
   is saved, retried and deleted like any HTTP job. Retrying the same RID
   returns the same task IDs.

A one-file result keeps the optional rename. An engine result that has a
resolve ID and more than one file, other than BT, is still refused as
`single_file_required`; in 1.9.3 HTTP and ED2K always resolve to one file.

## Multi-file publication (`backend/internal/multisave`)

The module does not depend on any downloader. A task describes its completed
files as stage-relative sources and stores the returned items in its job record.

- **One operation per file.** Each file is a separate SDK
  `files.moveFromStaging` operation, so every write uses the owner's DSM
  permissions. Existing files are never overwritten.
- **Completion.** A task is saved only when every file is committed with a
  matching receipt.
- **Safe paths.** Sources must be clean relative paths: no `..`, no absolute
  paths, no NUL or backslash, no duplicates. Each file is at most 16 GiB.
- **Limits.** A torrent may have at most 256 files. Across all jobs, at most
  4096 file records are stored, keeping `jobs.json` under its 8 MiB journal
  limit.
- **Before saving,** each selected file must be a regular file whose size
  matches the metadata exactly. Otherwise the task fails as
  `download_incomplete`.

### Folder layout (flattened)

The published SDK executor (CLI 0.8.0) cannot create subfolders, so a nested
layout is flattened into one name per file in the destination:

- `Album/cd1/01.flac` becomes `Album - cd1 - 01.flac`.
- A single top-level file keeps its own name.

Names are cleaned for DSM and SMB. They are capped at 255 bytes, keep an
extension of up to 16 bytes, and are made unique ignoring letter case with a
`~hash` suffix. `Item.Rel` keeps the intended relative path. Only
`destinationName` (`multisave/plan.go`) turns `Rel` into the written name.
When the SDK adds subfolders to `files.moveFromStaging`, only that function
and the move parameters need to change.

### Recovery, retry and deletion

- **Before any IPC.** Every file's move tuple is persisted first, with
  operation ID `gopeed-<task>.f<index>.a<attempt>`. Restarted daemon passes
  query or admit only unfinished files.
- **Transient errors** leave a file pending.
- **Terminal errors** (for example `EXISTS`) mark the task `save_failed`.
  Saved files are kept. The owner's retry starts a new attempt only for the
  failed files; saved files are never moved again.
- **Deleting a task** cancels admitted unfinished files and waits for
  uncertain ones. It then removes the engine task, the private hash stage and
  the stored torrent. Saved files remain and the removal notice reports them.
- **Older journals** have no `kind`, `hash`, `select` or `files` fields. They
  read unchanged as single-file jobs.
