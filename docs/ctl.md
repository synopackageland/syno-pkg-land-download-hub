# Download Hub operation registry

Requires CLI 0.48.0, SDK/App 0.49.2 and templates 0.39.1. The aliases remain
`gopeed` and `download-hub`; the package ID remains `SynoPkgGopeed`.

Edit [operations.json](../operations.json), then run `pnpm run ctl:generate`.
Commit the generated permissions, runtime registry and `generated/` artifacts.
`pnpm run check` and the build run `ctl generate --check` to refuse drift.
The CGI uses `api.NewServerWithOperations` to require exact handler, registry
and permission equality, including hidden methods and enabled SDK capabilities.

The complete command reference is generated in
[operations.md](../generated/operations.md). Input/output schemas are in
[ctl.json](../generated/ctl.json); AI tools are in
[tools.json](../generated/tools.json). Do not maintain another command list.

Discover the installed package and obtain the normal description approval before
executing commands. DSM sign-in, Application Privileges, administrator checks and
username/UID ownership still apply. The registry grants no resource authority.

AI reads use `read`. Every exposed mutation uses `draft`: `ctl tool` runs only its
non-mutating preview and returns a plan. A preview describes the registered effect;
it does not simulate an engine action, verify a live grant, or promise that a
resource is available. It echoes no private input. Execution checks remain with
the original handler. Obtain human authorization, inspect the normal command
with `--dry-run`, then execute it with `--yes` and read back the outcome.

Search and source probes use the existing SDK job start/status/result flow with a
bounded two-minute observation window. `--no-wait` returns promptly and retains
the complete admission record, including `outcome: busy` and the original job ID.
A busy record belongs to earlier work; it does not accept new search input.
The CLI's outer `accepted` wrapper is transport metadata, not proof of a new job.
A timeout does not prove failure or cancellation. Page reads preserve partial
results and owner-bound result IDs. Downloads keep immediate admission with
`completion: not_checked`; preparation, engine completion and committed saving
remain separate states. Keep the original job/reference after an uncertain reply.

Duplicate command aliases have been removed: `search sources` is the single safe
source listing; source and extension enable/disable pairs each become a single
`set-enabled` command with an explicit `--enabled true|false`. There are no legacy
aliases or proxy adapters.

Hidden UI/SDK operations retain their existing transports and authorization.
Synchronous maintenance calls remain hidden. NDJSON/transfer routes keep their
SDK-declared flow; they are never exposed through a JSON proxy. URLs, credentials,
torrent data, destination paths and variable extension settings remain marked
`writeOnly` where returned or accepted by the command contract.
