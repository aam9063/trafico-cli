# Review report: WATCH-01c

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-29
- **Scope reviewed:** the new `internal/watch` package (engine, message renderer, sinks) plus the schema v1→v2 change in `internal/store`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

This is the last layer between the feed and the user's phone, so the reviewer's
job was to find the notification that should arrive and does not, or the one
that carries something it must not.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| minor | `sinks.go:35-45`, `:78`, `:134` | **The response body was never closed on success.** `checkStatus` only read and closed on a non-2xx response, and neither `Deliver` had a `defer`. Every successful notification leaked a connection in a process that runs every five minutes indefinitely. Not a crash: an accumulation. | **Applied.** `checkStatus` drains and closes on every path so the connection can be reused. Test with a recording transport asserting the body is closed and drained after a successful ntfy and webhook delivery. |
| minor | `sinks.go:145-163` | **One unrenderable event blocked the queue forever.** `DrainPending` stopped at the first failure, which is right for an outage but wrong for undeliverable data: a payload that cannot be rendered will never render, so everything behind it is stalled with no recovery. | **Applied.** The two cases are separated. A render error stamps the event out of the retry queue (`store.MarkSkipped`, the row stays for diagnostics) and the drain continues; a `sink.Deliver` error keeps stop-and-retry. Test enqueues three events, makes the first unrenderable, asserts the other two are delivered. |
| nit | `watch.go:262-266`, `:291-295` | `marshalPayload` errors were swallowed into `data = nil`, storing an empty payload that would render as an error forever. | **Applied.** A marshal error stores a fixed marker that `RenderMessage` reports as an explicit internal error, so the failure surfaces instead of becoming a queue stall. |
| nit | `store.go:199-227` | The migration read `user_version` **outside** the transaction, so two processes opening the same version-1 database could both decide to migrate and the loser failed with a duplicate-column error. | **Applied.** The version is re-read as the first statement inside the transaction and the step is skipped if another process already applied it. The concurrency test then exposed a residual `SQLITE_BUSY` on the lock upgrade, absorbed by a bounded retry (10 × 20 ms) that re-reads and skips. |
| nit | `watch.go:31-37` | The baseline sentinel's collision argument borrowed an invariant from `internal/model` ("ids always look like `dgt:...`"). | **Applied.** One-line guards skip any incident whose id equals the sentinel, making the safety local. |
| advisory | `watch.go:124-209` | No mutual exclusion around the read-decide-write cycle, while `internal/store` documents two overlapping handles as legal. A `watch run` and a manual `watch check` landing on the same snapshot both append `incident.new` — a duplicate, never a loss. | **Deferred to WATCH-01d**, recorded in the Ola 3 task document: it is a CLI-level lock. |

Verified by execution, no findings:

- **S7 / P-07, the hard rule**: the payload struct has no field for a watch name, route name, place or coordinates — by construction, not by filtering. The reviewer probed both `new` and `cleared`, checked the exact key set of the payload, and rendered messages containing neither the watch name (which embedded coordinates) nor the incident coordinates, always with attribution.
- **S3 baseline**: with zero incidents the sentinel row is written; the next check emits exactly one `incident.new`. The failure the sentinel prevents — a new swallowed, then a `cleared` for a never-announced incident — cannot be constructed.
- **S5 events**: full lifecycle probed — absent once (silent), reappearance resets, absent twice produces exactly one `cleared`, further absences silent. Road, PK and type changes alone emit nothing; the update triggers are exactly severity, lanes (order-insensitive) and end time.
- **S8 sinks**: ntfy, discord, slack and telegram shapes; exec via stdin, never a shell. A failure of any kind leaves the event pending; no path marks delivered before knowing the outcome.
- **Migration against a real v1 database**: built by driving the **shipped v1 code** from `HEAD`, then opened with the v2 code — `user_version` bumped, state rows and pending events survived.

## Veredicto

**Approve after one correction round.** Both minors were the kind that do not break today and hurt later: a connection leaked per successful delivery, and one corrupt event silencing all the others. The three nits came with them because each closed a path where a failure would have been silent or a guarantee borrowed. Per §10.5 the round is spent.

### Not verified

- The concurrent-migration race and the cross-process duplicate were reasoned from source before the fix; after it, a four-child-process test exercises the migration.
- `internal/window` internals (DST, midnight): treated as a trusted, separately reviewed dependency.
- `--emit-initial`, `watch add/list/remove/check/run` and the service commands: that code does not exist yet.

### Debt recorded

The migration retry matches error **text** (`SQLITE_BUSY`, `database is locked`) because the driver does not expose a typed code through the wrapped error. If `modernc.org/sqlite` ever changes that wording, the retry degrades to a surfaced error — not to silent data loss — and a test would show it.
