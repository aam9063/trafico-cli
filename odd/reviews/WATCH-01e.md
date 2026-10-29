# Review report: WATCH-01e

- **Reviewer:** `gentle-ai-verify` (clean-context, read-only subagent, independent of the implementer)
- **Date:** 2026-10-29
- **Scope reviewed:** the `watch` commands — `internal/app/watchrun.go`, `internal/cli/watch.go`, the schema v4 change in `internal/store`, before their commit

<!-- Reviewed in a clean-context, read-only pass (docs/ARRANQUE.md §10.5).
     Findings follow the order: correctness; P-04, P-05, P-07, P-08; JSON
     contract and exit codes; time zones; simplicity and duplication; tests;
     parser performance with benchmark evidence. Keep the two headings below
     exactly as written: the CI gate (scripts/require_review.sh) matches them. -->

## Hallazgos

| Severity | File:line | Finding | Proposal |
|---|---|---|---|
| blocker | `internal/app/watchrun.go:368-389` | **An unusable sink was silently dropped with a success exit.** A watch configured with `--notify tg` emitted the event, failed to build the sink, stamped the event out of the queue and returned nil — exit 0. The user asked for that notification explicitly and it was permanently lost with no signal. | **Applied.** `watch add` now rejects a sink it cannot construct (exit 2, where the user can fix it) and a sink that becomes unusable later is a per-watch error in the run report with its events left queued for retry. `MarkSkipped` is now only for zero-sink watches, removed watches and unrenderable payloads. |
| major | `watchrun.go:596-601` | **The stale-lock takeover could delete a live lock.** The stale path removed whatever file sat at the lock path without re-verifying it was the one it had stat'd: two processes could interleave and **both** end up holding the lock — the exact failure the lock exists to prevent. | **Applied.** The takeover now runs under a second `O_EXCL` gate file, and the gate holder re-verifies token, device and inode with `os.SameFile` **while holding the gate**, so nothing can replace the file between the check and the remove. Release is ownership-checked. A stress test runs two concurrent cycles and asserts exactly one executes. |
| major | `watchrun.go:602-604` | A persistently undeletable stale lock made `watch run` spin hot forever instead of failing. | **Applied.** The removal error is handled with bounded retries (absorbing transient Windows readers) and a genuinely undeletable lock fails with an error naming the removal failure. |
| major | `watchrun.go` (RunWatch/RunAll) | **The interval was validated but never enforced.** Two runs 30 seconds apart both fetched and both checked. With a scheduler every minute that is 1440 daily requests to the DGT, which P-06 forbids: the interval only held if the scheduler happened to respect it. | **Applied.** Schema v4 records `last_check_at` per watch; the gate runs **before the fetch** in both paths, so a skipped watch costs no request, and the skip is reported as `skipped_interval`, deliberately distinct from the window skip. |
| minor | spec S3 | `--emit-initial` is not implemented. Cobra rejects it loudly, so nothing is silent. | **Deferred** to its own task: it needs the engine's baseline behaviour to become optional. |
| minor | spec S8 | A `telegram` sink cannot be configured because `config.Sink` has no `chat_id`. | **Deferred** to its own PR: it is a contract change. Today such a sink fails loudly at `watch add`. |

Verified by execution, no findings beyond the above:

- **S4 windows**, **S5 events**, **S7/P-07** and **S10** hold end to end. The reviewer's probe observed a cleared notification reading `Incidencia finalizada: restricción en A-372 PK 50.25-50.65. Fuente: DGT (CC BY)` — road, PK and type, no watch name and no coordinates.
- **`check` versus `run`**: `check` runs the real engine against a scratch copy of the state, so it reports what would be notified and writes nothing; the next real `run` performs the detection.
- **Tests with teeth**: removing the lock fails the contention tests; making `check` write fails its report test; marking a failed delivery delivered fails the retry test.

## Veredicto

**Approve after one correction round.** The blocker lost a notification the user had asked for, silently; two of the majors broke the guarantees the previous parts established (the lock did not lock, the interval did not throttle). All four are fixed with tests that fail against the previous code, and the interval gate now sits before the fetch so the DGT is not charged for a skip.

### Not verified

- The lock's takeover race under adversarial timing: a stress test asserts single execution, but the alternative — OS advisory locks — would be structurally simpler and needs build-tagged files.
- POSIX permission bits at runtime (Windows host; CI runs the Linux and macOS legs).
- `watch service` (WATCH-02).

### Documentation debt

`docs/ARRANQUE.md` §5.5 should record the `skipped_interval` run field and the `last_check_at` column. It is outside the edit surfaces of this unit and is added to the Ola 3 closing task.
