# Changelog

## 0.2.2-0115 — Built from the public repository

- Built from the public source repository; features are unchanged
- The GPL corresponding source is now served from the public repository's releases
- SDK/App and the Go module unchanged at 0.53.49, UI at ~0.25.29, CLI at ~0.54.6, executor at 0.53.6 and templates at 0.42.1

## 0.2.2-0114 — Phone search and media tool fixes

- Phone layout: the search status line no longer covers the Filter results field when it wraps to a second line
- Core updates: until the media tool status has been read, each tool shows an unknown version instead of Not installed
- SDK/App and the Go module unchanged at 0.53.49, UI at ~0.25.29, CLI at ~0.54.6, executor at 0.53.6 and templates at 0.42.1

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0113 baseline, and the frontend unit suite. Full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0113, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0113 — Compact phone toolbar

- Phone layout: a page's toolbar takes fewer rows and sits at the top edge
- Update UI to ~0.25.29 (on a phone, a page's toolbar takes two thin rows at the top edge: Home beside the view switch, counts beside the actions, secondary actions with an icon drawn as icons; wider layouts and a DSM window are unchanged); SDK/App and the Go module unchanged at 0.53.49, CLI at ~0.54.6, executor at 0.53.6 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0112 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0112, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0112 — Phone layers match desktop

- Phone layout: the background, cards and nested panels now use the same colours as on a computer
- Update UI to ~0.25.28 (on a phone, the page background, cards and nested panels use the same layering as the desktop layout; a CSS-only change); SDK/App and the Go module unchanged at 0.53.49, CLI at ~0.54.6, executor at 0.53.6 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0111 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0111, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0111 — Full text of shortened labels

- Interface polish: hover over shortened text to see all of it
- Update UI to ~0.25.25 (any text the App cuts with an ellipsis shows its whole text in a tooltip on hover or keyboard focus; Download Hub draws no coloured label tags or tag lists, so the tag colour and "+N" changes do not apply) and SDK/App and the Go module to 0.53.49 (its change is for Rust Apps only); CLI unchanged at ~0.54.6, executor at 0.53.6 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0110 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0110, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0110 — DSM display language

- Opens in your DSM display language instead of the browser's
- Update SDK/App and the Go module to 0.53.48 (the App follows the user's DSM display language instead of the browser's, also for a sign-in that is already cached) and UI to ~0.25.23 (Download Hub uses no coloured label tags, so its pages look the same); CLI unchanged at ~0.54.6, executor at 0.53.6 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0109 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0109, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0109 — Faster interface loading

- The interface loads slightly faster
- Update UI to ~0.25.22 (the component library ships as multi-module ESM, so the first screen loads less JavaScript; exports unchanged), SDK/App and the Go module to 0.53.46 and CLI to ~0.54.6; executor unchanged at 0.53.6 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0108 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0108, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0108 — Selected names in drop-down lists

- Drop-down lists show the selected name correctly
- Update UI to ~0.25.21 (a Combobox shows the name of the selected item correctly); SDK/App and the Go module stay at 0.53.44, executor unchanged at 0.53.6, CLI stays at ~0.53.18 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0107 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0107, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0107 — Smoother panels and menus on phones

- Panels and menus look and respond better on phones
- Update UI to ~0.25.20 (bottom sheets and the side menu keep their shadow on phones; touch no longer leaves a focus ring); SDK/App and the Go module stay at 0.53.44, executor unchanged at 0.53.6, CLI stays at ~0.53.18 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0106 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0106, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0106 — Full button labels

- Long button labels wrap instead of being cut off
- Update UI to ~0.25.19 (button labels always wrap and are never truncated with an ellipsis); SDK/App and the Go module stay at 0.53.44, executor unchanged at 0.53.6, CLI stays at ~0.53.18 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0105 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0105, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0105 — More consistent spacing and corners

- Dialogs, side panels and controls use more consistent spacing and corners
- Dialogs and panels open faster on phones
- Update UI to ~0.25.18 (one token scale: control radius 6→4, dialog radius 10→12 in a DSM window, dialog and side sheet inset 20→16, 150/250 ms transitions; sheets and dialogs open without a forced layout pass) and SDK/App and the Go module to 0.53.44 (self-package restart, `openControlPanel`, unix socket upstream for the identity gateway; none used by the App); executor unchanged at 0.53.6, CLI stays at ~0.53.18 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0104 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0104, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0104 — Open a list item from its row number

- Clicking a row number in a list opens that item
- Update UI to ~0.25.17 (clicking a grid row number opens the record) and SDK/App and the Go module to 0.53.41 (`api.IngressTime`; request durations run from when the request reached the SDK); executor unchanged at 0.53.6, CLI stays at ~0.53.18 and templates at 0.42.1. The App code is unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0103 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0103, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0103 — Home on every page in the launcher

- In the Package Land launcher, every page has Home, and Back steps through the App's pages
- Update UI to ~0.25.16 (Home leads every App page inside the Package Land launcher; sub-pages are history entries) and CLI to ~0.53.18; SDK/App and the Go module stay at 0.53.37 (executor unchanged at 0.53.6) and templates at 0.42.1. The App code and the Go backend are unchanged

Release preparation includes the catalog check, both architecture builds and CLI inspections compared with the released 0.2.2-0102 baseline. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0102, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0102 — The phone keyboard stays up

- On Android phones, the keyboard no longer closes when you tap a text field
- Update UI to ~0.25.12: equal left and right phone margins, only actions float over a page, and a back arrow to the App list inside the Package Land phone app; SDK/App and the Go module stay at 0.53.37 (executor unchanged at 0.53.6), CLI at ~0.53.15 and templates at 0.42.1. The App code and the Go backend are unchanged

Release preparation includes the catalog check, both architecture builds, CLI inspections against the same catalog and the released 0.2.2-0101 baseline, and source/SPK leak scans. Unit suites other than the UI pin check, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0101, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0101 — Package Center works after installing in Safari

- Fix a Package Center error after installing in Safari
- Update CLI to ~0.53.15: INFO no longer asks DSM to reload nginx on install and uninstall (the executor reloads it itself when it removes the portal entry); SDK/App and the Go module stay at 0.53.37 (executor unchanged at 0.53.6), UI at ~0.25.9 and templates at 0.42.1

Release preparation includes the catalog check, both architecture builds, CLI inspections against the same catalog and the released 0.2.2-0100 baseline, and source/SPK leak scans. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0100, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0100 — Confirm dialogs stay inside the app window

- Confirm dialogs opened from a wizard cover only the app window
- Update SDK/App and the Go module to 0.53.37 (executor unchanged at 0.53.6), UI to ~0.25.9 and CLI to ~0.53.14; templates stay at 0.42.1

Release preparation includes the catalog check, both architecture builds, CLI inspections against the same catalog and the released 0.2.2-0099 baseline, and source/SPK leak scans. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0099, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0099 — Full-height lists on phones

- Phone lists show all rows with a single page scroll
- Update SDK/App and the Go module to 0.53.36 (executor unchanged at 0.53.6), UI to ~0.25.8, CLI to ~0.53.13 and templates to 0.42.1

Release preparation includes the catalog check, both architecture builds, CLI inspections against the same catalog and the released 0.2.2-0098 baseline, and source/SPK leak scans. Unit suites, full gates, browser tests, cross-architecture execution, new screenshots and NAS acceptance are excluded; catalog screenshots are reused from the previous release. The executor and policy match 0.2.2-0098, so existing approval is eligible for retention when the installed protected runtime and DSM task readback still match.

## 0.2.2-0098 — Smoother tables and phone approval on older DSM

- Make resizing table columns smoother
- Fix occasional errors when approving from a phone on older DSM versions
- Update SDK/App and the Go module to 0.53.34 (executor 0.53.6), UI to 0.25.7, CLI to ~0.53.12 and templates to 0.42.0
- Switch the root and frontend dependencies to pnpm with exact `pnpm-lock.yaml` pins
- Take Package Center descriptions and release notes only from the catalog (`SYNOPKGLAND_CATALOG` / `--catalog`)

Release preparation includes the catalog check, both architecture builds, CLI inspections against the same catalog, and source/SPK leak scans. Unit suites, full gates, browser tests, cross-architecture execution, screenshots and NAS acceptance are excluded. The executor moves from 0.53.5 to 0.53.6, so administrators may be asked to approve again after upgrading.

## 0.2.2-0097 — More reliable sign-in and busy NAS feedback

- Improve sign-in reliability and responsiveness when the NAS is busy
- Make selected items easier to identify on phones
- Update SDK/App and the Go module to 0.53.19, UI to 0.25.2 and CLI to ~0.52.11 with reproducible lockfile pins
- Replace short App read and background-save deadlines with the SDK request allowance while retaining cancellation

Release preparation includes both architecture builds, CLI inspections, source/SPK leak scans and the existing unit tests for the adjusted read deadlines. Desktop catalog screenshots are reused from the previous release. Full unit suites, full gates, browser tests, cross-architecture execution and NAS acceptance are excluded.

Offline comparison with released 0.2.2-0096 finds unchanged executor, policy and task definitions on both architectures. Approval retention is eligible only if the installed protected runtime and exact DSM task readback still match that baseline; no live retention acceptance was performed.

## 0.2.2-0096 — Refreshed appearance and startup feedback

- Improve startup and authorization feedback with updated shared components
- Refresh window corners, dialog overlays and keyboard focus styling
- Refine the appearance and touch feedback on phones
- Update SDK/App and the Go module to 0.53.15, UI to 0.25.1 and CLI to ~0.52.10 with reproducible lockfile pins

Release preparation is limited to both architecture builds, CLI inspections, source/SPK leak scans and refreshed English screenshots from the local fixture. Unit tests, full gates, browser tests, cross-architecture execution and NAS acceptance are excluded from this dependency-only rebuild.

Offline approval retention is unknown: CLI 0.52.10 rejects the released 0.2.2-0095 baseline because its older install-selected wizard files do not match the current format. Current SPKs are built and inspected independently; this does not establish live approval retention.

## 0.2.2-0095 — Smooth phone scrolling and quieter status checks

- Fix the phone screen freezing when scrolling upward after the header hides
- Correct Portal header layering, the phone top bar and the settings action footer
- Use an in-memory executor audit ring for read-only status checks to reduce disk wake-ups
- Update SDK/App and the Go module to 0.53.6 and UI to 0.23.7; follow CLI patch updates within ~0.52.5 while retaining lockfile pins

Offline comparison with 0.2.2-0094 finds an executor-only payload update with unchanged policy and task definitions on both architectures. Approval retention is eligible only if the installed protected runtime and exact DSM task readback still match that baseline.

## 0.2.2-0094 — Phone approval and layout fixes

- Update CLI to 0.52.5, SDK/App and the Go module to 0.53.4, and UI to 0.23.6 with exact dependency pins
- Fix first approval after phone sign-in by including X-SYNO-HASH and JSON-encoded API.Info request formats in the generated mobile entry
- Fix excess bottom space, sideways page scrolling and sticky offsets on phones, and use transform-only Portal header motion
- Keep the existing mobile registration and shared SDK approval flow

Validation is limited to related unit tests, both architecture builds, manifest checks, SPK inspections, the local mobile-approval artifact check and leak scans. No device testing was performed for this build.

Offline comparison with 0.2.2-0093 finds an executor-only payload update with unchanged policy and task definitions on both architectures. Approval retention is eligible only if the installed protected runtime and exact DSM task readback still match that baseline.

## 0.2.2-0093 — Toolchain refresh and compact phone controls

- Update UI to 0.23.3, SDK/App to 0.53.2, CLI to 0.52.4 and templates to 0.41.3 with exact npm and Go module pins
- Use the UI’s compact phone navigation, scroll-aware bars and shared touch targets; remove redundant App control-sizing overrides
- Keep the full App name in the phone navigation drawer and avoid an empty title bar on tab pages
- Include the UI's fixes for excess space above the phone tab bar on scrolling lists with a floating action button and the drawer close button's focus after a tap
- Allow administrators to approve the executor from mobile entries using the same pinned tasks and password confirmation as DSM windows
- Package minified public JavaScript and CSS without source maps using the CLI’s public UI checks
- Remove sentence-ending periods from Package Center descriptions

Validation is limited to related unit tests, both architecture builds, manifest checks, SPK inspections and a public-file leak scan. No device testing was performed for this build.

Offline comparison with 0.2.2-0090 finds unchanged executor, policy and task definitions on both architectures. Approval retention is eligible only if the installed protected runtime and exact DSM task readback still match that baseline.

## 0.2.2-0090 — Mobile sharing and reliable saves

- Share web links, magnet links, eD2k links, text containing links and torrent files into Download Hub through the compatible Syno Package Land mobile app, then review the new download before starting
- Add further shared links and torrent files to an open download draft while keeping your edits
- Save completed NAS downloads to your phone from task details, including individual files from a multi-file download
- Download files whose suggested names contain slashes or question marks; unsupported characters are replaced with underscores
- Save another download with the same name without replacing the existing file; the new file receives a name such as `name (1).ext`, which is also used for previews and phone downloads
- Correctly recognize completed multi-file saves in subfolders and recover saves left pending by the earlier result check without copying the files again
