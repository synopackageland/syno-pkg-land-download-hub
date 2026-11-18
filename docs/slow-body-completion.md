# Delayed HTTP bodies and Gopeed completion (B2)

Gopeed 1.9.3 can report `done` and `progress.downloaded=41984` after 15 seconds
without having written any of the advertised 41984-byte body. File size alone
cannot certify completion: the HTTP fetcher preallocates the output file.

The unmodified official macOS arm64 release reproduces the same failure as the
DSM Phase 2 report. A local HTTP source flushes `Content-Length: 41984`, delays
45 seconds, then sends deterministic nonzero bytes. The engine reports `done`
after 15.09 seconds; the staged file is 41984 zeros, SHA-256
`23559d9ee0afe1511261d05350bc6764d73d3084a51b3fdc79cc1a08093ccd96`.
An independent request receives the correct body after 45.15 seconds.
The engine's `/api/v1/tasks/<id>/stats` still reports one connection with
`downloaded: 0, completed: false, failed: false`.

## Minimal upstream reproduction

This uses the official executable directly, without the package adapter, DSM,
or the SDK. It requires Python's standard library and leaves only fixture state,
samples and logs; its temporary API token is deleted during cleanup.

```sh
python3 scripts/repro_slow_body.py \
  --binary /path/to/official/gopeed \
  --output /tmp/gopeed-slow-body-repro
```

The output directory must not exist. `--delay 2` provides a fast comparison.
Host release: `gopeed-web-v1.9.3-macos-arm64.zip`, archive SHA-256
`d0a6923297c31cdcdfd9caab2e1d312b6a91a1cc3bab4d4ec43418093cc7c927`;
executable SHA-256 `55d74189490ec0302fd9155089163ec729f41ede42a169620942bf6ac1a5a2b8`.
The executable was byte-compared with the verified official archive.

## Root cause

The verified v1.9.3 source archive is pinned in `third-party/gopeed/lock.json`.
No upstream code or executable was modified.

- `internal/controller/controller.go`: `Touch` truncates the new file to the
  advertised size before any body bytes arrive.
- `internal/protocol/http/fetcher.go`: the non-range resolve-response path uses
  a 15-second `TimeoutReader`. A non-EOF read error sets `conn.State=connFailed`
  but deliberately leaves `conn.failed=false` and returns without retrying.
- `onDownloadComplete` notices incomplete chunks but constructs a terminal error
  only for connections where both `State==connFailed` and `failed==true`. This
  transient read timeout therefore returns a nil error despite zero writes.
- `pkg/download/downloader.go`: `onDone` then sets `Progress.Downloaded` to the
  advertised resource size, masking the actual zero-byte progress.

The former package adapter accepted `status == done` and the staging file's
size. That published the preallocated zeros. The SDK moved exactly the bytes it
was given; this content failure belongs to the engine and package completion
admission, independently of SDK blockers B1, B3 and B4.

## Package-side protection

Before the first `moveFromStaging` admission, both the daemon and owner API must
verify HTTP task status, resource size and progress against the regular staged
file, then query the engine's connection write counters. All reported
connections must explicitly be completed, not failed, and their nonnegative
written totals must equal the expected file size without overflow. Missing,
malformed or inconsistent evidence fails closed. Bounds remain 16 GiB/file,
128 reported connections, the existing bounded IPC response and polling pass.

Incomplete terminal content gets durable `download_incomplete` /
`download_failed`, with no executor job ID, no save admission and no automatic
retry. Private staging bytes remain for diagnosis. The UI says Download failed,
explains that nothing was saved and asks the owner to remove and download again;
it hides the unreliable 100% progress and excludes that task from Completed.
Copy is translated in all nine locales. Legitimate all-zero content is accepted
when its completion evidence is valid; this is not a zero-byte-content heuristic.

Running sources continue to wait. A transient `/stats` IPC failure waits for a
later verification pass, never admitting a move on unavailable evidence. Once an
operation has been admitted, restart/polling still queries only its original
reference: it does not redownload, re-admit, or alter SDK receipt handling.

This verifies the engine's actual write/completion evidence, not a source-provided
cryptographic checksum. It does not establish byte identity for every possible
upstream bug or dishonest server. Gopeed 1.9.3 writes a range resource that the
resolve request prefetched in full without opening any connection, so its
`/stats` lists none; small files usually finish this way. Connections are
persisted and a non-range body always reads through connection 0, so an empty
list on a done HTTP task whose resource supports ranges is this prefetch, and is
accepted. An empty list without range support, or incomplete evidence retained
after a restart, is still refused rather than published.
An upstream fix exposing trustworthy complete-write evidence would improve that
compatibility. No fallback trusts file allocation, scans for zeros, or refetches
the source with different credentials.
