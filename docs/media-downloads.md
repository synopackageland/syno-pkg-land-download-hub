# Media downloads

The probe uses the installed SDK’s `outbound.StartProbeProxy` public-network boundary.

HTTP(S) links get one yt-dlp probe with all default extractors, including Generic.
The same routing is used by task preparation and `downloads.classify`; the preview
never admits a download. Protocol/torrent inputs bypass yt-dlp entirely.

| Input / probe result | Route |
| --- | --- |
| Magnet, uploaded/URL torrent, ED2K, other non-HTTP protocols | Gopeed, no probe |
| Site extractor match | yt-dlp |
| Generic HLS/DASH manifest, including bare `.m3u8` / `.mpd` | yt-dlp |
| Generic embedded video/audio or page with media formats | yt-dlp |
| Generic plain direct file, including ZIP/ISO/EXE/PDF and MP4/MP3 | Gopeed |
| yt-dlp does not handle an image gallery and gallery-dl is installed on x86_64 | gallery-dl |
| Unsupported/no media, tool crash, malformed/oversized probe output or work limit | Gopeed HTTP |
| DRM restriction | Refuse with `media_drm`; never download the manifest as a file |
| Refused address, redirect or DNS answer | Refuse with `url_denied` |

For Generic results, HLS/DASH/fragmented stream protocols take precedence over
`direct: true`. Otherwise `direct: true`, or a single plain HTTP(S) format whose
URL equals the input, selects Gopeed. An embedded media URL differs from its HTML
page and stays with yt-dlp. Plain MP4/MP3 files use Gopeed because extraction adds
nothing and Gopeed retains its multi-connection and resume features. Empty Generic
results have no media and also fall back. Recognized sign-in, geo, unavailable and
size restrictions keep their existing visible reasons.

`downloads.classify` retains `results[].url` and `kind` and adds `route`
(`yt-dlp`, `gallery-dl` or `gopeed`; empty for blocked/unsupported inputs). The media probe
response also adds `route`, retaining `kind`, `rid`, `info`, `merge` and
`toolMissing`. A classification refusal includes `code`; DRM has kind `media`,
route `yt-dlp`, and code `media_drm`. Consumers can group media as
“Video / stream (yt-dlp)” and direct files as “Direct (Gopeed)”. A missing tool
keeps the existing known-site hint; no successful yt-dlp extraction is implied.
Classification contacts public
HTTP(S) sources when a media tool is installed; callers should allow cancellation.
Task admission remains immediate and the daemon owns preparation.

Probe failures never block ordinary HTTP downloads. Search results deliberately
keep Generic excluded; flat search resolution is a separate contract.

## Tools

yt-dlp, gallery-dl, ffmpeg and deno are not bundled in the SPK. An administrator installs them on
the Core updates page; the package daemon downloads, verifies and activates them as the
package account under `<var>/gopeed/media/tools/<tool>/<version>-<sha16>/` (mode 0500,
never rewritten). `tools/state.json` keeps the active and previous version.

| Tool | Source (stable only) | Verification before activation |
|---|---|---|
| gallery-dl (x86_64 only) | github.com/mikf/gallery-dl most recent stable release with `gallery-dl.bin` | GitHub asset SHA-256 = downloaded SHA-256; ELF x86_64; `--version` |
| yt-dlp install | github.com/yt-dlp/yt-dlp latest release, `yt-dlp_linux` / `yt-dlp_linux_aarch64` | GitHub asset digest = `SHA2-256SUMS` line = downloaded SHA-256; ELF machine; `--version` |
| yt-dlp update | the same verified official download as install | SHA-256; ELF machine; `--version`. Current executable retained on failure |
| ffmpeg | github.com/yt-dlp/FFmpeg-Builds `autobuild-*` tag (immutable), `linux64-gpl` / `linuxarm64-gpl` | digest = `checksums.sha256`; only `ffmpeg` and `ffprobe` regular members unpacked; ELF; `-version` |
| deno | github.com/denoland/deno latest release, `deno-<arch>-unknown-linux-gnu.zip` | GitHub asset digest; ELF; `--version` |

