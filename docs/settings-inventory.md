# Upstream UI/API inventory

Gopeed 0.2.2-0001 admits administrators and signed-in DSM users allowed by DSM Application Privileges. Newly registered apps initially allow administrators only, including on upgrade; the administrator must authorize ordinary users in Control Panel → Privileges → Application Privileges. The SDK CGI entry gate covers all business/SDK APIs and transfers, with at most 30 seconds of decision caching. Global engine and extension settings remain administrator-only (`api.RequireAdmin` plus route floors). Settings contains an administrator-only explanation and SDK navigation to Control Panel. Direct privilege-page navigation is pending a verified SDK target; no app-owned permission mechanism is added.

The existing pre-0.2.0 administrator-only change was reviewed (`6a9ecc2`, `7f3b093`, `b2f7cf7`, `5e2c021`). The original `UID:username` owner keys and exact task/list filters are reused. Existing histories are retained under the original UID; normal users never inherit an administrator’s records.

Inspected the pinned official source (`pkg/rest/server.go`, `internal/fetcher`, `pkg/download`, extension manifests and Flutter settings screens) and the [official extension documentation](https://gopeed.com/docs/dev-extension), [sample extensions](https://github.com/GopeedLab/gopeed-extension-samples) and [stable releases](https://github.com/GopeedLab/gopeed/releases/latest). As checked on 2026-10-07, the stable API reports v1.9.3. Upstream 2.x previews are not admitted automatically.

| Control | Current native behavior | Scope / reason |
|---|---|---|
| Destination / folder picker | Implemented using SDK File Station | Per DSM user; SDK NasPicker within the administrator-confirmed install-selected scope; selection is the user’s grant |
| Download address / filename | Implemented; one input routes BT (magnet, `.torrent` link, uploaded `.torrent`) and media pages (yt-dlp), and leaves every other address to the engine: HTTP(S), ED2K (`ed2k://`, eMule magnet) and any scheme an enabled extension handles | Owner-bound task and SDK grant; private staging then SDK moveFromStaging; refuses `file:`, non-torrent `data:`, local/private hosts and credentials ([BT and multi-file](bt-and-multi-file.md)) |
| Extension resolve results | Implemented; a multi-file result opens the BT file selection | Each chosen file becomes its own single-file job and SDK save; each file request is checked like a pasted address |
| BT file selection | Implemented; all files selected by default with total size | Per task; stored durably with the durable intent and patched after create |
| ED2K TCP / UDP ports | Implemented; 1024–65535, default 4662 / 4672, not the BT port | Admin/global; `protocolConfig.ed2k.listenPort`/`udpPort` (upstream default 0 = random); open TCP and UDP in the DSM firewall; applies after package restart |
| Active/completed/history, progress and speed | Implemented | Lists filtered by authenticated user/UID |
| Pause/resume/remove history | Implemented, including interrupted startup recovery | History removal leaves files; no hidden filesystem deletion |
| Open downloaded file location | Implemented | Current permission and file existence checks; portal opens full DSM then File Station |
| Maximum running jobs | Implemented; 1–16 | Admin/global; GET/PUT `/api/v1/config`; retains other fields |
| HTTP connections per new job | Implemented; 1–32 | Admin/global; `protocolConfig.http.connections`; stored for supervisor restart |
| BT seed ratio / seed minutes | Implemented; 0–100 / 0–10080, default 0/0 = stop at completion | Admin/global; `protocolConfig.bt.seedRatio`/`seedTime`; files move only after seeding stops |
| BT listening port | Implemented; 1024–65535, default 16889 | Admin/global; `protocolConfig.bt.listenPort`; open TCP and UDP in the DSM firewall; applies after all BT tasks stop or package restart |
| Install/enable/disable/update/remove extensions | Implemented with official registry discovery and version checks | Admin/global; HTTPS GitHub source, source/permissions consent, installs disabled; idle/stopped-store snapshot, verified upgrade and failure rollback preserve settings/enabled state |
| Extension settings | Implemented from declared manifest | Admin/global; string, number, boolean and choices; credentials are shared, not per-user |
| Check/update/rollback official binary | Implemented through package supervisor | Admin; checks digest, ELF arch and version; bounded stable 1.9.x adapter |
| Media page URL (YouTube, X, other yt-dlp sites) | Implemented; probe, quality/audio choice, pause/resume/retry, reasons | Per user, owner-bound; see media-downloads.md. Cookies/sign-in not supported |
| Check/update/rollback yt-dlp, ffmpeg, deno | Implemented on Core updates | Admin; official GitHub stable releases, digest + ELF + version; yt-dlp self-update admitted only at the official digest |
| UI language / theme | DSM locale and SDK theme | Existing application strings retain their fallback; all new multi-user, destination and cancellation-reason strings cover all nine existing locales |
| Global speed limit | Not offered | No supported public global limiter found in pinned API; do not create a nonfunctional control |
| Torrent (magnet, `.torrent`) | Implemented | Owner-only tasks; one submitted task per info hash; each file published by its own SDK operation, flattened names under CLI 0.8.0 |
| Extra BT trackers | Implemented; up to 64 public http/https/udp announce URLs | Admin/global; `protocolConfig.bt.trackers`; local, private, link-local and credentialed hosts refused; `settings.set` without `trackers` keeps the saved list |
| ED2K servers, `server.met`, `nodes.dat` / batches / DHT tuning | Not exposed | No separate authorization contract; upstream defaults apply |
| Extraction / arbitrary filesystem browser | Refused | Package-account filesystem writes cannot substitute for caller's DSM ACL |
| Script execution / webhook / unrestricted proxy | Not exposed | Shared global execution and network behavior need separate authorization contracts |
| Upstream accounts/token/API port/storage settings | Not exposed | Owned by the DSM integration; private engine token and `127.0.0.1:19893` API stay server-side |

Settings polling updates task/runtime information without replacing an in-progress numeric form edit. Applying preferences validates bounds on the server, preserves upstream configuration, persists the approved fields and verifies live readback. Installed plugins are engine-wide, not per-user isolation; the native UI says so before installation.

The official Github Release sample demonstrates installation, disable/enable, declared User-Agent setting and update. That verifies management, not successful downloads through every third-party extension. An extension result with several files is offered for selection and each chosen file is downloaded and saved as its own job; BT tasks publish multiple files from one task.

0.2.1-0001 uses Gopeed's official `/api/extensions` list API, confirmed in the pinned official Flutter `gopeed_site_api.dart` and live registry JSON. Installed update checks use the official `/api/v1/extensions/{identity}/update` endpoint. Stable numeric newer versions are admitted; unknown/prerelease formats and changed settings schemas require review. Current-session autosave runs while open and catches up on reopen; offline delegation remains blocked by CLI 0.6.0 (see sdk-followups.md).

For 0.2.2, `gopeed.request GET /api/v1/config` returns only the caller’s stored destination and grant-presence display metadata; it never exposes the global engine config. New download resolve/create always revalidate the SDK destination. Frontend cached paths or booleans are not authority. The global settings APIs, extension APIs, binary APIs, executor control and delegate enable/disable remain administrator-only. Existing history is not migrated into new ownership or fabricated grants.

## Runtime and publication boundaries

The package account downloads into private staging. Each destination grant is
bound to its DSM user; the SDK executor publishes with that user's permissions.
Only a protected committed receipt marks a file Saved. Query uncertain operations
using their original reference. Removing a task keeps published files. See
[media destinations](media-downloads.md#media-choices-and-rules) and
[multi-file publication](bt-and-multi-file.md#multi-file-publication-backendinternalmultisave).

Engine replacement requires every user's engine tasks to be finished or errored.
The supervisor verifies the replacement and restores the prior executable if
startup fails. Compatibility follows the adapter and locked upstream metadata.
Extensions run in Gopeed's shared runtime with engine-wide settings; an
administrator must explicitly trust and enable them. Returned file requests are
checked, but Gopeed and its extensions are not a hostile-tenant network sandbox.

The mobile shell accepts links and torrent files into a draft that the user must
confirm. Repeated shares join the open draft without replacing edits. Saving a
finished file to a phone uses File Station in the current DSM session; multi-file
downloads expose each committed file separately. This action requires the native
shell. Desktop and browser Portal launches use their own controls.

Hyper Backup application backup includes package data. Select destination shares
separately to back up published files. Restore requires the App to be installed;
engine rollback is not a data snapshot.

### Durable download submission

The shipped download form resolves once and keeps the returned RID for an unchanged
URL/filename/destination draft until create succeeds. API clients that retry a
submission must use this same RID; a new resolve is a new submission.

Before the non-idempotent engine create POST, Gopeed fsyncs the resolved RID,
original UID/username owner, grant, filename and unique private staging in jobs.json.
A marked RID is never posted again. An accepted reply is committed to Jobs; a lost
reply, timeout, accepted engine error or failed App commit leaves the durable intent.
Read-only reconciliation matches the exact staging and filename, refuses ambiguous
or foreign matches, and preserves an existing committed job. It runs on owner list
reads, every daemon save pass, and before native engine startup resumes owned tasks.
The user receives the same task ID when retrying an accepted RID. Deleting its
history does not permit the old RID to create a new task.

A crash before transport may leave an intent for which no engine task exists. It
remains unresolved and consumes the existing 128-entry queue bound; Gopeed never
interprets a missing task or failed query as permission to POST again. The user may
start a new explicit submission. This is a conservative bound on delivery, not a
claim that an engine request can be made atomic with the App journal. No session
credentials or source URL are retained in the intent. Existing histories, SDK move
refs and saved receipts remain unchanged; Recovery retries and legacy migration use the SDK recovery contract; lifecycle hooks are generated by the pinned CLI.

### Multi-file durable intent

BT jobs extend the same journal entry with `kind`, `hash`, `fileCount`,
`select` and, once the task is complete and seeding has stopped, `files`. The
`files` list holds one durable publication item per file: source, intended
relative path, destination name, size, attempt, state and code. The selection
is fsynced before the create POST. Each file's SDK tuple is fsynced before its
first move. Reconciliation matches a BT task by its private hash stage and
info hash rather than by filename. Old journals without these fields are read
as single-file jobs. The journal limit is 8 MiB, with a shared bound of 4096
file records across jobs.
