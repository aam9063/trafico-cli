# Review report: WATCH-01b

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-29
- **Scope reviewed:** the new `internal/store` package (watch state and event log) plus the `go.mod`/`go.sum` change adding `modernc.org/sqlite`, before its commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

The store is where a crash turns into a wrong notification, so the reviewer's
main job was to break it: it rewrote `ReplaceState` non-transactionally in a
scratch copy to see whether the atomicity test had teeth, probed the migration
on a torn database, and ran two processes writing concurrently.

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `store.go:375-385` | **`PruneEvents` deleted undelivered events.** The statement had no `delivered_at` guard, and the doc comment openly documented it. A pending event older than the cutoff is a notification the user **never received**, destroyed silently and unrecoverably. Reproduced: a 48-hour-old undelivered event was pruned and `UndeliveredEvents` then returned nothing. The existing `TestEventsRoundTrip` could not catch it: its only undelivered row is newer than the cutoff, so it survived for the wrong reason. | **Applied.** The guard is now `AND delivered_at IS NOT NULL`, the rule is stated in the comment, and `TestPruneEventsKeepsUndelivered` **fails against the previous code** (`pruned = 2, want 1`). |
| major | `store.go:400` | **Timestamps did not sort chronologically.** `time.RFC3339Nano` omits trailing zeros, so `08:00:00Z` sorts *after* `08:00:00.5Z` as text, because `'Z'` > `'.'`. That breaks the oldest-first retry order of `UndeliveredEvents` and the prune cutoff. Reproduced with a whole-second stamp next to a fractional one. | **Applied.** A fixed-width format with all nine fractional digits (`2006-01-02T15:04:05.000000000Z07:00`) is used by both `storeTime` and `parseStoredTime`. `TestUndeliveredEventsOrdersChronologically` fails against the previous code. Nothing is released, so no data migration was needed. |
| minor | `store_test.go` | The newer-schema refusal branch was untested (it worked; verified by hand). | **Applied.** `TestOpenRefusesNewerSchema` writes `user_version = 99`, reopens and asserts the error. |
| minor | `store.go:69-72`, `store.go:398-406` | Existing files and directories were never **tightened**: modes were applied only at creation, so a database restored from a backup kept a wider mode while §7 says it is `0600`. | **Applied.** `Open` now `chmod`s the file to `0600` and the directory to `0700` when they differ, no-op on Windows. The test skips there with an explicit reason and runs on the Linux and macOS CI legs. |
| nit | `doc.go` | The package doc claimed it persists "routes, places"; it handles watch state and events only. | **Applied.** |
| nit | `store.go:332` | `RowsAffected`'s error was swallowed by `err == nil &&`, silently skipping the missing-row check. | **Applied.** The error is handled explicitly. |

Confirmed by execution, no findings:

- **Atomicity has teeth.** With `ReplaceState` rewritten non-transactionally, `TestReplaceStateIsAtomicOnFailure` **fails**; the single deferred-rollback transaction is what makes the previous snapshot survive.
- **Migration is safe on damage.** A schema transaction failing halfway leaves `user_version` at 0 (the stamp rolls back with it) and the next `Open` recovers. A torn database with one table present recovers fully. A newer-version database is refused.
- **Concurrency.** Two processes, ten goroutines each, with `busy_timeout(5000)`: zero errors. The reviewer notes the honest caveat: a lock held longer than five seconds still yields `SQLITE_BUSY`, and WAL is not enabled.
- **`MarkDelivered` stamps, never deletes** (`TestMarkDeliveredKeepsRows`).
- **No CGO anywhere**: the modernc driver is pure Go, and linux, darwin and windows builds all succeed with `CGO_ENABLED=0`.
- **`go.mod` bump justified**: `modernc.org/sqlite v1.61.0` itself declares `go 1.26.0`, so the floor move is the dependency's requirement and not a choice. The four promoted `// indirect` markers were stale — all are real direct imports (`go-toml` in `config/load.go`, `pflag` in `cli/incidents.go`, `yaml.v3` in `datex2/mapping.go`).
- **P-07**: no logging in the package, and watch names appear only in local error strings.

## Veredicto

**Approve after one correction round.** The blocker destroyed notifications permanently, which is the worst class of defect in a watcher: not a wrong message, a message that never arrives and cannot be recovered. It is fixed with a test that fails against the old code. The major was invisible to every existing test and only appears when two timestamps land in the same second — fixed with a regression test. The four smaller findings came with them.

### Not verified

- POSIX permission bits at runtime: both permission tests skip on Windows and there is no Unix runtime available here. They run on the `ubuntu-latest` and `macos-latest` CI legs, and the tightening path is exercised by `TestOpenTightensLoosePermissions` there.
- `go test` executed on linux/darwin: the binaries were cross-compiled, not run.
- `busy_timeout` behaviour when a transaction holds the lock longer than five seconds.
- The watch semantics themselves (baseline, `incident.cleared` after two absences): `internal/watch` does not exist yet; the store only persists what that layer will decide.