Checks use official stable GitHub releases and compare the available executable with the active version. gallery-dl searches the latest release entries for the newest standalone Linux asset, skipping source-only releases; absent or invalid asset digests refuse installation. It never uses a mirror or installs a separate Python runtime. On aarch64 the tool card says “Not available on this model”; galleries keep the normal routing path. Updates use the package downloader and its
optional administrator GitHub token.
The official checksum, ELF architecture and reported version must match before
activation. Rollback re-hashes the
previous version before switching. Any failure keeps the current version and shows the
reason on the page.

**Running downloads during an update:** a running yt-dlp keeps the executable it
started with; versions are separate directories, and only the active and previous
version are kept. New and resumed downloads use the new active version. A download that
started two updates earlier loses its directory and may then fail with a retryable
reason.

## Downloads

- Owner-bound job (`media-<rid>`) in `jobs.json`; the source URL is kept for the
  daemon and shown only to its owner. Default format is best video+audio merged to
  MP4 (MKV fallback) when ffmpeg is installed; choices are offered heights and
  audio-only MP3, M4A, Opus or FLAC with FFmpeg. Without ffmpeg only single-file formats are used. The existing `merge: false`
  response and Core updates installation hint remain; ffmpeg is not bundled.
- The daemon runs at most 2 downloads, `nice 10`, 12 h per attempt, 16 GB per file,
  playlists up to 256 items. Pause and delete send SIGTERM to the process group, then
  SIGKILL after 10 s. Available slots go first to owners with fewer active media
  runs; ties rotate between owners. A single owner may use both slots when no
  other owner is waiting. Running downloads are not preempted for a new owner.
- Files go to the job's private staging; temporary files, the download archive and
  progress go to `media/jobs/<id>/`. After a daemon restart, running jobs are
  requeued; `--continue` resumes partial files and `--download-archive` skips
  completed playlist items.
- Results publish through `multisave` and the SDK `files.moveFromStaging`. One file
  keeps its name; several are flattened to `<title> - <file>`.

## Isolation

Every run uses `--ignore-config --no-config-locations --no-plugin-dirs --no-update
--no-exec --no-batch-file --no-cookies-from-browser
--no-remote-components`, a fixed cache under package data, `--windows-filenames`
with byte-bounded templates, and a minimal environment (`HOME`, `XDG_*`, `TMPDIR`
under package data, `PATH=/usr/bin:/bin`). Only http/https public addresses are
accepted. Arguments are built by the backend; the UI sends only the RID and a format
name.

### Probe network policy and work limits

The SDK owns a per-invocation authenticated loopback HTTP/CONNECT proxy with
no environment proxy. The proxy checks the SDK public-address policy before
each HTTP request or HTTPS tunnel, checks **every** DNS answer (including mixed
public/private answers), and dials only those checked IPs, without another DNS
lookup. Redirects and embedded resources must pass the same policy. Private,
loopback, link-local, reserved ranges, localhost and URL credentials are refused.
HTTPS stays encrypted and yt-dlp still verifies the upstream certificate.

Each probe allows at most 64 accepted clients (including idle clients),
64 proxy requests/tunnels, 64 outbound connections,
32 MiB total upstream wire traffic in both directions, 16 MiB JSON output and
16 KiB diagnostic tail. Redirect chains consume this same finite work budget;
there is no 45-second total probe deadline. Existing 30-second socket inactivity
handling remains distinct from elapsed probe duration. Extractor/network retries
are disabled for this one probe. Caller/pause cancellation or exhausted work
closes tunnels and kills the process group; work exhaustion falls back to Gopeed,
while policy refusal never does. Proxy credentials are ephemeral, never stored or
returned, and are stripped before forwarding HTTP. The SDK refuses a root proxy process and Apps cannot supply proxy listeners,
HTTP handlers, resolvers or dialers. The proxy governs yt-dlp's configured networking; it is not an OS sandbox or a new security guarantee for
upstream Gopeed/extension sockets or subsequent media downloads.

## Media choices and rules

