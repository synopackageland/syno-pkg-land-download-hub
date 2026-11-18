# Validation boundary

## Build inputs

`pnpm run build` runs `scripts/build.py`: registry generation checks, SDK
`check`, SDK `build`, package-policy verification and `inspect` for both SPKs.
The preparation hook verifies official upstream archives and builds the package
runtime and UI. Keep both pnpm lockfiles and the Go dependency pins intact.

Package Center descriptions and per-version release notes come only from the
catalog's `catalog.yaml`, not `dsm-package.yaml`. Set `SYNOPKGLAND_CATALOG` in the
shell or `.env` to that file, or pass `--catalog <catalog.yaml>` when calling
`synopkgland check`, `build` and `inspect` directly. The catalog needs release
notes for the exact manifest version. Run `pnpm run test:artifact` after building.

Local fixture tests and screenshots establish local behavior; they do not
certify NAS ACLs, writer identity or the full package lifecycle.

## Internal-name check

`scripts/check-internal-names.sh` (run by `npm test` and fast CI) fails when a
file names an internal `.syno` host, a company-internal or personal domain, a
private IPv4 address other than the common examples, a work-thread id, a
`/Users/<name>` folder or a secret. List machine names outside the repository in
`SYNOPKGLAND_INTERNAL_NAMES` or a file named by `SYNOPKGLAND_INTERNAL_NAMES_FILE`
to check them too. A commit hook can run the same rules on just the added lines
and the message: `--staged` and `--message FILE`.
