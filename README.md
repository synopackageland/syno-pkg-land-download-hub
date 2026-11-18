# Download Hub for DSM

Download Hub is a native Synology DSM app for downloading files, torrents and
online media to your NAS. It uses the unmodified official Gopeed engine and
supports x86_64 and aarch64 NAS models.

Each DSM user manages their own downloads and preferences. DSM Application
Privileges controls access; administrators manage the shared engine and tools.

## Features

- HTTP/HTTPS, BitTorrent and ED2K downloads, with pause and resume.
- Video and audio downloads through optional yt-dlp; image galleries through
  optional gallery-dl on x86_64.
- Search sources, followed channels and personal media rules.
- Personal destination folders with DSM permission checks.
- Trusted Gopeed extensions and verified engine/tool updates with rollback.
- Mobile link sharing and saving finished files to a phone.
- Discoverable CLI commands for download and search workflows.

## Architecture

```text
Vue UI / CLI -> SDK-authenticated Go API -> package daemon
                                            |-- Gopeed / media tools
                                            |-- search helper
                                            `-- private staging -> SDK executor -> user folder
```

The API enforces ownership and administrator permissions. The daemon prepares
and runs tasks in the background; the SDK executor publishes staged files using
the destination owner's DSM identity. Saved status requires a committed receipt.

## Setup and development

Install Node.js compatible with the pinned dependencies, pnpm from the
`packageManager` field in [package.json](package.json), Python with `tarfile`
extraction-filter support, and the Go toolchains declared in
[backend/go.mod](backend/go.mod), [search-helper/go.mod](search-helper/go.mod)
and [scripts/prepare.py](scripts/prepare.py). Go SDK repository access and network
access to official upstream archives are required. Keep dependency pins intact.

Install dependencies from the repository root:

```sh
pnpm install --frozen-lockfile
pnpm --dir frontend install --frozen-lockfile
```

For local development, rebuild the SDK-mounted UI as files change:

```sh
pnpm --dir frontend exec vite build --watch
```

This produces modules under `ui/`; there is no standalone development page or
`dev` script. Interactive integration needs DSM and the SDK context.

Run tests and build the frontend:

```sh
pnpm test
pnpm --dir frontend run build
```

Package SPKs using a catalog with descriptions and release notes matching the
manifest being built. The store text for released versions is in the public
[synopackageland/catalog](https://github.com/synopackageland/catalog): clone it
and point `SYNOPKGLAND_CATALOG` at its `catalog.yaml`.

```sh
export SYNOPKGLAND_CATALOG=/path/to/catalog/catalog.yaml
pnpm run build
pnpm run test:artifact
```

The build verifies upstream archives, compiles the runtime and UI, runs SDK
check/build, and inspects both architecture SPKs in `dist/`. See
[build inputs and validation boundaries](docs/validation.md) for catalog setup.
After editing `operations.json`, run `pnpm run ctl:generate`.

Each release also publishes the GPL corresponding source. From a clean checkout
at the release tag, build those archives (upstream Gopeed source, its vendored
Go modules, and this repository with vendored modules) into a folder:

```sh
python3 scripts/gpl_sources.py dist/source
```

The archives are reproducible, so their SHA-256 sums can be checked
against the release assets.

See [media downloads](docs/media-downloads.md),
[BT and multi-file publication](docs/bt-and-multi-file.md),
[settings and runtime boundaries](docs/settings-inventory.md),
[CLI commands](docs/ctl.md) and [browser search access](docs/cloudflare-search.md)
for implementation details.

## License

Licensed under GPL-3.0-or-later; SDK components retain their MIT notices. See
[LICENSE](LICENSE), [NOTICE](NOTICE) and [artwork attribution](assets/icon-source.json).