Settings → Media stores each DSM user's defaults. On a phone, open the
Downloads options sheet to override media choices for the next batch. The same
choices carry into the full add dialog when choosing several links, and a
successful quick-add restores the default/rule mode. On desktop, the add dialog's
advanced media options apply to that batch only. The UI sends enumerated choices, never
command-line flags. Choices include subtitle language codes, embedded subtitles
or SRT/VTT sidecars, audio conversion, embedded thumbnails and metadata, chapter
files, SponsorBlock marking/removal, and Kodi/Jellyfin NFO sidecars. FFmpeg is
required for conversion, embedding, chapters and SponsorBlock. Missing FFmpeg
produces an explicit refusal rather than silently ignoring a choice.

Rules match an exact host or a channel URL, with channel rules taking priority.
An explicit per-download override wins over a rule's quality/format. The backend
snapshots choices at admission. A channel discovered during the media probe uses
the admission's rule snapshot. The SDK destination is the root; each rule or follow stores a validated relative subfolder, such as `Videos/YouTube/Channel`, or an empty path to use the root. The editor shows `<root>/` beside a text field and offers subfolders already used by this user. It performs no grant-scoped browsing or destination selection. Absolute paths, `..`, empty segments, control/DSM-invalid characters, reserved names and oversized UTF-8 components are refused.

New home users confirm their first download folder, default `Downloads`, in a small dialog (a bottom sheet on phones). The backend selects `/home` with the caller's own DSM session only after that confirmation and only when the protected scope permits home. Existing destinations remain roots with empty subfolders. The main subfolder is a per-user setting; changing it never replaces a grant. **Use another folder** offers the SDK picker within the administrator-approved scope. Home-service checks use the caller's own SDK DSM channel and File Station share visibility, are cached briefly per Context, and have no polling timer. A disabled service gets a dismissible administrator/non-administrator callout and Retry; the App never turns it on.

Publication sends the existing SDK `MoveParams.Name` as `<subfolder>/<file>` with the original grant and stable operation ID. No `dest_dir`, socket calls or App NAS mkdir are used. The pinned executor opens each path component beneath the granted directory, creates missing components after dropping to the user's identity, and refuses symlink and mount crossings. Folder creation is mode 0700 with the user's inherited ACL. A root change affects new jobs; rules/follows stay relative and admitted jobs retain their pinned tuple. Only a matching full-path committed receipt is Saved.

NFO files are escaped XML with title, plot, uploader/studio, runtime, upload date
and source identifier. They publish beside the video through the same receipt-
checked multi-file save. Published video, subtitle and NFO stems remain paired
when long filenames are shortened. Private extractor JSON is never published.

## Followed channels

Following requires an explicit per-channel opt-in. The default check interval is
six hours (minimum two hours). Each check lists at most the latest 100 entries,
including recent uploads on the first check. Results enter the same durable
preparation queue as pasted links. A channel's owner-bound admission archive and
its yt-dlp download archive prevent repeated downloads. The admission archive
holds up to 10,000 entries; at the limit the channel shows an error and retains
its archive rather than evicting entries and downloading them again. Failed
checks and a full queue wait until the next configured check; no short wall-clock
failure budget is added to channel checks.

The worker shares the daemon's existing tick and state snapshot. With no followed
channels it starts no extra process, goroutine, ticker or disk read. Removing a
follow cancels its active check and prevents late results from being admitted;
already queued downloads remain under the user's normal pause/delete controls.
Followed jobs use the owner's cookies only.

## Private website cookies

Each user can paste or import Netscape `cookies.txt` (up to 256 KiB). Import
replaces that user's previous jar. The store is keyed by nonzero local UID and
binds the account name to refuse reused identities. Package-private directories
are 0700 and files are 0600. The API returns only domains, cookie counts and
import time; it never returns names or values, and administrators have no API
selector for another user's jar. Import content is marked sensitive in the
internal operation schema and is never logged.

Every owner-bound probe or download receives a fresh 0600 temporary copy filtered
to its source host's cookie domains. YouTube short links also permit the canonical
YouTube domain. Browser cookie extraction stays disabled with
`--no-cookies-from-browser`; no browser profile, DSM session or user-supplied
cookie path is passed to the media tool. Copies are removed after the process
exits, including start failure, pause and package shutdown. A resumed job makes a
new copy. Crash leftovers in a job control directory are removed before its next
run. Users can delete one domain or all cookies.

