# Admission and receipt recovery (SDK 2a4a2740)

Gopeed persists the exact `delegate.MoveParams` in each job before IPC, alongside
its stable SaveJobID and grant ID. Older intents acquire this field from their
original stored staging/name/size/reference, without new IDs or grants. Every
unfinished nonterminal save calls `Service.MoveFromStaging` with that same tuple.
The pinned SDK owns reserved/sent/accepted phases, spool-lock claims, source
identity checks, bounded backoff (10 possibly-delivered sends, 2 seconds to
5 minutes), and the executor's existing exact-tuple deduplication. Gopeed never
clears mirrors, sends raw IPC, or implements its own retry loop.

Transient codes EXECUTOR_UNAVAILABLE, EXECUTOR_TIMEOUT, EXECUTOR_PROTOCOL,
canceled, EXECUTOR_PAUSED, EXECUTOR_STARTING (SDK 0.10.3), queue-full, STORAGE_ERROR,
ADMISSION_PENDING, jobs_busy
and jobs_unavailable leave
the job pending, not uncertain. The next daemon pass tries the same SDK facade.
All other admission/status refusals, including STAGING_CHANGED,
ADMISSION_EXHAUSTED, spool_unsafe, journal_corrupt and accepted operation-not-found,
become terminal save_failed;
DELEGATION_DENIED retains the specific access_denied UI. APP_DENIED keeps
the grant since SDK 0.9.6 and, like PERMISSION_DENIED, is a retryable
save_failed. Staging and
original reference remain. A fresh destination only applies to new downloads.
A protected uncertain operation is still nonterminal: the SDK queries its known
reference and never re-admits it. Saved still requires the original matching
committed receipt. Published jobs are excluded from later passes; a stale
concurrent save cannot overwrite a durable completion or refusal.

Each job receives a fresh 15-second context rather than sharing a 15-second
pass deadline. The engine-operation.lock guards only task evidence, safe legacy
staging relocation and intent persistence; it is released before SDK IPC.
The authenticated publish route uses the same short engine-lock preparation,
so neither an admission wait nor a status wait occupies the engine lock for an
entire pass. Task deletion still serializes with preparation, requires cancellation
of an attempted move, and must retain the job if cancellation is unconfirmed.
Global update/plugin guards are unchanged.

Tests written first showed that the previous product failed absent
socket recovery, injected pre-transport crash recovery and two-job independent
budgets/list responsiveness. Positive tests now exercise the production SDK
mirror and Unix client; fixture executor responses certify wiring and one
admission, not real filesystem ownership or DSM ACLs. Real SDK receiver/writer
and NAS evidence belongs to the SDK device acceptance suite. Additional tests cover the retry/terminal
matrix, lost accepted operations, exact legacy tuple persistence, committed
exclusion, stale result monotonicity, and translated pending/failure UI.

Upgrade recovery is restricted to old save_failed records with jobs_busy,
jobs_unavailable or canceled, SaveStarted, a complete persisted MoveParams, and
an exact match to the original owner/grant/job/staging/name/size. It persists
pending before calling the same SDK facade; it never reconstructs a missing
legacy tuple, takes the current destination's grant, creates a new reference,
or revives access_denied/download_failed/other terminal errors. Committed jobs
are monotonic and are never submitted again. SDK journal failures may return a
known protected operation read-only; only a matching committed receipt makes
Gopeed Saved. Executor uncertain remains nonterminal.