`PurgeMediaUser` removes media preferences and cookies and marks owned jobs for
the normal cancellation path without changing SDK operation references. The
existing daemon preparation tick checks for confirmed account deletion at most
hourly when media users exist; the existing startup staging sweep also checks.
Transient account lookup failures are not treated as deletion. No DSM account or
NAS destination data is deleted by this cleanup.

## Not supported

DRM, paywall bypass, course-platform download flows and live/upcoming streams are
not supported. Cookie import provides a user's own authorized signed-in access;
it does not bypass content restrictions. Browser-extension cookie capture and
HLS sniffing are follow-up work.

Gallery-dl integration awaits an architecture decision: the official Linux
standalone release is x86_64 and no official aarch64 standalone is available.
The official project publishes releases on
[Codeberg](https://codeberg.org/mikf/gallery-dl/releases). No mirror, alternate
runtime or unverified binary is selected automatically.

## Immediate task admission and pause/resume

`downloads.create_many` performs source-format, caller permission and current
SDK destination checks once per batch, then atomically records a preparing job
for each accepted link. It never probes yt-dlp, waits for peers, resolves engine
metadata or creates staging directories in the CGI. Accepted results contain task
IDs; source failures appear on the task, where Continue retries preparation.
Repeated caller keys return the original admission or accepted engine tasks.

The existing package daemon runs one preparation at a time alongside its media
runner. Source probes use a journal snapshot and only merge their previews after
the slow work. No DSM cookie, token or fabricated authenticated caller is stored;
only the owner binding, source and pinned destination grant are retained. The SDK
continues to validate actual NAS writes. Engine create intents retain their
existing durable, non-reposting recovery rules.

| Task phase | Pause | Continue |
| --- | --- | --- |
| Preparing (any link) | Persist intent; cancel the daemon's current probe | Retry source preparation with the original caller key |
| Gopeed HTTP / BT / ED2K / extension file | Send upstream pause; retry a lost acknowledgement on the daemon tick | Send upstream continue; actual byte continuation depends on the upstream fetcher/source |
| yt-dlp download | Stop its process group on the daemon tick; keep private partials | Start yt-dlp with its existing `--continue` and archive; unsupported partial continuation restarts |
| Waiting to save | Hold automatic save admission | Allow the next daemon save pass |
| Single/multiple-file background save | Cancel original SDK references; retain staging and poll until terminal | After terminal cancellation, create a fresh copy attempt for unfinished files; keep committed files |

A copy already committed when pause reaches the executor remains saved. An
uncertain/canceling operation keeps its original reference and cannot start a
replacement copy. Per-task cross-process save locks order admission against
cancellation without holding the journal lock. Clearing a record retains files
already saved on the NAS.

Local fixtures cover admission, metadata waiting, ownership, retry, preparing
pause/clear, concurrent admission, single-file cancellation across restart and
multi-file continuation. They do not certify DSM permissions or NAS lifecycle.

NFO field names and paired filenames follow the
[Kodi movie NFO reference](https://kodi.wiki/view/NFO_files/Movies) and
[Jellyfin local NFO reference](https://jellyfin.org/docs/general/server/metadata/nfo/).
Actual library scanning remains part of device acceptance.

## gallery-dl execution

gallery-dl is an optional image-only fallback after yt-dlp reports no handled media. A job pins the chosen downloader; an unavailable gallery-dl at resume is an explicit missing-tool error. Both probes use the same SDK outbound proxy, cookie-copy isolation and output caps. Recognized yt-dlp access/DRM refusals are never bypassed through gallery-dl.

Arguments are fixed: `--config-ignore`, no input prompts, no postprocessors, no persistent authentication cache, no cookie updates, an exact private staging directory, ASCII byte-bounded filenames derived from each image URL (stable across retries and distinct across posts) and a fixed image-extension filter. Downloads request at most 256 images/posts and 16 GiB per file; the runner independently checks private file counts, file types and sizes while the process runs and again at completion. These are tool/process limits, not an OS sandbox. Cookies are the same temporary, owner- and source-host-filtered copies used by yt-dlp. Partial files are excluded from publication. Normal media pause/delete/timeout controls stop the entire process group, and results use the existing SDK receipt flow.
